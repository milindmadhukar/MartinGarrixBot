package main

import (
	"context"
	"log/slog"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// dedupeBySharedIdentifier merges rows that point at the same record on a streaming
// service, whatever their credits say.
//
// The two passes before this one reason about the artist string, and there is a class
// of duplicate no amount of that reasoning can reach: the act renamed. "Sikdope & IBRA"
// and "Sikdope, Ibranovski" are one release -- IBRA is what Ibranovski is called now --
// and so are "King Arthur"/"King Topher", "DJ Raiden"/"Raiden" and "Cobuz & Bustta"/
// "Hotline". Neither artist set contains the other, so dedupeBySubsetCredit will not
// look at them, and the suspects report will not name them either: looksAliased wants
// both spellings to be six characters before it accepts one as a substring of the
// other, and "ibra" is four.
//
// Lowering that threshold is not the fix. "Ra" is a substring of "Raiden" and an artist
// in its own right, and no length that admits IBRA excludes it -- which is exactly why
// the floor was put there.
//
// So this pass stops guessing from the credits and reads the evidence instead. Both
// Monster rows link to YouTube video NxhlqzCtc2w and to Apple release 1526794459. Two
// rows naming the same recording on the same service are the same recording, and a
// rename cannot hide that.
func dedupeBySharedIdentifier(ctx context.Context, env *script.Env) (merged, deferred int) {
	rows, err := env.Queries.GetSongsForSubsetDedupe(ctx)
	if err != nil {
		script.Fatal("failed to load songs for identifier dedupe", err)
	}

	byID := make(map[int64]db.GetSongsForSubsetDedupeRow, len(rows))
	groups := map[string][]int64{}
	var order []string
	for _, r := range rows {
		byID[r.ID] = r
		for _, token := range identityTokens(r) {
			if _, seen := groups[token]; !seen {
				order = append(order, token)
			}
			groups[token] = append(groups[token], r.ID)
		}
	}

	gone := map[int64]bool{}
	// A pair sharing two identifiers must not be judged twice; the second look would
	// find the loser already deleted.
	judged := map[[2]int64]bool{}

	for _, token := range order {
		ids := groups[token]
		if len(ids) < 2 {
			continue
		}

		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				a, b := byID[ids[i]], byID[ids[j]]
				if gone[a.ID] || gone[b.ID] {
					continue
				}
				pair := [2]int64{min(a.ID, b.ID), max(a.ID, b.ID)}
				if judged[pair] {
					continue
				}

				reason := whyNotOneRecording(a, b)
				if reason == "" {
					judged[pair] = true
					winner, loser := chooseSubsetWinner(a, b)
					slog.Info("merging rows that name the same record on a streaming service",
						slog.String("identifier", token),
						slog.String("title", winner.Name),
						slog.Int64("keep", winner.ID), slog.String("keep_artists", winner.Artists),
						slog.Int64("drop", loser.ID), slog.String("drop_artists", loser.Artists))

					if !mergeRows(ctx, env, winner.ID, loser.ID, winner.StmpdSlug, loser.StmpdSlug,
						winner.BeatportID, loser.BeatportID) {
						continue
					}
					gone[loser.ID] = true
					merged++
					continue
				}

				// Only the disagreements worth a human's attention are reported. Two
				// rows sharing an identifier and nothing else is the ordinary case --
				// every track on an EP carries the EP's Apple id -- and logging those
				// would bury the handful that matter.
				if reason == "different songs" {
					continue
				}
				judged[pair] = true
				deferred++
				slog.Warn("left alone: shares an identifier but is not safely one row",
					slog.String("identifier", token), slog.String("reason", reason),
					slog.Int64("a", a.ID), slog.String("a_name", a.Name), slog.String("a_artists", a.Artists),
					slog.Int64("b", b.ID), slog.String("b_name", b.Name), slog.String("b_artists", b.Artists))
			}
		}
	}

	return merged, deferred
}

// identityTokens are the records this row claims to be, namespaced by service so that
// an Apple id can never compare equal to a Spotify one.
//
// Apple contributes the release id rather than the track id. The two rows this pass
// exists for carry ".../album/monster/1526794459?i=1526794460" and
// ".../album/monster-single/1526794459" -- the same release, and only one of them
// names a track at all, so a track id would have nothing to compare against.
//
// A release id is coarser than a track id: every song on an album shares it. That is
// safe here only because the pair rules below require the titles to agree as well, and
// two songs on one album do not share a title.
func identityTokens(r db.GetSongsForSubsetDedupeRow) []string {
	var out []string
	if v := utils.NormalizeYoutubeURL(r.YoutubeUrl.String); v != "" {
		out = append(out, "youtube "+v)
	}
	if id := utils.AppleAlbumIDFromURL(r.AppleMusicUrl.String); id != "" {
		out = append(out, "apple "+id)
	}
	if k := utils.StreamingURLKey(r.SpotifyUrl.String); k != "" {
		out = append(out, "spotify "+k)
	}
	return out
}

// whyNotOneRecording returns the reason two rows sharing an identifier must not be
// merged, or "" when they are one recording stored twice.
func whyNotOneRecording(a, b db.GetSongsForSubsetDedupeRow) string {
	titleA, variantA := utils.SplitVariant(a.Name, "", a.MixName.String)
	titleB, variantB := utils.SplitVariant(b.Name, "", b.MixName.String)

	// The commonest shape by far, and not a duplicate at all: a track and the release
	// it appears on share an Apple id and often a video. "Design EP" and "Do You Know"
	// are both Goja and both link to HCFM7DlzJls, and they are two different rows on
	// purpose.
	if titleA == "" || titleA != titleB {
		return "different songs"
	}

	// A remix and its original are legitimately published against one release. Only
	// rows agreeing on the rendition are the same recording.
	if !utils.RenditionsAgree(variantA, variantB) {
		// Named in a fixed order, so the reason a pair was skipped reads the same
		// however the two rows happened to be visited.
		first, second := rendition(variantA), rendition(variantB)
		if second < first {
			first, second = second, first
		}
		return "different renditions: " + first + " and " + second
	}

	// The catalogue is the authority on what counts as a separate release, exactly as
	// in the two passes before this one. Merging rows holding different slugs drops
	// one, and the next backfill-stmpd run recreates it.
	if a.StmpdSlug.Valid && b.StmpdSlug.Valid && a.StmpdSlug.String != b.StmpdSlug.String &&
		a.StmpdSlug.String != "" && b.StmpdSlug.String != "" {
		return "separate releases in the STMPD catalogue"
	}

	// A rendition hanging off the other row is a deliberate link, not a duplicate.
	// Where it is wrong, unlinking it is link-remix-parents' decision to revisit --
	// this pass would silently destroy the parent's remix instead.
	if isParentOf(a, b) || isParentOf(b, a) {
		return "one row is already filed as a rendition of the other"
	}

	return ""
}

func isParentOf(parent, child db.GetSongsForSubsetDedupeRow) bool {
	return child.ParentSongID.Valid && child.ParentSongID.Int64 == parent.ID
}

// rendition renders a variant token for a log line, naming the empty one rather than
// leaving a blank where a value should be.
func rendition(v string) string {
	if v == "" {
		return "(none)"
	}
	return v
}
