package handlers

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/rest"
	"github.com/jackc/pgx/v5"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func MessageHandler(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.MessageCreate) {

		if e.Message.Author.Bot || e.Message.Author.System {
			return
		}

		// TODO: Update message in bots channel for level change
		// True garrixer role add if crosses level 13 // check from config
		// Handler to prompt users to do slash commands if they are not using prefix commands

		if strings.HasPrefix(strings.ToLower(e.Message.Content), "mg.") {
			replyMessageContent := "Prefix commands are deprecated. Please use slash commands instead. Type `/` to see available commands."
			utils.ReplyToMessageDeleteAfter(b.Client, e.ChannelID, e.Message, replyMessageContent, 10)
			b.Client.Rest().DeleteMessage(e.ChannelID, e.Message.ID, rest.WithDelay(10))
			return
		}

		user, err := b.Queries.GetUser(context.Background(), int64(e.Message.Author.ID))

		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				user, err = b.Queries.CreateUser(context.Background(), int64(e.Message.Author.ID))
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
			MessageID:   int64(e.MessageID),
			ChannelID:   int64(e.ChannelID),
			AuthorID:    int64(e.Message.Author.ID),
			Content:     e.Message.Content,
			LastXpAdded: user.LastXpAdded,
			TotalXp:     user.TotalXp,
		}

		if !user.LastXpAdded.Valid || now.Sub(user.LastXpAdded.Time.UTC()) >= time.Minute {
			// Generate random number between 15 and 25
			xp := 15 + rand.Int32N(11)
			params.TotalXp.Int32 = user.TotalXp.Int32 + xp
			params.TotalXp.Valid = true
			params.LastXpAdded.Time = now
			params.LastXpAdded.Valid = true
		}

		err = b.Queries.MessageSent(context.Background(), params)
		if err != nil {
			slog.Error("Failed to log message", slog.Any("err", err))
		}
	})
}
