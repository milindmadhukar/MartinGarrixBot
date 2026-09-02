package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// lyricsBatchSize bounds how much work one cycle does.
//
// Small on purpose. The one-off sweep in scripts/backfill-lyrics is what drains the
// backlog; this exists to catch what arrives afterwards -- a release published this
// week whose words reach LRCLIB a fortnight later -- and the queue it works is measured
// in tens of rows, not hundreds. Sixty rows at 500ms is half a minute of requests a
// day.
const lyricsBatchSize = 60

// GetSongLyrics fills in words for songs that have none.
//
// Lyrics have only ever been entered by hand, through psql, which is why 79 rows out of
// 1348 have them and why the quiz has been drawing from the same handful of songs for
// as long as it has existed. LRCLIB needs no key and no account, and every candidate is
// verified against the row it is meant to describe before anything is written -- an
// unverified search result hangs a cover version's words on a real song, and nobody
// would ever notice.
func GetSongLyrics(b *stmpdbot.STMPDBot, ticker *time.Ticker) {
	client := utils.NewLrclibClient()

	for ; ; <-ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		tally, err := runLyricsCycle(ctx, b, client)
		cancel()

		if err != nil {
			utils.RecordSourceFailure(utils.SourceLyrics, err)
			slog.Error("Lyrics backfill cycle failed", slog.Any("err", err))
			continue
		}

		// An empty backlog is a success. It is the goal, and treating it as a failure
		// would report the source degraded exactly when it has finished its job --
		// the same reasoning SourceAnniversary records in sourcehealth.go.
		utils.RecordSourceSuccess(utils.SourceLyrics)

		if tally.Filled > 0 || tally.Instrumentals > 0 {
			slog.Info("Lyrics backfill complete",
				slog.Int("filled", tally.Filled),
				slog.Int("copied_to_renditions", tally.FannedOut),
				slog.Int("flagged_instrumental", tally.Instrumentals),
				slog.Int("not_found", tally.Missing))
		}
	}
}

func runLyricsCycle(ctx context.Context, b *stmpdbot.STMPDBot, client *utils.LrclibClient) (utils.LyricsTally, error) {
	var tally utils.LyricsTally

	rows, err := b.Queries.GetSongsMissingLyrics(ctx, lyricsBatchSize)
	if err != nil {
		return tally, err
	}

	for _, row := range rows {
		target := utils.LyricsRowFor(row.ID, row.Name, row.NormalizedName.String,
			row.Artists, row.ReleaseName.String, row.LengthMs.Int32)

		res, err := utils.FetchLyrics(ctx, client, target.Query)
		if err != nil {
			// A rate limit ends the batch rather than spending the rest of it
			// collecting 429s. LRCLIB's documentation says continuing through one may
			// earn a ban, and the next tick is only hours away.
			var limited utils.ErrLrclibRateLimited
			if errors.As(err, &limited) {
				slog.Warn("LRCLIB rate limited us, ending the batch",
					slog.Duration("retry_after", limited.RetryAfter))
				return tally, err
			}
			if ctx.Err() != nil {
				return tally, ctx.Err()
			}
			// One row failing at the HTTP layer says nothing about the next one, and
			// nothing about this row either -- so it is not stamped, and comes back
			// on the following cycle.
			slog.Warn("LRCLIB lookup failed",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.Any("err", err))
			continue
		}

		utils.ApplyLyrics(ctx, b.Queries, target, res, &tally)
	}

	return tally, nil
}
