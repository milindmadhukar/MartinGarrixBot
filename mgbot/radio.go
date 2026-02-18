package mgbot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// PlayNextRadioSong fetches and plays a random song from the database
func (b *MartinGarrixBot) PlayNextRadioSong(guildID snowflake.ID) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get random song from database
	song, err := b.Queries.GetRandomSongForRadio(ctx)
	if err != nil {
		slog.Error("Failed to get random song", slog.Any("err", err))
		return
	}

	// Build search query using artist and song name
	query := fmt.Sprintf("ytsearch:%s - %s", song.Artists, song.Name)

	// Play the track using RadioManager helper (stores track info and plays)
	if err := b.RadioManager.PlayTrackWithInfo(ctx, guildID, query, song.ID, song.Artists, song.Name); err != nil {
		slog.Error("Failed to play track", slog.Any("err", err), slog.String("artist", song.Artists), slog.String("song", song.Name))
		// Try again with next song
		time.Sleep(2 * time.Second)
		b.PlayNextRadioSong(guildID)
	}
}

// ReconnectRadio attempts to reconnect the radio after a disconnect
func (b *MartinGarrixBot) ReconnectRadio(guildID snowflake.ID) {
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
func (b *MartinGarrixBot) StartRadioInGuild(ctx context.Context, guildID snowflake.ID) error {
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

	// Set initial voice channel status
	statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer statusCancel()
	if err := utils.UpdateVoiceChannelStatus(statusCtx, b.Client, b.Cfg.Bot.Token, radioChannelID, "Loading..."); err != nil {
		slog.Error("Failed to set initial voice channel status", slog.Any("err", err))
	}

	// Start playing
	b.PlayNextRadioSong(guildID)

	return nil
}

// StopRadioInGuild stops the radio in a specific guild
func (b *MartinGarrixBot) StopRadioInGuild(ctx context.Context, guildID snowflake.ID) error {
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
func (b *MartinGarrixBot) DisconnectAllRadioChannels(ctx context.Context) {
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
			if channelID := player.ChannelID(); channelID != nil {
				// Clear voice channel status
				if err := utils.UpdateVoiceChannelStatus(ctx, b.Client, b.Cfg.Bot.Token, *channelID, ""); err != nil {
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
