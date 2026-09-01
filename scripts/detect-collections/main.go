// Command detect-collections finds rows that are releases rather than tracks and that
// no naming rule can catch.
//
// "Seven" is the Martin Garrix EP. Nothing about the row says so: the title carries no
// marker, there is no mix name, no running time and no beatport release name. Only the
// catalogue it came from knows, and the STMPD dataset does not record it either.
//
// Apple does. Asking about the *release* an Apple link points at -- not the track
// within it -- returns "Seven - EP" with a track count of seven, where a genuine
// single comes back with one or two.
//
// The count alone would be wrong: a track sitting on an eight-track album would
// qualify. So a row is only flagged when it is named after the release itself, which
// is what distinguishes "Seven the EP" from "Ocean, a song on it".
package main

import (
	"log/slog"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func main() {
	env, ctx, cleanup := script.Setup("detect-collections")
	defer cleanup()

	rows, err := env.Queries.GetSongsToCheckForCollection(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}
	slog.Info("Rows to check", slog.Int("count", len(rows)))

	client := utils.NewItunesClient()
	var flagged, checked, skipped int
	prog := script.NewProgress("check releases", len(rows))

	for _, row := range rows {
		prog.Step()

		id := utils.AppleAlbumIDFromURL(row.AppleMusicUrl.String)
		if id == "" {
			skipped++
			continue
		}

		result, err := client.Lookup(ctx, id)
		if err != nil {
			slog.Error("lookup failed", slog.Int64("song_id", row.ID), slog.Any("err", err))
			continue
		}
		checked++
		if result == nil || !result.IsMultiTrackRelease() {
			continue
		}

		// Only when the row is named after the release, not after a track on it.
		if utils.NormalizeToken(row.Name) != utils.NormalizeToken(result.CollectionTitle()) {
			continue
		}

		if env.DryRun {
			slog.Info("would flag as a release",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.String("apple", result.CollectionName), slog.Int("tracks", result.TrackCount))
			flagged++
			continue
		}

		n, err := env.Queries.SetSongCollection(ctx, db.SetSongCollectionParams{
			ID: row.ID, IsCollection: true,
		})
		if err != nil {
			slog.Error("failed to flag", slog.Int64("song_id", row.ID), slog.Any("err", err))
			continue
		}
		if n > 0 {
			flagged++
			slog.Info("flagged as a release, not a track",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.String("apple", result.CollectionName), slog.Int("tracks", result.TrackCount))
		}
	}
	prog.Done()

	slog.Info("Collection detection complete",
		slog.Int("rows", len(rows)), slog.Int("checked", checked),
		slog.Int("flagged", flagged), slog.Int("no_apple_id", skipped))
}
