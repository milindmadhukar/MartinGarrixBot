package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

// UpdateVoiceChannelStatus updates the voice channel status using Discord's REST API
// Status text can be up to 500 characters, pass empty string to clear the status
func UpdateVoiceChannelStatus(ctx context.Context, client bot.Client, botToken string, channelID snowflake.ID, status string) error {
	// Discord API endpoint: PUT /channels/{channel.id}/voice-status
	// This is a custom call since disgo doesn't have built-in support yet
	type voiceStatusUpdate struct {
		Status string `json:"status"`
	}

	payload := voiceStatusUpdate{Status: status}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT",
		"https://discord.com/api/v10/channels/"+channelID.String()+"/voice-status",
		bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Rest().HTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord API returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetMember gets a guild member, checking cache first then falling back to REST API
func GetMember(client bot.Client, guildID, userID snowflake.ID) (*discord.Member, error) {
	// Try cache first
	member, ok := client.Caches().Member(guildID, userID)
	if ok {
		return &member, nil
	}

	// Cache miss - fetch from REST API
	memberPtr, err := client.Rest().GetMember(guildID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get member from REST API: %w", err)
	}

	return memberPtr, nil
}

// GetVoiceState gets a user's voice state in a guild, checking cache first then falling back to REST API
func GetVoiceState(client bot.Client, guildID, userID snowflake.ID) (*discord.VoiceState, error) {
	// Try cache first
	voiceState, ok := client.Caches().VoiceState(guildID, userID)
	if ok {
		return &voiceState, nil
	}

	// Cache miss - fetch from REST API
	voiceStatePtr, err := client.Rest().GetUserVoiceState(guildID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get voice state from REST API: %w", err)
	}

	return voiceStatePtr, nil
}

// CountHumansInVoiceChannel counts the number of human (non-bot) members in a voice channel
// Uses cache first, then falls back to REST API for cache misses
func CountHumansInVoiceChannel(client bot.Client, guildID, channelID snowflake.ID) int {
	humanCount := 0

	client.Caches().VoiceStatesForEach(guildID, func(vs discord.VoiceState) {
		if vs.ChannelID != nil && *vs.ChannelID == channelID {
			// Skip if it's the bot itself
			if vs.UserID == client.ApplicationID() {
				return
			}

			// Use the utility function to get member (cache-first-then-REST)
			member, err := GetMember(client, guildID, vs.UserID)
			if err != nil {
				// Failed to fetch member, assume human (safer for skip logic)
				slog.Debug("Failed to get member",
					slog.String("guild_id", guildID.String()),
					slog.String("user_id", vs.UserID.String()),
					slog.Any("err", err))
				humanCount++
				return
			}

			if !member.User.Bot {
				humanCount++
			}
		}
	})
	return humanCount
}
