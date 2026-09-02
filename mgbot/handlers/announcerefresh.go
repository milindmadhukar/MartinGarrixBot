package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// announcementRefreshBatch bounds one cycle.
//
// Generous, because the expensive part is the edit and almost no candidate needs one:
// the batch is a database read plus a string comparison per row, and only a row whose
// buttons actually changed costs a REST call.
const announcementRefreshBatch = 25

// announcementEditDelay paces the edits, matching the delay the notifier already leaves
// between messages.
const announcementEditDelay = 500 * time.Millisecond

// RefreshAnnouncements corrects the link buttons on announcements that were posted
// before the song's links existed.
//
// A release is announced within days of coming out; its streaming links arrive over the
// following weeks, from the STMPD sync, the hourly enrichment and the backfill scripts.
// The announcement is the most looked-at message the bot posts and it has always frozen
// with whatever links existed in its first fifteen minutes, because nothing remembered
// where it was.
//
// The embed is never touched -- only the components. What a release was called and what
// it looked like on the day is the record; which services carry it is not.
func RefreshAnnouncements(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	for ; ; <-ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		edited, err := refreshAnnouncementsOnce(ctx, b)
		cancel()

		if err != nil {
			slog.Error("Announcement refresh cycle failed", slog.Any("err", err))
			continue
		}
		if edited > 0 {
			slog.Info("Refreshed announcement buttons", slog.Int("edited", edited))
		}
	}
}

func refreshAnnouncementsOnce(ctx context.Context, b *mgbot.MartinGarrixBot) (int, error) {
	rows, err := b.Queries.GetAnnouncementsToRefresh(ctx, announcementRefreshBatch)
	if err != nil {
		return 0, err
	}

	edited := 0
	for _, row := range rows {
		if ctx.Err() != nil {
			return edited, ctx.Err()
		}

		want := utils.SongLinkSignature(row.Song)
		if want == row.ButtonsKey {
			// The overwhelmingly common case, and the reason this watcher is cheap:
			// a settled catalogue makes no REST calls at all. The signature covers
			// the buttons and nothing else, so the hourly artwork and release-date
			// enrichment -- which touches many of these same rows -- is invisible
			// here rather than triggering an edit every hour.
			continue
		}

		components := utils.GetSongButtonRows(row.Song)
		if len(components) == 0 && row.ButtonsKey != "" {
			// A song does not lose all its links. If it looks like it has, something
			// upstream is wrong, and a message as posted is better than a message
			// stripped of its buttons on bad data.
			slog.Warn("Song appears to have lost every link; leaving its announcement alone",
				slog.Int64("song_id", row.SongID), slog.String("name", row.Song.Name))
			continue
		}

		if err := editAnnouncement(ctx, b, row, components); err != nil {
			continue // already logged, and classified for the next cycle
		}

		if err := b.Queries.MarkAnnouncementEdited(ctx, db.MarkAnnouncementEditedParams{
			GuildID: row.GuildID, MessageID: row.MessageID, ButtonsKey: want,
		}); err != nil {
			slog.Error("Failed to record an announcement edit",
				slog.Int64("song_id", row.SongID), slog.Any("err", err))
			continue
		}

		edited++
		slog.Info("Added newly discovered links to an announcement",
			slog.Int64("song_id", row.SongID), slog.String("name", row.Song.Name),
			slog.Uint64("message_id", uint64(row.MessageID)))

		select {
		case <-time.After(announcementEditDelay):
		case <-ctx.Done():
			return edited, ctx.Err()
		}
	}

	return edited, nil
}

// editAnnouncement rewrites one message's components, and decides what a refusal means
// for the row.
func editAnnouncement(ctx context.Context, b *mgbot.MartinGarrixBot, row db.GetAnnouncementsToRefreshRow, components []discord.LayoutComponent) error {
	// A struct literal rather than NewMessageUpdate().WithComponents(...): disgo's
	// builders take value receivers, so a discarded result is a silent no-op. Every
	// field here is an omitempty pointer, so setting only Components leaves the
	// message's content and embed exactly as they were posted.
	//
	// Not NewMessageUpdateV2 either -- that sets MessageFlagIsComponentsV2, which is
	// incompatible with the embed these messages carry.
	_, err := b.Client.Rest.UpdateMessage(
		snowflake.ID(row.ChannelID), snowflake.ID(row.MessageID),
		discord.MessageUpdate{Components: &components},
	)
	if err == nil {
		return nil
	}

	var restErr *rest.Error
	if errors.As(err, &restErr) {
		switch restErr.Code {
		case rest.JSONErrorCodeUnknownMessage, rest.JSONErrorCodeUnknownChannel:
			// The message or its channel is gone. There is nothing left to correct,
			// and keeping the row would retry against it forever.
			if delErr := b.Queries.DeleteAnnouncement(ctx, db.DeleteAnnouncementParams{
				GuildID: row.GuildID, MessageID: row.MessageID,
			}); delErr != nil {
				slog.Error("Failed to forget a deleted announcement",
					slog.Int64("song_id", row.SongID), slog.Any("err", delErr))
			}
			return err

		case rest.JSONErrorCodeMissingAccess, rest.JSONErrorCodeLackPermissionsToPerformAction:
			// The bot lost the channel. Park the row rather than deleting it:
			// permissions come back, and this is the only record of where the message
			// is. The next successful post to the channel un-parks it.
			slog.Warn("Cannot edit an announcement, parking it",
				slog.Int64("song_id", row.SongID),
				slog.Uint64("channel_id", uint64(row.ChannelID)),
				slog.Any("err", err))
			if failErr := b.Queries.MarkAnnouncementFailed(ctx, db.MarkAnnouncementFailedParams{
				GuildID: row.GuildID, MessageID: row.MessageID,
			}); failErr != nil {
				slog.Error("Failed to park an announcement",
					slog.Int64("song_id", row.SongID), slog.Any("err", failErr))
			}
			return err
		}
	}

	// Anything else -- a 5xx, a network failure, a timeout -- says nothing about this
	// row. Leave it untouched and try again next cycle.
	slog.Error("Failed to refresh an announcement",
		slog.Int64("song_id", row.SongID),
		slog.Uint64("message_id", uint64(row.MessageID)), slog.Any("err", err))
	return err
}
