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
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
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

	prog := script.NewProgress("link remixes", len(byTitle))
	for _, group := range byTitle {
		prog.Step()
		if len(group) < 2 {
			continue
		}

		parentIdx := chooseParent(rows, group)
		if parentIdx < 0 {
			continue
		}
		parent := rows[parentIdx]

		for _, i := range group {
			r := rows[i]

			want := pgtype.Int8{}
			if i != parentIdx && isChildOf(parent, r) {
				want = pgtype.Int8{Int64: parent.ID, Valid: true}
			}

			// Never clear a parent link this run did not have grounds to set: a
			// row outside any group keeps whatever it has. Only rows inside a
			// group are re-decided.
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
				unchanged++
			case want.Valid:
				linked++
			default:
				cleared++
			}
		}
	}

	for _, r := range rows {
		if !looksInstrumental(r.Name, r.MixName.String) {
			continue
		}
		n, err := env.Queries.SetSongInstrumental(ctx, db.SetSongInstrumentalParams{
			ID: r.ID, IsInstrumental: true,
		})
		if err != nil {
			script.Fatal("failed to flag instrumental", err)
		}
		instrumental += int(n)
	}

	prog.Done()

	slog.Info("Parent linking complete",
		slog.Int("linked", linked),
		slog.Int("cleared", cleared),
		slog.Int("already_correct", unchanged),
		slog.Int("flagged_instrumental", instrumental))
}

// chooseParent picks the canonical row for a group of same-titled songs.
//
// The best parent is the one a user means when they name the song: no rendition of
// its own, the fewest artists (a remix adds the remixer), streaming links, and the
// earliest release. Ties break on id so the choice is stable across runs.
func chooseParent(rows []db.GetSongsForParentLinkingRow, group []int) int {
	candidates := append([]int(nil), group...)
	sort.SliceStable(candidates, func(a, b int) bool {
		ra, rb := rows[candidates[a]], rows[candidates[b]]

		// A collection is a release, not a song, so it must never become the entry a
		// song's remixes hang off. "Catharina (Remixes)" is a child of "Catharina",
		// not its parent.
		if ra.IsCollection != rb.IsCollection {
			return !ra.IsCollection
		}

		va := storedVariant(ra) != ""
		vb := storedVariant(rb) != ""
		if va != vb {
			return !va
		}

		na, nb := len(utils.SplitArtists(ra.Artists)), len(utils.SplitArtists(rb.Artists))
		if na != nb {
			return na < nb
		}

		la, lb := hasLinks(ra), hasLinks(rb)
		if la != lb {
			return la
		}

		// A known date beats an absent one; an unreleased row should not become the
		// canonical entry for a song that has actually come out.
		if ra.ReleaseDate.Valid != rb.ReleaseDate.Valid {
			return ra.ReleaseDate.Valid
		}
		if ra.ReleaseDate.String != rb.ReleaseDate.String {
			return ra.ReleaseDate.String < rb.ReleaseDate.String
		}
		return ra.ID < rb.ID
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

// isChildOf reports whether child is a rendition of parent: it must name a rendition,
// and its credits must contain the parent's.
func isChildOf(parent, child db.GetSongsForParentLinkingRow) bool {
	if storedVariant(child) == "" {
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
