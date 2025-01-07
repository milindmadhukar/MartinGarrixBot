package handlers

import (
	"context"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
)

func MessageHandler(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.MessageCreate) {
		// TODO: Check for blocking
		// TODO : Fetch user

		b.Queries.MessageSent(context.Background(), db.MessageSentParams{
			MessageID: int64(e.MessageID),
			ChannelID: int64(e.ChannelID),
			AuthorID:  int64(e.Message.Author.ID),
			Content:   e.Message.Content,
			// FIX: Placeholder for now
			TotalXp: pgtype.Int4{
				Int32: 1000,
				Valid: true,
			},
			LastXpAdded: pgtype.Timestamp{},
		})
	})
}
