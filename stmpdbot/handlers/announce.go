package handlers

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// announceWindow bounds how far back a release can be dated and still be worth
// announcing. It is the second of two independent locks on the announcement path:
// songs.announced_at is the primary watermark, and this window means that even a
// row which somehow arrives unstamped -- a failed identity match, a hand-inserted
// row, a restored backup -- cannot push a years-old release into the channel.
const announceWindow = 21 * 24 * time.Hour

// isRecentRelease reports whether a songs.release_date is recent enough to announce.
//
// A NULL date is never recent. It means either that the song is unreleased -- someone
// added it because they heard it played -- or that we could not establish a date, and
// neither is something to announce. An unparseable date is treated the same way:
// staying quiet on bad data is always the safe direction here.
func isRecentRelease(releaseDate pgtype.Text) bool {
	if !releaseDate.Valid || releaseDate.String == "" {
		return false
	}

	d, err := time.Parse(time.DateOnly, releaseDate.String)
	if err != nil {
		slog.Debug("Unparseable release_date, not announcing",
			slog.String("release_date", releaseDate.String))
		return false
	}
	return time.Since(d) <= announceWindow
}
