package stmpdbot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils"
)

const (
	// Backoff between attempts, doubling each time, capped at radioMaxBackoff.
	radioBaseBackoff = 2 * time.Second
	radioMaxBackoff  = 5 * time.Minute
	// Consecutive failures before we treat this as a source-side outage rather than a
	// handful of bad rows, and drop to polling at radioMaxBackoff instead.
	radioCircuitBreakAt = 15
)

// radioBackoff maps consecutive playback failures onto a delay before the next attempt.
//
// The count is deliberately held on RadioManager rather than as a loop variable: the
// common failure is asynchronous. PlayTrackWithInfo returns nil, TrackStartEvent fires,
// and only then does Lavalink raise TrackExceptionEvent - so each retry arrives as a
// fresh call and a local counter would restart at zero every time. That is precisely how
// the 2026-08-18 outage produced one track per second for ten days.
func radioBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}

	if failures >= radioCircuitBreakAt {
		return radioMaxBackoff
	}

	return min(radioBaseBackoff<<min(failures-1, 8), radioMaxBackoff)
}

// radioQuery prefers the stored YouTube URL over a text search, which avoids a search
// round-trip and the chance of search picking the wrong upload.
//
// It falls back to search unless the URL names a single video. songs.youtube_url is not
// uniform: 193 of 848 rows are playlist links. Loading one of those would play the first
// track of the playlist instead of the song we picked, and pull up to
// youtubePlaylistLoadLimit pages of results to do it.
func radioQuery(song db.GetRandomSongForRadioRow) string {
	if song.YoutubeUrl.Valid && isYouTubeVideoURL(song.YoutubeUrl.String) {
		return song.YoutubeUrl.String
	}

	return fmt.Sprintf("ytsearch:%s - %s", song.Artists, song.Name)
}

// isYouTubeVideoURL reports whether url addresses one video rather than a playlist or mix.
func isYouTubeVideoURL(url string) bool {
	if strings.Contains(url, "/playlist") || strings.Contains(url, "list=") {
		return false
	}

	return strings.Contains(url, "watch?v=") || strings.Contains(url, "youtu.be/")
}

// PlayNextRadioSong fetches and plays a random song from the database, pacing itself
// against the guild's recent failure history until a track plays or the radio is stopped.
//
// This deliberately does not recurse. When every track fails, an unbounded immediate retry
// turns a source outage into thousands of requests a day from an IP YouTube is already
// rate-limiting, which makes the underlying problem worse rather than better.
func (b *STMPDBot) PlayNextRadioSong(guildID snowflake.ID) {
	// TrackEnd, TrackException and TrackStuck can all advance the track, and any of them
	// can fire while an advance is already running. Without this guard they compound.
	if !b.RadioManager.TryBeginAdvance(guildID) {
		slog.Debug("Track advance already in flight, skipping", slog.String("guild_id", guildID.String()))
		return
	}
	defer b.RadioManager.EndAdvance(guildID)

	for {
		// The only exit other than a successful start: someone stopped the radio, or
		// Lavalink went away, while we were backing off.
		if !b.RadioManager.IsActive(guildID) {
			slog.Info("Radio no longer active, stopping advance", slog.String("guild_id", guildID.String()))
			return
		}

		failures := b.RadioManager.PlaybackFailureCount(guildID)
		if delay := radioBackoff(failures); delay > 0 {
			slog.Warn("Backing off before next radio attempt",
				slog.String("guild_id", guildID.String()),
				slog.Int("consecutive_failures", failures),
				slog.Duration("backoff", delay))
			time.Sleep(delay)
		}

		// Cancel explicitly rather than deferring: the deferred cancels would otherwise
		// all pile up until the loop finished.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		song, err := b.Queries.GetRandomSongForRadio(ctx)
		if err != nil {
			cancel()
			slog.Error("Failed to get random song", slog.Any("err", err))
			return
		}

		err = b.RadioManager.PlayTrackWithInfo(ctx, guildID, radioQuery(song), song.ID, song.Artists, song.Name)
		cancel()

		if err == nil {
			// Not a success yet - the track may still fail at playback. The counter is
			// cleared by LavalinkTrackEndListener once a track actually finishes.
			return
		}

		failures = b.RadioManager.RecordPlaybackFailure(guildID)
		slog.Error("Failed to play track",
			slog.Any("err", err),
			slog.String("artist", song.Artists),
			slog.String("song", song.Name),
			slog.Int("consecutive_failures", failures))
	}
}

