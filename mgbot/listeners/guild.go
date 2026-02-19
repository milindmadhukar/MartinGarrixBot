package listeners

import (
	"context"
	"log/slog"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
)

// GuildJoinListener handles when the bot joins a new guild
func GuildJoinListener(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildJoin) {
		// Create guild configuration entry in database
		guild, err := b.Queries.CreateGuild(context.Background(), int64(e.GuildID))
		if err != nil {
			slog.Error("Failed to create guild configuration",
				slog.Any("guild_id", e.GuildID),
				slog.Any("err", err))
			return
		}

		slog.Info("Created guild configuration",
			slog.Any("guild_id", guild.GuildID),
			slog.String("guild_name", e.Guild.Name))
	})
}
