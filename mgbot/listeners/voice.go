package listeners

import (
	"context"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/events"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
)

// VoiceStateUpdateListener forwards voice state updates to Lavalink
func VoiceStateUpdateListener(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildVoiceStateUpdate) {
		// Only forward bot's own voice state updates
		if e.VoiceState.UserID != b.Client.ApplicationID() {
			return
		}

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
