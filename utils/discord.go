package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// CountHumansInVoiceChannel counts the number of human (non-bot) members in a voice channel
func CountHumansInVoiceChannel(client bot.Client, guildID, channelID snowflake.ID) int {
	humanCount := 0
	client.Caches().VoiceStatesForEach(guildID, func(vs discord.VoiceState) {
		if vs.ChannelID != nil && *vs.ChannelID == channelID {
			member, ok := client.Caches().Member(guildID, vs.UserID)
			if ok && !member.User.Bot {
				humanCount++
			}
		}
	})
	return humanCount
}
