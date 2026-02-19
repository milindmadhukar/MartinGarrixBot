package listeners

import (
	"context"
	"log/slog"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
)

// VoiceStateUpdateListener forwards voice state updates to Lavalink and handles radio resume
func VoiceStateUpdateListener(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildVoiceStateUpdate) {
		// Forward bot's own voice state updates to Lavalink
		if e.VoiceState.UserID == b.Client.ApplicationID() {
			// Check if RadioManager is initialized
			if b.RadioManager == nil {
				return
			}

			b.RadioManager.Client.OnVoiceStateUpdate(
				context.TODO(),
				e.VoiceState.GuildID,
				e.VoiceState.ChannelID,
				e.VoiceState.SessionID,
			)

			// Clean up when disconnected
			if e.VoiceState.ChannelID == nil {
				b.RadioManager.SetActive(e.VoiceState.GuildID, false)
			}
			return
		}

		// Handle user join/leave for radio resume/pause logic
		if b.RadioManager == nil {
			return
		}

		// Check if radio is active and paused in this guild
		if !b.RadioManager.IsActive(e.VoiceState.GuildID) || !b.RadioManager.IsPaused(e.VoiceState.GuildID) {
			return
		}

		// Get the bot's voice channel
		player := b.RadioManager.Client.ExistingPlayer(e.VoiceState.GuildID)
		if player == nil || player.ChannelID() == nil {
			return
		}

		radioChannelID := *player.ChannelID()

		// Check if a user joined the radio channel
		if e.VoiceState.ChannelID != nil && *e.VoiceState.ChannelID == radioChannelID {
			// Check if the user is not a bot
			member, ok := b.Client.Caches().Member(e.VoiceState.GuildID, e.VoiceState.UserID)
			if !ok || member.User.Bot {
				return
			}

			// A human joined! Resume the radio
			slog.Info("Human joined radio channel, resuming playback",
				slog.String("guild_id", e.VoiceState.GuildID.String()),
				slog.String("user_id", e.VoiceState.UserID.String()))

			b.RadioManager.SetPaused(e.VoiceState.GuildID, false)

			// Play next song
			go b.PlayNextRadioSong(e.VoiceState.GuildID)
		}
	})
}

// VoiceServerUpdateListener forwards voice server updates to Lavalink
func VoiceServerUpdateListener(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.VoiceServerUpdate) {
		// Check if RadioManager is initialized
		if b.RadioManager == nil {
			return
		}

		b.RadioManager.Client.OnVoiceServerUpdate(
			context.TODO(),
			e.GuildID,
			e.Token,
			*e.Endpoint,
		)
	})
}
