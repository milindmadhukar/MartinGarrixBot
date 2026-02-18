package listeners

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
)

func GuildMemberJoinListener(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildMemberJoin) {
		// Log to database
		now := time.Now().UTC()
		err := b.Queries.LogMemberJoin(context.Background(), db.LogMemberJoinParams{
			MemberID: int64(e.Member.User.ID),
			Time: pgtype.Timestamp{
				Time:  now,
				Valid: true,
			},
		})
		if err != nil {
			slog.Error("Failed to log member join", slog.Any("err", err))
		}

		// Get guild configuration for log channel
		config, err := b.Queries.GetLeaveJoinLogsChannel(context.Background(), int64(e.GuildID))
		if err != nil {
			// If guild config doesn't exist, silently skip logging
			if errors.Is(err, pgx.ErrNoRows) {
				return
			}
			slog.Error("Failed to get leave join logs channel", slog.Any("err", err))
			return
		}

		if !config.Valid || config.Int64 == 0 {
			return
		}

		// Get guild for member count
		guild, ok := e.Client().Caches().Guild(e.GuildID)
		memberCount := "Unknown"
		if ok {
			memberCount = fmt.Sprintf("#%d", guild.MemberCount)
		}

		// Send log message to channel
		channelID := snowflake.ID(config.Int64)
		accountCreated := e.Member.User.ID.Time()
		embed := discord.NewEmbedBuilder().
			SetTitle("Member Joined").
			SetColor(0x00FF00). // Green
			SetDescription(fmt.Sprintf("<@%d> has joined the server", e.Member.User.ID)).
			AddField("User", e.Member.User.Tag(), true).
			AddField("User ID", e.Member.User.ID.String(), true).
			AddField("Account Created", discord.TimestampStyleRelative.FormatTime(accountCreated), true).
			SetTimestamp(now).
			SetFooter(fmt.Sprintf("Member %s", memberCount), "").
			Build()

		_, err = b.Client.Rest().CreateMessage(channelID, discord.NewMessageCreateBuilder().
			SetEmbeds(embed).
			Build())
		if err != nil {
			slog.Error("Failed to send member join log", slog.Any("err", err))
		}
	})
}

func GuildMemberLeaveListener(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildMemberLeave) {
		// Determine user ID - try User first, fallback to Member.User
		userID := e.User.ID
		if userID == 0 && e.Member.User.ID != 0 {
			userID = e.Member.User.ID
		}

		// If we still don't have a user ID, we can't proceed
		if userID == 0 {
			slog.Warn("Received member leave event with no user ID", slog.Any("guild_id", e.GuildID))
			return
		}

		// Log to database
		now := time.Now().UTC()
		err := b.Queries.LogMemberLeave(context.Background(), db.LogMemberLeaveParams{
			MemberID: int64(userID),
			Time: pgtype.Timestamp{
				Time:  now,
				Valid: true,
			},
		})
		if err != nil {
			slog.Error("Failed to log member leave", slog.Any("err", err))
		}

		// Get guild configuration for log channel
		config, err := b.Queries.GetLeaveJoinLogsChannel(context.Background(), int64(e.GuildID))
		if err != nil {
			// If guild config doesn't exist, silently skip logging
			if errors.Is(err, pgx.ErrNoRows) {
				return
			}
			slog.Error("Failed to get leave join logs channel", slog.Any("err", err))
			return
		}

		if !config.Valid || config.Int64 == 0 {
			return
		}

		// Get guild for member count
		guild, ok := e.Client().Caches().Guild(e.GuildID)
		memberCount := "Unknown"
		if ok {
			memberCount = fmt.Sprintf("#%d", guild.MemberCount)
		}

		// Determine username/tag for display
		userTag := fmt.Sprintf("User#%d", userID)
		if e.User.ID != 0 {
			userTag = e.User.Tag()
		} else if e.Member.User.ID != 0 {
			userTag = e.Member.User.Tag()
		}

		// Send log message to channel
		channelID := snowflake.ID(config.Int64)
		embedBuilder := discord.NewEmbedBuilder().
			SetTitle("Member Left").
			SetColor(0xFF0000). // Red
			SetDescription(fmt.Sprintf("<@%d> has left the server", userID)).
			AddField("User", userTag, true).
			AddField("User ID", userID.String(), true)

		// Add joined date if available
		if e.Member.User.ID != 0 && !e.Member.JoinedAt.IsZero() {
			embedBuilder.AddField("Joined", discord.TimestampStyleRelative.FormatTime(e.Member.JoinedAt), true)
		}

		embed := embedBuilder.
			SetTimestamp(now).
			SetFooter(fmt.Sprintf("Member %s", memberCount), "").
			Build()

		_, err = b.Client.Rest().CreateMessage(channelID, discord.NewMessageCreateBuilder().
			SetEmbeds(embed).
			Build())
		if err != nil {
			slog.Error("Failed to send member leave log", slog.Any("err", err))
		}
	})
}
