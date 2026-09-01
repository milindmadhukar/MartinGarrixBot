// Command rekey-songs recomputes songs.match_key and songs.base_key for every row.
//
// The keys are derived in Go by utils/matchkey.go, so they cannot be filled in by a
// migration. Run this once after migration 000011, and again after any change to the
// normalization rules -- backfill-stmpd and link-remix-parents both depend on the
// keys being present and current.
//
// Idempotent: rows whose keys already match are left alone.
package main

import (
	"log/slog"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func main() {
	env, ctx, cleanup := script.Setup("rekey-songs")
	defer cleanup()

	rows, err := env.Queries.GetSongsForKeying(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}

	var changed, unchanged, flagged int
	prog := script.NewProgress("rekey songs", len(rows))
	for _, row := range rows {
		prog.Step()
		matchKey := utils.MatchKey(row.Name, "", row.MixName.String, row.Artists)
		baseKey := utils.BaseKey(row.Name, row.Artists)

		if env.DryRun {
			slog.Debug("would key song",
				slog.Int64("song_id", row.ID),
				slog.String("match_key", matchKey),
				slog.String("base_key", baseKey))
			changed++
			continue
		}

		// Collections are re-evaluated here too. The migration that first populated
		// the flag could only look at the title, so a DJ set called "Tomorrowland
		// 2016: The Elixir Of Life" was filed as a song -- its mix name and its
		// 29-minute running time are what give it away.
		if !row.IsCollection && utils.IsCollection(row.Name, row.MixName.String, row.LengthMs.Int32) {
			if env.DryRun {
				slog.Info("would flag as a collection",
					slog.Int64("song_id", row.ID), slog.String("name", row.Name),
					slog.String("mix", row.MixName.String))
				flagged++
			} else if n, err := env.Queries.SetSongCollection(ctx, db.SetSongCollectionParams{
				ID: row.ID, IsCollection: true,
			}); err != nil {
				script.Fatal("failed to flag a collection", err)
			} else if n > 0 {
				flagged++
				slog.Info("flagged as a collection",
					slog.Int64("song_id", row.ID), slog.String("name", row.Name),
					slog.String("mix", row.MixName.String))
			}
		}

		n, err := env.Queries.SetSongKeys(ctx, db.SetSongKeysParams{
			ID:       row.ID,
			MatchKey: utils.Text(matchKey),
			BaseKey:  utils.Text(baseKey),
		})
		if err != nil {
			script.Fatal("failed to write song keys", err)
		}
		if n > 0 {
			changed++
		} else {
			unchanged++
		}
	}

	prog.Done()

	slog.Info("Rekey complete",
		slog.Int("total", len(rows)),
		slog.Int("written", changed),
		slog.Int("already_current", unchanged),
		slog.Int("newly_flagged_as_collections", flagged))
}
