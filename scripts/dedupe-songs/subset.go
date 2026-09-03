package main

import (
	"context"
	"log/slog"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// dedupeBySubsetCredit merges rows that are the same recording credited to different
// subsets of the same artists.
//
// A match_key includes the artist set, so it only ever finds rows that already agree
// on who made the song. It cannot see that "20 and Lost" by Osrin and "20 and Lost
// feat. ELVIRA" by Elvira & Osrin are one record, or that "Alive" by Ytram & Citadelle
// is the same song as "Alive" by Martin Garrix, Ytram & Citadelle. One side simply
// omits a credit the other carries, and sorting the catalogue by name turns up pair
// after pair of them.
//
// The rule is deliberately narrow: same song title, same rendition, and one artist set
// contained in the other. A shared title alone is not enough -- two acts can name a
// song the same thing -- which is why containment, not overlap, is required.
func dedupeBySubsetCredit(ctx context.Context, env *script.Env) (merged, deferred int) {
	rows, err := env.Queries.GetSongsForSubsetDedupe(ctx)
	if err != nil {
		script.Fatal("failed to load songs for subset dedupe", err)
	}

	type key struct{ title, rendition string }
	groups := map[key][]db.GetSongsForSubsetDedupeRow{}
	var order []key

	for _, r := range rows {
		title, rendition := utils.SplitVariant(r.Name, "", r.MixName.String)
		if title == "" {
			continue
		}
		// Same reason MatchKey canonicalises it: grouping on the raw token puts
		// beatport's "All We Got (Extended Mix)" and the catalogue's "All We Got" in
		// different buckets, so the pass never gets as far as comparing their credits.
		rendition = utils.CanonicalRendition(rendition)
		k := key{title, rendition}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}

	gone := map[int64]bool{}

	for _, k := range order {
		group := groups[k]
		if len(group) < 2 {
			continue
		}

		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if gone[a.ID] || gone[b.ID] {
					continue
				}
				if utils.ArtistSetKey(a.Artists) == utils.ArtistSetKey(b.Artists) {
					continue // the ordinary match_key pass already owns these
				}
				if !utils.ArtistsSubsume(a.Artists, b.Artists) && !utils.ArtistsSubsume(b.Artists, a.Artists) {
					continue
				}

				// The catalogue is the authority on what counts as a separate
				// release, exactly as in the match_key pass.
				if a.StmpdSlug.Valid && b.StmpdSlug.Valid && a.StmpdSlug.String != b.StmpdSlug.String {
					deferred++
					slog.Warn("left alone: separate releases in the catalogue",
						slog.String("title", k.title),
						slog.Int64("a", a.ID), slog.String("a_artists", a.Artists),
						slog.Int64("b", b.ID), slog.String("b_artists", b.Artists))
					continue
				}

				winner, loser := chooseSubsetWinner(a, b)
				slog.Info("merging a song credited two ways",
					slog.String("title", k.title), slog.String("rendition", k.rendition),
					slog.Int64("keep", winner.ID), slog.String("keep_artists", winner.Artists),
					slog.Int64("drop", loser.ID), slog.String("drop_artists", loser.Artists))

				if !mergeRows(ctx, env, winner.ID, loser.ID, winner.StmpdSlug, loser.StmpdSlug,
					winner.BeatportID, loser.BeatportID) {
					continue
				}
				gone[loser.ID] = true
				merged++
			}
		}
	}

	return merged, deferred
}

// chooseSubsetWinner keeps the row with the better provenance, preferring the one the
// catalogue knows and then the fuller credit, since that is the one a listener would
// recognise.
func chooseSubsetWinner(a, b db.GetSongsForSubsetDedupeRow) (winner, loser db.GetSongsForSubsetDedupeRow) {
	if a.StmpdSlug.Valid != b.StmpdSlug.Valid {
		if a.StmpdSlug.Valid {
			return a, b
		}
		return b, a
	}
	if a.Lyrics.Valid != b.Lyrics.Valid {
		if a.Lyrics.Valid {
			return a, b
		}
		return b, a
	}

	na, nb := len(utils.SplitArtists(a.Artists)), len(utils.SplitArtists(b.Artists))
	if na != nb {
		if na > nb {
			return a, b
		}
		return b, a
	}

	if a.ID < b.ID {
		return a, b
	}
	return b, a
}