// ReconnectRadio attempts to reconnect the radio after a disconnect
func (b *STMPDBot) ReconnectRadio(guildID snowflake.ID) {
	time.Sleep(3 * time.Second) // Wait before reconnecting

	// Get guild configuration to find the radio channel
	config, err := b.Queries.GetRadioVoiceChannels(context.Background())
	if err != nil {
		slog.Error("Failed to get radio channels", slog.Any("err", err))
		return
	}

	for _, cfg := range config {
		if cfg.GuildID == int64(guildID) && cfg.RadioVoiceChannel.Valid {
			channelID := snowflake.ID(cfg.RadioVoiceChannel.Int64)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if err := b.Client.UpdateVoiceState(ctx, guildID, &channelID, false, false); err != nil {
				slog.Error("Failed to reconnect to voice channel", slog.Any("err", err))
			} else {
				slog.Info("Reconnected to voice channel", slog.String("guild_id", guildID.String()))
				// Wait for voice connection, then play next song
				time.Sleep(2 * time.Second)
				b.PlayNextRadioSong(guildID)
			}
			return
		}
	}
}

// StartRadioInGuild starts the 24/7 radio in a specific guild
func (b *STMPDBot) StartRadioInGuild(ctx context.Context, guildID snowflake.ID) error {
	// Ensure Lavalink is connected, if not try to connect
	if b.RadioManager == nil {
		return fmt.Errorf("radio manager not initialized")
	}

	if !b.RadioManager.IsLavalinkConnected() {
		slog.Info("Lavalink not connected, attempting to connect...")
		if err := b.RadioManager.ConnectToLavalink(ctx, b.Cfg.Lavalink.URL, b.Cfg.Lavalink.Password); err != nil {
			return fmt.Errorf("failed to connect to Lavalink: %w", err)
		}
		// Note: Lavalink listeners should already be registered from initial setup
		// If reconnecting after bot restart, listeners are registered in main.go
	}

	// Get guild configuration
	configs, err := b.Queries.GetRadioVoiceChannels(ctx)
	if err != nil {
		return fmt.Errorf("failed to get radio channels: %w", err)
	}

	// Find the radio channel for this guild
	var radioChannelID snowflake.ID
	for _, cfg := range configs {
		if cfg.GuildID == int64(guildID) && cfg.RadioVoiceChannel.Valid {
			radioChannelID = snowflake.ID(cfg.RadioVoiceChannel.Int64)
			break
		}
	}

	if radioChannelID == 0 {
		return fmt.Errorf("no radio voice channel configured for this guild")
	}

	// Mark radio as active
	b.RadioManager.SetActive(guildID, true)

	// Connect to voice channel
	if err := b.Client.UpdateVoiceState(ctx, guildID, &radioChannelID, false, false); err != nil {
		return fmt.Errorf("failed to connect to voice channel: %w", err)
	}

	slog.Info("Connected to radio channel", slog.String("guild_id", guildID.String()), slog.String("channel_id", radioChannelID.String()))

	// Wait for voice connection to establish
	time.Sleep(2 * time.Second)

	// Start playing (the TrackStartEvent will update the voice channel status)
	b.PlayNextRadioSong(guildID)

	return nil
}

// StopRadioInGuild stops the radio in a specific guild
func (b *STMPDBot) StopRadioInGuild(ctx context.Context, guildID snowflake.ID) error {
	// Check if RadioManager is initialized
	if b.RadioManager == nil {
		return fmt.Errorf("radio manager not initialized")
	}

	// Stop the radio and clear voice channel status
	if err := b.RadioManager.StopRadioAndClearStatus(ctx, b.Client, b.Cfg.Bot.Token, guildID); err != nil {
		slog.Error("Error stopping radio and clearing status", slog.Any("err", err))
	}

	// Disconnect from voice channel
	if err := b.Client.UpdateVoiceState(ctx, guildID, nil, false, false); err != nil {
		return fmt.Errorf("failed to disconnect from voice channel: %w", err)
	}

	slog.Info("Stopped radio", slog.String("guild_id", guildID.String()))
	return nil
}

// DisconnectAllRadioChannels disconnects the bot from all active radio channels
func (b *STMPDBot) DisconnectAllRadioChannels(ctx context.Context) {
	if b.RadioManager == nil {
		return
	}

	// Get all active guilds
	activeGuilds := b.RadioManager.GetActiveGuilds()

	for _, guildID := range activeGuilds {
		slog.Info("Disconnecting from radio channel due to Lavalink failure", slog.String("guild_id", guildID.String()))

		// Get the channel ID from player before disconnecting
		player := b.RadioManager.Client.ExistingPlayer(guildID)
		if player != nil {
			if channelID := player.Voice.ChannelID; channelID != 0 {
				// Clear voice channel status
				if err := utils.UpdateVoiceChannelStatus(ctx, b.Client, b.Cfg.Bot.Token, channelID, ""); err != nil {
					slog.Error("Failed to clear voice channel status", slog.Any("err", err))
				}
			}
		}

		// Disconnect from voice channel
		if err := b.Client.UpdateVoiceState(ctx, guildID, nil, false, false); err != nil {
			slog.Error("Failed to disconnect from voice channel", slog.Any("err", err), slog.String("guild_id", guildID.String()))
		}
	}

	// Stop all radios in RadioManager
	b.RadioManager.StopAllRadios(ctx)
}
