package utils

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationTypeYoutube NotificationType = "youtube"
	NotificationTypeReddit  NotificationType = "reddit"
	NotificationTypeSTMPD   NotificationType = "stmpd"
)

// NotificationItem represents a single item to be notified
type NotificationItem struct {
	// For simple content-based notifications (YouTube)
	Content string

	// For embed-based notifications (Reddit, STMPD)
	Embed *discord.Embed

	// For interactive components (STMPD)
	Components []discord.ContainerComponent
}

// GuildNotificationConfig holds the channel and role info for a guild
type GuildNotificationConfig struct {
	ChannelID snowflake.ID
	RoleID    *snowflake.ID // nil if no role to ping
}

// BotDependencies provides the necessary dependencies for the BatchNotifier
type BotDependencies interface {
	GetQueries() *db.Queries
	GetRestClient() rest.Client
}

// BatchNotifier handles batched notifications for a specific notification type
type BatchNotifier struct {
	Queries          *db.Queries
	RestClient       rest.Rest
	NotificationType NotificationType
	Items            []NotificationItem
}

// NewBatchNotifier creates a new batch notifier
func NewBatchNotifier(queries *db.Queries, restClient rest.Rest, notificationType NotificationType) *BatchNotifier {
	return &BatchNotifier{
		Queries:          queries,
		RestClient:       restClient,
		NotificationType: notificationType,
		Items:            make([]NotificationItem, 0),
	}
}

// AddItem adds an item to the batch
func (bn *BatchNotifier) AddItem(item NotificationItem) {
	bn.Items = append(bn.Items, item)
}

// Send sends all batched notifications to the configured guilds
func (bn *BatchNotifier) Send() error {
	if len(bn.Items) == 0 {
		return nil
	}

	guilds, err := bn.getGuildConfigs()
	if err != nil {
		return fmt.Errorf("failed to get guild configs: %w", err)
	}

	for _, guild := range guilds {
		if err := bn.sendToGuild(guild); err != nil {
			slog.Error("Failed to send notification to guild",
				slog.String("type", string(bn.NotificationType)),
				slog.Uint64("channel_id", uint64(guild.ChannelID)),
				slog.Any("err", err))
			continue
		}
		time.Sleep(1 * time.Second) // Rate limiting between guilds
	}

	return nil
}

// getGuildConfigs fetches the guild configurations for this notification type
func (bn *BatchNotifier) getGuildConfigs() ([]GuildNotificationConfig, error) {
	var configs []GuildNotificationConfig

	switch bn.NotificationType {
	case NotificationTypeYoutube:
		guilds, err := bn.Queries.GetYoutubeNotifactionChannels(context.Background())
		if err != nil {
			return nil, err
		}
		for _, g := range guilds {
			config := GuildNotificationConfig{
				ChannelID: snowflake.ID(g.YoutubeNotificationsChannel.Int64),
			}
			if g.YoutubeNotificationsRole.Valid {
				roleID := snowflake.ID(g.YoutubeNotificationsRole.Int64)
				config.RoleID = &roleID
			}
			configs = append(configs, config)
		}

	case NotificationTypeReddit:
		guilds, err := bn.Queries.GetRedditNotificationChannels(context.Background())
		if err != nil {
			return nil, err
		}
		for _, g := range guilds {
			config := GuildNotificationConfig{
				ChannelID: snowflake.ID(g.RedditNotificationsChannel.Int64),
			}
			if g.RedditNotificationsRole.Valid {
				roleID := snowflake.ID(g.RedditNotificationsRole.Int64)
				config.RoleID = &roleID
			}
			configs = append(configs, config)
		}

	case NotificationTypeSTMPD:
		guilds, err := bn.Queries.GetSTMPDNofiticationChannels(context.Background())
		if err != nil {
			return nil, err
		}
		for _, g := range guilds {
			config := GuildNotificationConfig{
				ChannelID: snowflake.ID(g.StmpdNotificationsChannel.Int64),
			}
			if g.StmpdNotificationsRole.Valid {
				roleID := snowflake.ID(g.StmpdNotificationsRole.Int64)
				config.RoleID = &roleID
			}
			configs = append(configs, config)
		}
	}

	return configs, nil
}

