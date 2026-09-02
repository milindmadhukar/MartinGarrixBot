package utils

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// Writing a lyrics decision back is shared between the one-off sweep and the daily
// watcher, so that the two cannot disagree about what a miss costs a row or about
// whether an instrumental is allowed to retire itself.

// LyricsTally counts what a pass did, for its closing summary.
type LyricsTally struct {
	Filled        int
	Instrumentals int
	FannedOut     int // remix rows that inherited the words
	Missing       int
	Rejected      int
}

// LyricsRow is the part of the backlog query this package needs. Declared here rather
// than taking db.GetSongsMissingLyricsRow so that the applier does not have to change
// shape every time that query's select list does.
type LyricsRow struct {
	ID    int64
	Name  string
	Query LyricsQuery
}

// ApplyLyrics writes the outcome of one LRCLIB lookup.
//
// Every branch stamps lrclib_checked_at, including the ones that found nothing. A row
// nobody stamped is indistinguishable from a row nobody has reached, so the backlog
// query would hand it back on every cycle forever.
func ApplyLyrics(ctx context.Context, q *db.Queries, row LyricsRow, res LyricsResult, tally *LyricsTally) {
	switch res.Outcome {
	case LyricsFound:
		n, err := q.SetSongLyrics(ctx, db.SetSongLyricsParams{
			ID:       row.ID,
			Lyrics:   Text(res.Record.Plain()),
			LrclibID: lrclibID(res.Record.ID),
		})
		if err != nil {
			slog.Error("Failed to store lyrics",
				slog.Int64("song_id", row.ID), slog.Any("err", err))
			return
		}
		if n == 0 {
			// The guard on lyrics IS NULL held: somebody entered words for this row
			// between the query and the write. Theirs win.
			slog.Debug("Row gained lyrics while we were fetching, leaving them",
				slog.Int64("song_id", row.ID))
			return
		}

		tally.Filled++
		slog.Info("Filled in lyrics from LRCLIB",
			slog.Int64("song_id", row.ID), slog.String("name", row.Name),
			slog.Int64("lrclib_id", res.Record.ID),
			slog.String("matched", res.Record.TrackName+" -- "+res.Record.ArtistName))

		// A remix of a vocal track has the same words. CopyLyricsToRemixes exists for
		// exactly this and has never had a caller; without it every remix row stays in
		// the backlog and is fetched separately, which is the same answer at several
		// times the cost.
		fanned, err := q.CopyLyricsToRemixes(ctx, row.ID)
		if err != nil {
			slog.Error("Failed to copy lyrics to renditions",
				slog.Int64("song_id", row.ID), slog.Any("err", err))
			return
		}
		tally.FannedOut += int(fanned)

	case LyricsInstrumental:
		n, err := q.MarkSongInstrumentalFromLrclib(ctx, db.MarkSongInstrumentalFromLrclibParams{
			ID: row.ID, LrclibID: lrclibID(res.Record.ID),
		})
		if err != nil {
			slog.Error("Failed to flag a song as instrumental",
				slog.Int64("song_id", row.ID), slog.Any("err", err))
			return
		}
		if n > 0 {
			tally.Instrumentals++
			slog.Info("LRCLIB says this recording has no words",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.Int64("lrclib_id", res.Record.ID))
		}

	case LyricsMissing:
		if err := q.MarkLrclibMiss(ctx, row.ID); err != nil {
			slog.Error("Failed to record an LRCLIB miss",
				slog.Int64("song_id", row.ID), slog.Any("err", err))
			return
		}
		tally.Missing++

	case LyricsRejected:
		// The candidate did not describe this row. Stamp the attempt but do not spend
		// one of the row's four tries: the row is fine, the answer was not.
		if err := q.MarkLrclibChecked(ctx, row.ID); err != nil {
			slog.Error("Failed to stamp an LRCLIB attempt",
				slog.Int64("song_id", row.ID), slog.Any("err", err))
			return
		}
		tally.Rejected++
		if res.Record != nil {
			slog.Debug("Rejected an LRCLIB result as a different recording",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.String("candidate", res.Record.TrackName+" -- "+res.Record.ArtistName))
		}
	}
}

// LyricsRowFor builds the applier's view of a backlog row.
//
// normalized_name is preferred over the stored name and falls back to deriving it, the
// same way the quiz does: LRCLIB indexes titles, so a row the keying pass has not
// reached must still ask about "Breach" rather than "Breach (Walk Alone)".
func LyricsRowFor(id int64, name, normalized, artists, album string, lengthMs int32) LyricsRow {
	title := normalized
	if title == "" {
		title = NormalizedTitle(name)
	}
	return LyricsRow{
		ID:   id,
		Name: name,
		Query: LyricsQuery{
			Title:    title,
			Name:     name,
			Artists:  artists,
			Album:    album,
			LengthMs: lengthMs,
		},
	}
}

// lrclibID wraps a record id as a nullable column value. Zero is not a real LRCLIB id,
// so it reads as NULL -- which is what "hand-entered" means on this column.
func lrclibID(id int64) pgtype.Int8 {
	return pgtype.Int8{Int64: id, Valid: id != 0}
}
