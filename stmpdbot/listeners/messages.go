package listeners

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
)

func MessageCreateListener(b *stmpdbot.STMPDBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.MessageCreate) {
		if e.Message.Author.Bot || e.Message.Author.System || e.GuildID == nil {
			return
		}

		// TODO: Handler to prompt users to do slash commands if they are not
		// using prefix commands

		if strings.HasPrefix(strings.ToLower(e.Message.Content), "mg.") {
			replyMessageContent := "Prefix commands are deprecated. Please use slash commands instead. Type `/` to see available commands."
			utils.ReplyToMessageDeleteAfter(b.Client, e.ChannelID, e.Message, replyMessageContent, 10)
			b.Client.Rest.DeleteMessage(e.ChannelID, e.Message.ID, rest.WithDelay(10))
			return
		}

		user, err := b.Queries.GetUser(context.Background(), db.GetUserParams{
			ID:      int64(e.Message.Author.ID),
			GuildID: int64(*e.GuildID),
		})

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				user, err = b.Queries.CreateUser(context.Background(), db.CreateUserParams{
					ID:      int64(e.Message.Author.ID),
					GuildID: int64(*e.GuildID),
				})
				if err != nil {
					slog.Error("Failed to create user", slog.Any("err", err))
					return
				}
				slog.Info("Created user", slog.Any("user", user.ID))
			} else {
				slog.Error("Failed to get user", slog.Any("err", err))
				return
			}
		}

		now := time.Now().UTC()

		params := db.MessageSentParams{
			MessageID: int64(e.MessageID),
			GuildID:   int64(*e.GuildID),
			ChannelID: int64(e.ChannelID),
			AuthorID:  int64(e.Message.Author.ID),
			Content:   e.Message.Content,
		}

		// user.TotalXp is deliberately not read here. The award is a delta and
		// the database adds it, so the listener never round-trips a total.
		if award, awarded := xpAward(
			user.LastXpAdded.Time,
			user.LastXpAdded.Valid,
			now,
			rollXP(),
		); awarded {
			params.Roll = award
			params.AwardedAt = pgtype.Timestamp{Time: now, Valid: true}
		}

		row, err := b.Queries.MessageSent(context.Background(), params)
		if err != nil {
			slog.Error("Failed to log message", slog.Any("err", err))
			return
		}

		// Announce only on an actual crossing, off totals the database returned.
		if level, ok := crossedLevel(row.OldXp, row.NewXp); ok {
			if guild, found := levelUpConfig(context.Background(), b, *e.GuildID); found {
				announceLevelUp(b, e, guild, level)
			}
		}
	})
}
