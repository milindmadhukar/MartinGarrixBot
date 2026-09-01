package listeners

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgolink/v4/disgolink"
	"github.com/disgoorg/disgolink/v4/lavalink"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// Every listener below takes a *pointer* event type. disgolink emits its events as
// pointers and NewListenerFunc matches them with a type assertion, so a value type
// here would compile cleanly and then simply never fire.

// LavalinkTrackStartListener is called when a track starts playing
func LavalinkTrackStartListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(e *disgolink.PlayerTrackStartEvent) {
		guildID := e.Player.GuildID

		slog.Info("Track started",
			slog.String("guild_id", guildID.String()),
			slog.String("track", e.Track.Info.Title),
			slog.String("author", e.Track.Info.Author))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Reset skip votes for new track
		b.RadioManager.ResetSkipVotes(guildID)

		// Get the voice channel ID from the player. In disgolink v4 this is a value
		// that is zero when the player is not in a channel, not a nil pointer.
		channelID := e.Player.Voice.ChannelID
		if channelID == 0 {
			slog.Warn("No voice channel ID available for updating status")
			return
		}

		// Get the song info from RadioManager (stored when track was queued)
		statusText := ""
		if trackInfo, exists := b.RadioManager.GetCurrentTrack(guildID); exists {
			statusText = fmt.Sprintf("%s - %s", trackInfo.Artist, trackInfo.SongName)
		}

		// Fallback to track info if not found in RadioManager
		if statusText == "" {
			statusText = fmt.Sprintf("%s - %s", e.Track.Info.Author, e.Track.Info.Title)
		}

		// Update voice channel status with current song
		if err := utils.UpdateVoiceChannelStatus(ctx, b.Client, b.Cfg.Bot.Token, channelID, statusText); err != nil {
			slog.Error("Failed to update voice channel status", slog.Any("err", err))
		}
	})
}

// LavalinkTrackEndListener is called when a track finishes
func LavalinkTrackEndListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(e *disgolink.PlayerTrackEndEvent) {
		guildID := e.Player.GuildID

		slog.Info("Track ended",
			slog.String("guild_id", guildID.String()),
			slog.String("reason", string(e.Reason)))

		// A track that played through to the end is the only trustworthy signal that
		// playback is healthy. TrackStartEvent is not: during the 2026-08-18 outage it
		// fired for every track, roughly a second before the track threw.
		if e.Reason == lavalink.TrackEndReasonFinished {
			b.RadioManager.ResetPlaybackFailures(guildID)
		}

		// Only auto-play next song if the reason allows it
		if !e.Reason.MayStartNext() {
			return
		}

		// Check if radio is still active for this guild
		if !b.RadioManager.IsActive(guildID) {
			return
		}

		// Get the voice channel ID from the player
		channelID := e.Player.Voice.ChannelID
		if channelID == 0 {
			slog.Warn("No voice channel ID available")
			return
		}

		// Check if there are any humans in the voice channel
		humanCount := utils.CountHumansInVoiceChannel(b.Client, guildID, channelID)

		if humanCount == 0 {
			// No one is listening, pause the radio and clear status
			slog.Info("No listeners in voice channel, pausing radio",
				slog.String("guild_id", guildID.String()))

			b.RadioManager.SetPaused(guildID, true)

			// Clear voice channel status
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := utils.UpdateVoiceChannelStatus(ctx, b.Client, b.Cfg.Bot.Token, channelID, ""); err != nil {
				slog.Error("Failed to clear voice channel status", slog.Any("err", err))
			}

			return
		}

		// There are listeners, play next song
		go playNextRadioSong(b, guildID)
	})
}

// LavalinkTrackExceptionListener is called when a track encounters an error
func LavalinkTrackExceptionListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(e *disgolink.PlayerTrackExceptionEvent) {
		guildID := e.Player.GuildID

		slog.Error("Track exception",
			slog.String("guild_id", guildID.String()),
			slog.String("message", e.Exception.Message),
			slog.String("severity", string(e.Exception.Severity)))

		// Count this against the guild before advancing: PlayNextRadioSong paces itself
		// off the failure count, and the load itself succeeded, so nothing else records it.
		failures := b.RadioManager.RecordPlaybackFailure(guildID)
		slog.Warn("Recorded playback failure",
			slog.String("guild_id", guildID.String()),
			slog.Int("consecutive_failures", failures))

		// Try to play next song on error
		if b.RadioManager.IsActive(guildID) {
			go playNextRadioSong(b, guildID)
		}
	})
}

// LavalinkTrackStuckListener is called when a track gets stuck
func LavalinkTrackStuckListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(e *disgolink.PlayerTrackStuckEvent) {
		guildID := e.Player.GuildID

		slog.Warn("Track stuck",
			slog.String("guild_id", guildID.String()),
			slog.String("threshold", e.Threshold.String()))

		// A stuck track is a failed track as far as pacing is concerned.
		b.RadioManager.RecordPlaybackFailure(guildID)

		// Try to play next song when stuck
		if b.RadioManager.IsActive(guildID) {
			go playNextRadioSong(b, guildID)
		}
	})
}

// LavalinkWebSocketClosedListener is called when the voice WebSocket connection closes
func LavalinkWebSocketClosedListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(e *disgolink.PlayerWebSocketClosedEvent) {
		guildID := e.Player.GuildID

		slog.Warn("WebSocket closed",
			slog.String("guild_id", guildID.String()),
			slog.Int("code", e.Code),
			slog.String("reason", e.Reason),
			slog.Bool("by_remote", e.ByRemote))

		// Attempt to reconnect if it's an unexpected closure
		if e.Code != 1000 && b.RadioManager.IsActive(guildID) {
			go reconnectRadio(b, guildID)
		}
	})
}

// playNextRadioSong delegates to the bot's PlayNextRadioSong method
func playNextRadioSong(b *mgbot.MartinGarrixBot, guildID snowflake.ID) {
	b.PlayNextRadioSong(guildID)
}

// reconnectRadio delegates to the bot's ReconnectRadio method
func reconnectRadio(b *mgbot.MartinGarrixBot, guildID snowflake.ID) {
	b.ReconnectRadio(guildID)
}
