// Command backfill-artwork fills in missing cover art from Apple.
//
// Beatport is the obvious place to look and it is the wrong one: every row beatport
// knows about already has artwork, and not one of the rows missing it carries a
// beatport_id. Apple can reach them -- 136 of the 142 have an Apple link, and the
// lookup returns a cover whose size is part of the URL, so it can be requested at
// embed resolution rather than as a 100px thumbnail.
//
// Where a row has no Apple id, the same verified search used for release dates finds
// one; an unverified search result would attach the wrong artwork to the song.
//
// Idempotent, and never announces.
package main

import (
	"context"
	"log/slog"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func main() {
	env, ctx, cleanup := script.Setup("backfill-artwork")
	defer cleanup()

	rows, err := env.Queries.GetSongsMissingArtwork(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}
	slog.Info("Songs with no cover art", slog.Int("count", len(rows)))

	client := utils.NewItunesClient()
	var resolved, viaSearch, notFound, failed int
	prog := script.NewProgress("resolve artwork", len(rows))

	for _, row := range rows {
		prog.Step()

		result, bySearch, err := lookupArtwork(ctx, client, row)
		if err != nil {
			slog.Error("lookup failed", slog.Int64("song_id", row.ID), slog.Any("err", err))
			failed++
			continue
		}
		if result == nil || result.Artwork() == "" {
			notFound++
			continue
		}
		if bySearch {
			viaSearch++
		}

		art := result.Artwork()
		if env.DryRun {
			slog.Info("would set artwork",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.String("apple_says", result.ArtistName+" - "+result.Title()),
				slog.String("url", art))
		}

		n, err := env.Queries.SetSongArtwork(ctx, db.SetSongArtworkParams{
			ID: row.ID, ThumbnailUrl: utils.Text(art),
		})
		if err != nil {
			slog.Error("failed to set artwork", slog.Int64("song_id", row.ID), slog.Any("err", err))
			failed++
			continue
		}
		if n > 0 {
			resolved++
		}
	}
	prog.Done()

	slog.Info("Artwork backfill complete",
		slog.Int("songs_missing_art", len(rows)),
		slog.Int("resolved", resolved),
		slog.Int("of_those_by_search", viaSearch),
		slog.Int("apple_had_nothing", notFound),
		slog.Int("failed", failed))
}

// lookupArtwork prefers the id already on the row, and falls back to a search whose
// result is checked against the row before it is trusted.
func lookupArtwork(ctx context.Context, client *utils.ItunesClient, row db.GetSongsMissingArtworkRow) (*utils.ItunesResult, bool, error) {
	if id := utils.AppleIDFromURL(row.AppleMusicUrl.String); id != "" && !utils.IsApplePlaylistURL(row.AppleMusicUrl.String) {
		r, err := client.Lookup(ctx, id)
		if err != nil {
			return nil, false, err
		}
		if r != nil && r.Artwork() != "" {
			return r, false, nil
		}
	}

	results, err := client.Search(ctx, row.Artists+" "+row.Name, 12)
	if err != nil {
		return nil, false, err
	}
	for i := range results {
		r := results[i]
		if r.Artwork() == "" {
			continue
		}
		if utils.SameRecording(row.Name, row.Artists, r.Title(), r.ArtistName) {
			return &results[i], true, nil
		}
	}
	return nil, false, nil
}
