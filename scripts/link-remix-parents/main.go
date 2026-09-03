// Command link-remix-parents points every remix row at the canonical row for the
// same song, and flags rows that are instrumentals.
//
// Beatport lists each remix as its own track, so one song becomes many rows:
// "Catharina" is six in production, "Told You So" is ten. Every row is a real
// distinct recording -- its own beatport id, BPM and length -- so none are deleted.
// What changes is presentation: parent_song_id is what /links autocomplete, /lyrics,
// /quiz and the radio rotation filter on, so the catalogue reads as one entry per
// song again.
//
// Grouping is by title plus artist containment rather than by base_key. Beatport
// credits a remix to the original artist plus the remixer, so the artist sets differ
// by construction and a base_key match would never fire.
//
// Run rekey-songs first. Idempotent: rows already pointing at the right parent are
// left alone.
package main

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

// instrumentalMarkers appear in a mix name or title when a track has no vocal to
// have lyrics for. Flagging them keeps them out of the "missing lyrics" backlog and
// stops /quiz asking a player to recall words that do not exist.
var instrumentalMarkers = []string{"instrumental", "dub mix", "drumless"}

func main() {
	env, ctx, cleanup := script.Setup("link-remix-parents")
	defer cleanup()

	rows, err := env.Queries.GetSongsForParentLinking(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}
	slog.Info("Loaded songs", slog.Int("count", len(rows)))

	byTitle := map[string][]int{}
	for i, r := range rows {
		title := utils.TitleKey(r.Name, "", r.MixName.String)
		if title == "" {
			continue
		}
		byTitle[title] = append(byTitle[title], i)
	}

	var linked, cleared, unchanged, instrumental int

	// A row cannot be its own remix. Two rows had been left pointing at themselves by
	// an earlier version of this pass, and neither was reachable by the grouping below
	// -- each was the only row with its title, so the group was skipped before any
	// linking decision was made and the bad pointer survived every later run.
	for _, r := range rows {
		if r.ParentSongID.Valid && r.ParentSongID.Int64 == r.ID {
			n, err := env.Queries.SetSongParent(ctx, db.SetSongParentParams{ID: r.ID})
			if err != nil {
				script.Fatal("failed to unlink a self-parented row", err)
			}
			if n > 0 {
				cleared++
				slog.Info("unlinked a row that was its own parent",
					slog.Int64("song_id", r.ID), slog.String("name", r.Name))
			}
		}
	}

	prog := script.NewProgress("link remixes", len(byTitle))
	for _, group := range byTitle {
		prog.Step()
		if len(group) < 2 {
			continue
		}

		// A title on its own is not a song. "Aurora" is a Martin Garrix & Blinders
		// record and, separately, an Aspyer one; grouping on the title alone put all
		// three rows together, elected the Aspyer row as the parent, and then linked
		// nothing because its credits matched neither of the others. The pair that
		// really was one song stayed split.
		//
		// So the title group is first split into clusters that agree on who made the
		// song, and each cluster gets its own parent.
		for _, cluster := range clusterByCredit(rows, group) {
			linkCluster(ctx, env, rows, cluster, &linked, &cleared, &unchanged)
		}
	}

	prog.Done()

	slog.Info("Parent linking complete",
		slog.Int("linked", linked),
		slog.Int("cleared", cleared),
		slog.Int("already_correct", unchanged),
		slog.Int("flagged_instrumental", instrumental))
}

// candidateOf reduces a row to what decides whether it is the one a listener means.
func candidateOf(r db.GetSongsForParentLinkingRow) catalogue.Candidate {
	return catalogue.Candidate{
		ID:             r.ID,
		IsCollection:   r.IsCollection,
		NamesRendition: storedVariant(r) != "",
		HasSlug:        r.StmpdSlug.Valid,
		HasLyrics:      r.Lyrics.Valid,
		HasLinks:       hasLinks(r),
		ArtistCount:    len(utils.SplitArtists(r.Artists)),
		ReleaseDate:    r.ReleaseDate.String,
	}
}

// chooseParent picks the canonical row for a group of same-titled songs.
//
// The ordering lives in catalogue.BetterCanonical, shared with the dedupe pass. It used
// to live here, in a second copy that ranked "names no rendition" and "fewest artists"
// above having any streaming links at all -- so a beatport "Original Mix" with no links
// beat the STMPD "Radio Edit" that had YouTube, Spotify and the lyrics, and every
// listener asking for that song got a card with no buttons on it.
func chooseParent(rows []db.GetSongsForParentLinkingRow, group []int) int {
	candidates := append([]int(nil), group...)
	sort.SliceStable(candidates, func(a, b int) bool {
		return catalogue.BetterCanonical(candidateOf(rows[candidates[a]]), candidateOf(rows[candidates[b]]))
	})

	best := candidates[0]

	if rows[best].IsCollection {
		return -1
	}

	// A group whose best candidate is itself a rendition has no original in the
	// table -- "Crash Land" exists only as a Sacha Robotti remix and a Rootkit remix.
	// Leaving it flat means the song appears twice in autocomplete under the same
	// name, which is the thing this pass exists to stop. Elect the best of them as
	// the entry for the song: the sort above has already preferred the row with the
	// catalogue's slug, the lyrics and the links, which is the one a listener means.
	return best
}

