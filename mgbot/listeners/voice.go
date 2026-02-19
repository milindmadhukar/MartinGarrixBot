package listeners

import (
	"context"
	"log/slog"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
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

		// Check if radio is active in this guild
		if !b.RadioManager.IsActive(e.VoiceState.GuildID) {
			return
		}

		// Get the bot's voice channel
		player := b.RadioManager.Client.ExistingPlayer(e.VoiceState.GuildID)
		if player == nil || player.ChannelID() == nil {
			return
		}

		radioChannelID := *player.ChannelID()

		// Check if this is a user joining the radio channel (not leaving or already in)
		// User is joining if:
		// 1. New state has them in the radio channel
		// 2. Old state had them in a different channel (or no channel)
		isNowInRadioChannel := e.VoiceState.ChannelID != nil && *e.VoiceState.ChannelID == radioChannelID
		wasInDifferentChannel := e.OldVoiceState.ChannelID == nil || *e.OldVoiceState.ChannelID != radioChannelID

		if isNowInRadioChannel && wasInDifferentChannel {
			// Check if the user is a bot (uses cache-first-then-REST utility)
			member, err := utils.GetMember(b.Client, e.VoiceState.GuildID, e.VoiceState.UserID)
			if err != nil {
				// Failed to fetch member, assume human (safer to resume)
				slog.Debug("Failed to get member during voice join, assuming human",
					slog.String("guild_id", e.VoiceState.GuildID.String()),
					slog.String("user_id", e.VoiceState.UserID.String()),
					slog.Any("err", err))
			} else if member.User.Bot {
				// It's a bot, ignore
				return
			}

			// Check if radio is paused
			if !b.RadioManager.IsPaused(e.VoiceState.GuildID) {
				// Radio is not paused, no need to resume
				return
			}

			// A human joined and radio is paused! Resume the radio
			slog.Debug("Human joined radio channel while paused, resuming playback",
				slog.String("guild_id", e.VoiceState.GuildID.String()),
				slog.String("user_id", e.VoiceState.UserID.String()),
				slog.String("channel_id", radioChannelID.String()))

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