// sendToGuild sends the batched notifications to a specific guild
func (bn *BatchNotifier) sendToGuild(guild GuildNotificationConfig) error {
	// First, send the ping message with the count
	headerContent := bn.buildHeaderContent(guild.RoleID)
	headerMsg, err := bn.RestClient.CreateMessage(guild.ChannelID,
		discord.NewMessageCreateBuilder().
			SetContent(headerContent).
			Build())
	if err != nil {
		return fmt.Errorf("failed to send header message: %w", err)
	}

	slog.Debug("Sent notification header",
		slog.String("type", string(bn.NotificationType)),
		slog.Uint64("channel_id", uint64(guild.ChannelID)),
		slog.Uint64("message_id", uint64(headerMsg.ID)),
		slog.Int("item_count", len(bn.Items)))

	// Small delay to ensure messages appear in order
	time.Sleep(500 * time.Millisecond)

	// Then send each item as a separate message (no ping)
	for i, item := range bn.Items {
		var msg *discord.Message
		var err error

		switch bn.NotificationType {
		case NotificationTypeYoutube:
			// Send each video as a separate message
			msg, err = bn.RestClient.CreateMessage(guild.ChannelID,
				discord.NewMessageCreateBuilder().
					SetContent(item.Content).
					Build())

		case NotificationTypeReddit:
			// Send each post as a separate embed message
			if item.Embed != nil {
				msg, err = bn.RestClient.CreateMessage(guild.ChannelID,
					discord.NewMessageCreateBuilder().
						SetEmbeds(*item.Embed).
						Build())
			}

		case NotificationTypeSTMPD:
			// Send each release as a separate embed message with buttons
			if item.Embed != nil {
				builder := discord.NewMessageCreateBuilder().
					SetEmbeds(*item.Embed)

				// Add components if they exist
				if len(item.Components) > 0 {
					for _, component := range item.Components {
						builder.AddContainerComponents(component)
					}
				}

				msg, err = bn.RestClient.CreateMessage(guild.ChannelID, builder.Build())
			}
		}

		if err != nil {
			slog.Error("Failed to send individual notification item",
				slog.String("type", string(bn.NotificationType)),
				slog.Uint64("channel_id", uint64(guild.ChannelID)),
				slog.Int("item_index", i),
				slog.Any("err", err))
			// Continue sending other items even if one fails
		} else if msg != nil {
			slog.Debug("Sent notification item",
				slog.String("type", string(bn.NotificationType)),
				slog.Uint64("channel_id", uint64(guild.ChannelID)),
				slog.Uint64("message_id", uint64(msg.ID)),
				slog.Int("item_index", i))
		}

		// Small delay between messages
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

// buildHeaderContent creates the header message with optional role ping
func (bn *BatchNotifier) buildHeaderContent(roleID *snowflake.ID) string {
	itemCount := len(bn.Items)

	rolePing := ""
	if roleID != nil {
		rolePing = fmt.Sprintf("<@&%d>, ", *roleID)
	}

	switch bn.NotificationType {
	case NotificationTypeYoutube:
		if itemCount == 1 {
			return rolePing + "New video posted!"
		}
		return fmt.Sprintf("%s%d new videos posted!", rolePing, itemCount)

	case NotificationTypeReddit:
		if itemCount == 1 {
			return rolePing + "New post on r/Martingarrix"
		}
		return fmt.Sprintf("%s%d new posts on r/Martingarrix", rolePing, itemCount)

	case NotificationTypeSTMPD:
		if itemCount == 1 {
			return rolePing + "New release on STMPD RCRDS!"
		}
		return fmt.Sprintf("%s%d new releases on STMPD RCRDS!", rolePing, itemCount)

	default:
		return rolePing + "New notification"
	}
}