// isChildOf reports whether child belongs under parent.
//
// The credits must contain the parent's -- clustering has already established that the
// two share a title, and containment is what makes the child a rendition of this song
// rather than a different song by another act with the same name.
//
// Naming a rendition is normally required as well: a row that names none is the song
// itself, not a version of it. That test is a proxy, though, and it only holds while the
// elected parent is the plain row. Once provenance elects a rendition as canonical --
// because it is the row carrying the label's slug, the lyrics and the links, or because
// the cluster has no plain original at all -- a plain-named sibling is still the same
// song and still belongs underneath.
//
// Without this the two rows for "Break Through The Silence" ended up as two canonical
// rows rather than one: the STMPD "Radio Edit" was elected, and the beatport
// "Original Mix" failed the rendition test, so it was unfiled instead of re-filed and
// the song appeared twice in autocomplete.
func isChildOf(parent, child db.GetSongsForParentLinkingRow) bool {
	if storedVariant(child) == "" && storedVariant(parent) == "" {
		return false
	}
	return utils.ArtistsSubsume(child.Artists, parent.Artists)
}

func storedVariant(r db.GetSongsForParentLinkingRow) string {
	_, variant := utils.SplitVariant(r.Name, "", r.MixName.String)
	return variant
}

func hasLinks(r db.GetSongsForParentLinkingRow) bool {
	return r.SpotifyUrl.Valid || r.YoutubeUrl.Valid || r.AppleMusicUrl.Valid
}

func looksInstrumental(name, mixName string) bool {
	haystack := strings.ToLower(name + " " + mixName)
	for _, marker := range instrumentalMarkers {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

// clusterByCredit splits rows that share a title into groups that are plausibly the
// same song, by requiring the credits to be compatible rather than merely co-titled.
//
// The seed of each cluster is the row with the fewest artists -- the base credit --
// and a row joins if its artists contain the seed's. That is the same containment the
// dedupe pass uses: a remix credits the original artist plus the remixer, so the
// original is always the smaller set.
func clusterByCredit(rows []db.GetSongsForParentLinkingRow, group []int) [][]int {
	remaining := append([]int(nil), group...)
	sort.SliceStable(remaining, func(a, b int) bool {
		return len(utils.SplitArtists(rows[remaining[a]].Artists)) <
			len(utils.SplitArtists(rows[remaining[b]].Artists))
	})

	var clusters [][]int
	used := make(map[int]bool, len(remaining))

	for _, seed := range remaining {
		if used[seed] {
			continue
		}
		cluster := []int{seed}
		used[seed] = true

		for _, other := range remaining {
			if used[other] {
				continue
			}
			if utils.ArtistsSubsume(rows[other].Artists, rows[seed].Artists) {
				cluster = append(cluster, other)
				used[other] = true
			}
		}
		clusters = append(clusters, cluster)
	}

	return clusters
}

// linkCluster files every rendition in a cluster under the cluster's canonical row.
func linkCluster(ctx context.Context, env *script.Env, rows []db.GetSongsForParentLinkingRow,
	cluster []int, linked, cleared, unchanged *int) {
	if len(cluster) < 2 {
		return
	}

	parentIdx := chooseParent(rows, cluster)
	if parentIdx < 0 {
		return
	}
	parent := rows[parentIdx]

	for _, i := range cluster {
		r := rows[i]

		want := pgtype.Int8{}
		if i != parentIdx && isChildOf(parent, r) {
			want = pgtype.Int8{Int64: parent.ID, Valid: true}
		}

		// Never clear a link this run had no grounds to set: a row outside any
		// cluster keeps whatever it has.
		if !want.Valid && !r.ParentSongID.Valid {
			continue
		}

		n, err := env.Queries.SetSongParent(ctx, db.SetSongParentParams{
			ID: r.ID, ParentSongID: want,
		})
		if err != nil {
			script.Fatal("failed to set parent", err)
		}
		switch {
		case n == 0:
			*unchanged++
		case want.Valid:
			*linked++
			slog.Info("filed a rendition under its song",
				slog.Int64("song_id", r.ID), slog.String("name", r.Name),
				slog.String("mix", r.MixName.String), slog.Int64("parent_id", parent.ID))
		default:
			*cleared++
		}
	}
}
