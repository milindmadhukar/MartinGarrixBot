// Command backfill-dates replaces the 1970-01-01 placeholder release dates with real
// ones, resolved from Apple's public lookup API.
//
// The placeholder is what the original importer wrote whenever it had no date. It is
// not harmless: release_date drives the announcement recency window, the "released"
// footer on a track card, and any date ordering, so a row stuck at 1970 sorts to the
// bottom of the catalogue forever.
//
// Resolution uses the numeric id already embedded in each row's own apple_music_url,
// not a search -- so the date returned belongs to the exact recording the row already
// links to, and no fuzzy matching is involved. Rows with no Apple link cannot be
// resolved this way and are reported, not guessed at.
//
// Idempotent, and never announces: this binary does not import the notifier.
package main

import (
	"context"
	"log/slog"
	"strings"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func main() {
	env, ctx, cleanup := script.Setup("backfill-dates")
	defer cleanup()

	rows, err := env.Queries.GetSongsWithPlaceholderDate(ctx)
	if err != nil {
		script.Fatal("failed to load songs with placeholder dates", err)
	}
	slog.Info("Songs carrying the 1970-01-01 placeholder", slog.Int("count", len(rows)))

	client := utils.NewItunesClient()
	var resolved, viaSearch, unresolvable, notFound, merged, failed, suspicious, playlist int

	for _, row := range rows {
		if utils.IsApplePlaylistURL(row.AppleMusicUrl.String) {
			playlist++
			slog.Warn("apple link points at a playlist, not a release - the button sends users to the wrong place",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name))
		}

		var result *utils.ItunesResult
		bySearch := false

		// Preferred path: the id already stored on the row. It names one exact
		// recording, so there is nothing to verify.
		if id := utils.AppleIDFromURL(row.AppleMusicUrl.String); id != "" && !utils.IsApplePlaylistURL(row.AppleMusicUrl.String) {
			r, err := client.Lookup(ctx, id)
			if err != nil {
				slog.Error("lookup failed",
					slog.Int64("song_id", row.ID), slog.String("apple_id", id), slog.Any("err", err))
				failed++
				continue
			}
			result = r
		}

		// Fallback: search by name and artists. This is a guess and is checked.
		if result == nil || result.Date() == "" {
			r, err := searchForRow(ctx, client, row)
			if err != nil {
				slog.Error("search failed",
					slog.Int64("song_id", row.ID), slog.String("name", row.Name), slog.Any("err", err))
				failed++
				continue
			}
			if r != nil {
				result, bySearch = r, true
			}
		}

		if result == nil {
			notFound++
			slog.Warn("no release found for this song",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name), slog.String("artists", row.Artists))
			continue
		}

		date := result.Date()
		if date == "" {
			notFound++
			continue
		}
		if bySearch {
			viaSearch++
		}

		// The id came from this row, so a mismatch means the stored link is wrong,
		// not that the lookup is. Worth surfacing, not worth refusing: the date
		// still belongs to whatever the row actually links to.
		if !titlesAgree(row.Name, result.Title()) {
			suspicious++
			slog.Warn("stored link points at a differently-titled recording",
				slog.Int64("song_id", row.ID),
				slog.String("stored", row.Name),
				slog.String("apple", result.Title()),
				slog.String("apple_artist", result.ArtistName),
				slog.String("date", date))
		}

		if env.DryRun {
			slog.Info("would set release date",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.String("artists", row.Artists), slog.String("date", date),
				slog.String("via", source(bySearch)),
				slog.String("apple_says", result.ArtistName+" - "+result.Title()))
			resolved++
			continue
		}

		n, err := env.Queries.SetSongReleaseDate(ctx, db.SetSongReleaseDateParams{
			ID: row.ID, ReleaseDate: utils.Text(date),
		})
		if err != nil {
			// The corrected date collided with unique_release, so a twin row already
			// holds this song at its real date. Fold this row into that one.
			if db.ErrorCode(err) == db.UniqueViolation {
				if mergeInto(ctx, env, row, date) {
					merged++
				} else {
					failed++
				}
				continue
			}
			slog.Error("failed to set release date",
				slog.Int64("song_id", row.ID), slog.Any("err", err))
			failed++
			continue
		}
		if n > 0 {
			resolved++
			slog.Info("release date resolved",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.String("date", date))
		}
	}

	slog.Info("Date backfill complete",
		slog.Int("placeholder_rows", len(rows)),
		slog.Int("resolved", resolved),
		slog.Int("of_those_by_search", viaSearch),
		slog.Int("merged_into_twin", merged),
		slog.Int("no_apple_link", unresolvable),
		slog.Int("apple_link_is_a_playlist", playlist),
		slog.Int("apple_had_no_record", notFound),
		slog.Int("title_mismatch_warnings", suspicious),
		slog.Int("failed", failed))
}

// mergeInto folds row into the row that already holds this song at its real date.
func mergeInto(ctx context.Context, env *script.Env, row db.GetSongsWithPlaceholderDateRow, date string) bool {
	twin, err := env.Queries.GetSong(ctx, db.GetSongParams{
		Name: row.Name, Artists: row.Artists, ReleaseDate: utils.Text(date),
	})
	if err != nil {
		slog.Error("date collided but no twin row was found",
			slog.Int64("song_id", row.ID), slog.String("name", row.Name),
			slog.String("date", date), slog.Any("err", err))
		return false
	}

	slog.Info("merging placeholder row into its dated twin",
		slog.String("name", row.Name), slog.Int64("keep", twin.ID), slog.Int64("drop", row.ID))

	if err := env.Queries.MergeSongRows(ctx, db.MergeSongRowsParams{
		WinnerID: twin.ID, LoserID: row.ID,
	}); err != nil {
		slog.Error("failed to merge", slog.Int64("song_id", row.ID), slog.Any("err", err))
		return false
	}
	if err := env.Queries.DeleteSong(ctx, row.ID); err != nil {
		slog.Error("failed to delete merged row", slog.Int64("song_id", row.ID), slog.Any("err", err))
		return false
	}
	return true
}

// titlesAgree is a loose sanity check on the stored link, not a match gate.
func titlesAgree(stored, apple string) bool {
	a, _ := utils.SplitVariant(stored, "", "")
	b, _ := utils.SplitVariant(apple, "", "")
	if a == "" || b == "" {
		return true
	}
	return strings.Contains(a, b) || strings.Contains(b, a) || utils.IsCloseMatch(a, b, 0.80)
}

// searchForRow asks Apple for a song by name and artists, and returns a result only
// if it actually describes this row.
//
// Search always answers with its nearest match, even when Apple holds nothing for the
// query, so an unverified result is worse than none: it writes a confident, wrong
// date. The earliest verified match wins, since a song's own release predates the
// compilations and re-issues it later appears on.
func searchForRow(ctx context.Context, client *utils.ItunesClient, row db.GetSongsWithPlaceholderDateRow) (*utils.ItunesResult, error) {
	results, err := client.Search(ctx, row.Artists+" "+row.Name, 12)
	if err != nil {
		return nil, err
	}

	var best *utils.ItunesResult
	for i := range results {
		r := results[i]
		if r.Date() == "" || !utils.SameRecording(row.Name, row.Artists, r.Title(), r.ArtistName) {
			continue
		}
		if best == nil || r.Date() < best.Date() {
			best = &results[i]
		}
	}
	return best, nil
}

func source(bySearch bool) string {
	if bySearch {
		return "search (verified)"
	}
	return "stored apple id"
}
