package handlers

import (
	"log/slog"
	"time"
)

// announceWindow bounds how far back a release can be dated and still be worth
// announcing. It is the second of two independent locks on the announcement path:
// songs.announced_at is the primary watermark, and this window means that even a
// row which somehow arrives unstamped -- a failed identity match, a hand-inserted
// row, a restored backup -- cannot push a years-old release into the channel.
const announceWindow = 21 * 24 * time.Hour

// isRecentRelease reports whether a songs.release_date value is recent enough to
// announce. release_date is a TEXT column holding an ISO date; legacy rows carry
// "1970-01-01" or "<year>-01-01" placeholders, which fall outside the window and
// are therefore never announced. An unparseable date is treated as not recent:
// staying quiet on bad data is always the safe direction here.
func isRecentRelease(releaseDate string) bool {
	d, err := time.Parse(time.DateOnly, releaseDate)
	if err != nil {
		slog.Debug("Unparseable release_date, not announcing",
			slog.String("release_date", releaseDate))
		return false
	}
	return time.Since(d) <= announceWindow
}
