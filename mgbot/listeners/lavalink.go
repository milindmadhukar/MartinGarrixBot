package listeners

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// LavalinkTrackStartListener is called when a track starts playing
func LavalinkTrackStartListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(player disgolink.Player, event lavalink.TrackStartEvent) {
		slog.Info("Track started",
			slog.String("guild_id", event.GuildID().String()),
			slog.String("track", event.Track.Info.Title),
			slog.String("author", event.Track.Info.Author))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Get the voice channel ID from the player
		channelID := player.ChannelID()
		if channelID == nil {
			slog.Warn("No voice channel ID available for updating status")
			return
		}

		// Get the song info from RadioManager (stored when track was queued)
		statusText := ""
		if trackInfo, exists := b.RadioManager.GetCurrentTrack(event.GuildID()); exists {
			statusText = fmt.Sprintf("%s - %s", trackInfo.Artist, trackInfo.SongName)
		}

		// Fallback to track info if not found in RadioManager
		if statusText == "" {
			statusText = fmt.Sprintf("%s - %s", event.Track.Info.Author, event.Track.Info.Title)
		}

		// Update voice channel status with current song
		if err := utils.UpdateVoiceChannelStatus(ctx, b.Client, b.Cfg.Bot.Token, *channelID, statusText); err != nil {
			slog.Error("Failed to update voice channel status", slog.Any("err", err))
		}
	})
}

// LavalinkTrackEndListener is called when a track finishes
func LavalinkTrackEndListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(player disgolink.Player, event lavalink.TrackEndEvent) {
		slog.Info("Track ended",
			slog.String("guild_id", event.GuildID().String()),
			slog.String("reason", string(event.Reason)))

		// Only auto-play next song if the reason allows it
		if !event.Reason.MayStartNext() {
			return
		}

		// Check if radio is still active for this guild
		if !b.RadioManager.IsActive(event.GuildID()) {
			return
		}

		// Play next random song
		go playNextRadioSong(b, event.GuildID())
	})
}

// LavalinkTrackExceptionListener is called when a track encounters an error
func LavalinkTrackExceptionListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(player disgolink.Player, event lavalink.TrackExceptionEvent) {
		slog.Error("Track exception",
			slog.String("guild_id", event.GuildID().String()),
			slog.String("message", event.Exception.Message),
			slog.String("severity", string(event.Exception.Severity)))

		// Try to play next song on error
		if b.RadioManager.IsActive(event.GuildID()) {
			go playNextRadioSong(b, event.GuildID())
		}
	})
}

// LavalinkTrackStuckListener is called when a track gets stuck
func LavalinkTrackStuckListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(player disgolink.Player, event lavalink.TrackStuckEvent) {
		slog.Warn("Track stuck",
			slog.String("guild_id", event.GuildID().String()),
			slog.String("threshold", event.Threshold.String()))

		// Try to play next song when stuck
		if b.RadioManager.IsActive(event.GuildID()) {
			go playNextRadioSong(b, event.GuildID())
		}
	})
}

// LavalinkWebSocketClosedListener is called when the voice WebSocket connection closes
func LavalinkWebSocketClosedListener(b *mgbot.MartinGarrixBot) disgolink.EventListener {
	return disgolink.NewListenerFunc(func(player disgolink.Player, event lavalink.WebSocketClosedEvent) {
		slog.Warn("WebSocket closed",
			slog.String("guild_id", event.GuildID().String()),
			slog.Int("code", event.Code),
			slog.String("reason", event.Reason),
			slog.Bool("by_remote", event.ByRemote))

		// Attempt to reconnect if it's an unexpected closure
		if event.Code != 1000 && b.RadioManager.IsActive(event.GuildID()) {
			go reconnectRadio(b, event.GuildID())
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
