package handlers

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// finaliseNewSong fills in the derived columns on a row the fetchers have just
// inserted: its match keys, whether it is a release rather than a track, and which
// song it is a rendition of.
//
// None of this was happening. The insert queries write only what the source gave
// them, and everything derived was set by the maintenance scripts -- so between runs
// a new row had no keys, no collection flag and no parent. The visible consequence is
// that a newly published EP counts as a song and the radio tries to stream it end to
// end; the quieter one is that every new remix shows up as its own entry in
// autocomplete until someone remembers to run a script.
//
// Doing it here is what lets the scripts stay one-off repairs rather than a chore.
func finaliseNewSong(ctx context.Context, b *stmpdbot.STMPDBot, song db.Song, index *utils.SongIndex) {
	matchKey := utils.MatchKey(song.Name, "", song.MixName.String, song.Artists)
	baseKey := utils.BaseKey(song.Name, song.Artists)

	if _, err := b.Queries.SetSongKeys(ctx, db.SetSongKeysParams{
		ID: song.ID, MatchKey: utils.Text(matchKey), BaseKey: utils.Text(baseKey),
	}); err != nil {
		slog.Error("Failed to key a new song", slog.Int64("song_id", song.ID), slog.Any("err", err))
	}

	if utils.IsCollectionName(song.Name) {
		if _, err := b.Queries.SetSongCollection(ctx, db.SetSongCollectionParams{
			ID: song.ID, IsCollection: true,
		}); err != nil {
			slog.Error("Failed to flag a new collection", slog.Int64("song_id", song.ID), slog.Any("err", err))
			return
		}
		slog.Info("New release is a collection, not a track",
			slog.Int64("song_id", song.ID), slog.String("name", song.Name))
		return
	}

	// A rendition belongs under the song it derives from, if that song is here.
	if _, variant := utils.SplitVariant(song.Name, "", song.MixName.String); variant == "" {
		return
	}

	parent := index.FindCanonical(baseKey, song.ID)
	if parent == nil {
		return
	}

	if _, err := b.Queries.SetSongParent(ctx, db.SetSongParentParams{
		ID: song.ID, ParentSongID: pgtype.Int8{Int64: parent.ID, Valid: true},
	}); err != nil {
		slog.Error("Failed to file a new rendition under its song",
			slog.Int64("song_id", song.ID), slog.Any("err", err))
		return
	}
	slog.Info("Filed a new rendition under its song",
		slog.Int64("song_id", song.ID), slog.String("name", song.Name),
		slog.Int64("parent_id", parent.ID), slog.String("parent", parent.Name))
}
