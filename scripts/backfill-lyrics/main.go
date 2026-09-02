// Command backfill-lyrics fills songs.lyrics from LRCLIB.
//
// Lyrics have only ever been entered by hand through psql, so 79 rows out of 1348 have
// them -- and that handful is the entire pool the quiz draws from. LRCLIB
// (https://lrclib.net) is a free, keyless community lyrics database with good coverage
// of this catalogue.
//
// This is the first sweep. The bot's own daily watcher works the same queue in batches
// of sixty, which would take weeks to reach the end of it; this walks the whole backlog
// in one pass, at LRCLIB's requested pacing of one request every 500ms.
//
// Run it with -dry-run first and read the log. Every fill is printed with the record it
// came from, and this is the run most likely to hang the wrong words on a song: a cover
// version, a live cut and a sped-up edit all share the title and the artist exactly.
// The dry run executes the writes inside a transaction that is rolled back, so what you
// read is what would have happened.
//
// Idempotent: a row that has words is never asked about again, and one LRCLIB has
// nothing for is retired after four misses on a widening schedule.
//
// Requires migrations through 000022, and rekey-songs -- LRCLIB is queried with
// songs.normalized_name, because it indexes titles and not credit strings.
package main

import (
	"errors"
	"log/slog"

	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// sweepLimit bounds one run. Comfortably above the whole backlog, so it is a runaway
// guard rather than a page size -- the query is ordered, so a truncated run would
// silently always work the same prefix.
const sweepLimit = 5000

func main() {
	env, ctx, cleanup := script.Setup("backfill-lyrics")
	defer cleanup()

	rows, err := env.Queries.GetSongsMissingLyrics(ctx, sweepLimit)
	if err != nil {
		script.Fatal("failed to load the lyrics backlog", err)
	}
	slog.Info("Songs with no lyrics", slog.Int("count", len(rows)))
	if len(rows) == sweepLimit {
		slog.Warn("Hit the per-run limit; run again to continue",
			slog.Int("limit", sweepLimit))
	}

	client := utils.NewLrclibClient()
	var tally utils.LyricsTally
	var failed int

	prog := script.NewProgress("fetch lyrics", len(rows))
	for _, row := range rows {
		prog.Step()

		target := utils.LyricsRowFor(row.ID, row.Name, row.NormalizedName.String,
			row.Artists, row.ReleaseName.String, row.LengthMs.Int32)

		res, err := utils.FetchLyrics(ctx, client, target.Query)
		if err != nil {
			// Being rate limited is not something to push through. LRCLIB's
			// documentation says continuing may earn a ban, and the schedule this
			// writes means a second run picks up exactly where this one stopped.
			var limited utils.ErrLrclibRateLimited
			if errors.As(err, &limited) {
				slog.Error("LRCLIB rate limited us; stopping. Run again later.",
					slog.Duration("retry_after", limited.RetryAfter))
				break
			}
			if ctx.Err() != nil {
				slog.Error("Out of time; run again to continue", slog.Any("err", ctx.Err()))
				break
			}
			slog.Warn("lookup failed",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.Any("err", err))
			failed++
			continue
		}

		utils.ApplyLyrics(ctx, env.Queries, target, res, &tally)
	}
	prog.Done()

	slog.Info("Lyrics backfill complete",
		slog.Int("considered", len(rows)),
		slog.Int("filled", tally.Filled),
		slog.Int("copied_to_renditions", tally.FannedOut),
		slog.Int("flagged_instrumental", tally.Instrumentals),
		slog.Int("lrclib_had_nothing", tally.Missing),
		slog.Int("rejected_as_a_different_recording", tally.Rejected),
		slog.Int("lookup_failed", failed))
}
