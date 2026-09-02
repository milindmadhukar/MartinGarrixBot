package utils

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NotificationTypeYoutube NotificationType = "youtube"
	NotificationTypeReddit  NotificationType = "reddit"
	NotificationTypeSTMPD   NotificationType = "stmpd"
	NotificationTypeTour    NotificationType = "tour"
)

// NotificationItem represents a single item to be notified
type NotificationItem struct {
	// For simple content-based notifications (YouTube)
	Content string

	// For embed-based notifications (Reddit, STMPD)
	Embed *discord.Embed

	// For interactive components (STMPD)
	Components []discord.LayoutComponent

	// SongID is the row this item announces, and zero when the item is not a song --
	// which is every YouTube, Reddit and tour item, so those paths are unaffected.
	//
	// Set it and the notifier remembers where the message landed. A release's
	// streaming links arrive over the weeks after it is announced, and until now the
	// message id came back from CreateMessage, went into a debug log and was thrown
	// away, so an announcement kept whatever buttons it had in its first fifteen
	// minutes forever.
	SongID int64

	// LinkSignature is the button set as posted, from utils.SongLinkSignature.
	LinkSignature string
}

// GuildNotificationConfig holds the channel and role info for a guild
type GuildNotificationConfig struct {
	// GuildID is needed to record an announcement: song_announcements is keyed on
	// (guild_id, message_id).
	GuildID   snowflake.ID
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
			// If no guild configs exist, return empty list silently
			if errors.Is(err, pgx.ErrNoRows) {
				return configs, nil
			}
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
			// If no guild configs exist, return empty list silently
			if errors.Is(err, pgx.ErrNoRows) {
				return configs, nil
			}
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
			// If no guild configs exist, return empty list silently
			if errors.Is(err, pgx.ErrNoRows) {
				return configs, nil
			}
			return nil, err
		}
		for _, g := range guilds {
			config := GuildNotificationConfig{
				GuildID:   snowflake.ID(g.GuildID),
				ChannelID: snowflake.ID(g.StmpdNotificationsChannel.Int64),
			}
			if g.StmpdNotificationsRole.Valid {
				roleID := snowflake.ID(g.StmpdNotificationsRole.Int64)
				config.RoleID = &roleID
			}
			configs = append(configs, config)
		}

	case NotificationTypeTour:
		guilds, err := bn.Queries.GetTourNotificationChannels(context.Background())
		if err != nil {
			// If no guild configs exist, return empty list silently
			if errors.Is(err, pgx.ErrNoRows) {
				return configs, nil
			}
			return nil, err
		}
		for _, g := range guilds {
			config := GuildNotificationConfig{
				ChannelID: snowflake.ID(g.TourNotificationsChannel.Int64),
			}
			if g.TourNotificationsRole.Valid {
				roleID := snowflake.ID(g.TourNotificationsRole.Int64)
				config.RoleID = &roleID
			}
			configs = append(configs, config)
		}
	}

	return configs, nil
}

// sendToGuild sends the batched notifications to a specific guild
func (bn *BatchNotifier) sendToGuild(guild GuildNotificationConfig) error {
	itemCount := len(bn.Items)

	// If there's only one item, combine the ping with the content in a single message
	if itemCount == 1 {
		return bn.sendSingleItem(guild)
	}

	// For multiple items, send ping first, then separate messages
	headerContent := bn.buildHeaderContent(guild.RoleID)
	headerMsg, err := bn.RestClient.CreateMessage(guild.ChannelID,
		discord.NewMessageCreate().
			WithContent(headerContent),
	)
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
				discord.NewMessageCreate().
					WithContent(item.Content),
			)

		case NotificationTypeReddit:
			// Send each post as a separate embed message
			if item.Embed != nil {
				msg, err = bn.RestClient.CreateMessage(guild.ChannelID,
					discord.NewMessageCreate().
						WithEmbeds(*item.Embed),
				)
			}

		case NotificationTypeSTMPD:
			// Send each release as a separate embed message with buttons
			if item.Embed != nil {
				builder := discord.NewMessageCreate().
					WithEmbeds(*item.Embed)

				// Add components if they exist
				if len(item.Components) > 0 {
					for _, component := range item.Components {
						builder = builder.AddComponents(component)
					}
				}

				msg, err = bn.RestClient.CreateMessage(guild.ChannelID, builder)
			}

		case NotificationTypeTour:
			// Send each tour show as a separate embed message with ticket button
			if item.Embed != nil {
				builder := discord.NewMessageCreate().
					WithEmbeds(*item.Embed)

				// Add components if they exist
				if len(item.Components) > 0 {
					for _, component := range item.Components {
						builder = builder.AddComponents(component)
					}
				}

				msg, err = bn.RestClient.CreateMessage(guild.ChannelID, builder)
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
			bn.recordAnnouncement(guild, item, msg)
		}

		// Small delay between messages
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

// sendSingleItem sends a single notification item with the ping combined
func (bn *BatchNotifier) sendSingleItem(guild GuildNotificationConfig) error {
	if len(bn.Items) != 1 {
		return fmt.Errorf("sendSingleItem called with %d items", len(bn.Items))
	}

	item := bn.Items[0]

	// Build role ping prefix
	rolePing := ""
	if guild.RoleID != nil {
		rolePing = fmt.Sprintf("<@&%d>, ", *guild.RoleID)
	}

	var msg *discord.Message
	var err error

	switch bn.NotificationType {
	case NotificationTypeYoutube:
		// Combine ping with video content
		content := rolePing + item.Content
		msg, err = bn.RestClient.CreateMessage(guild.ChannelID,
			discord.NewMessageCreate().
				WithContent(content),
		)

	case NotificationTypeReddit:
		// Send embed with ping as content
		if item.Embed != nil {
			content := rolePing + "New post on r/Martingarrix"
			msg, err = bn.RestClient.CreateMessage(guild.ChannelID,
				discord.NewMessageCreate().
					WithContent(content).
					WithEmbeds(*item.Embed),
			)
		}

	case NotificationTypeSTMPD:
		// Send embed with buttons and ping as content
		if item.Embed != nil {
			content := rolePing + "New release on STMPD RCRDS!"
			builder := discord.NewMessageCreate().
				WithContent(content).
				WithEmbeds(*item.Embed)

			// Add components if they exist
			if len(item.Components) > 0 {
				for _, component := range item.Components {
					builder = builder.AddComponents(component)
				}
			}

			msg, err = bn.RestClient.CreateMessage(guild.ChannelID, builder)
		}

	case NotificationTypeTour:
		// Send embed with buttons and ping as content
		if item.Embed != nil {
			content := rolePing + "New tour date announced!"
			builder := discord.NewMessageCreate().
				WithContent(content).
				WithEmbeds(*item.Embed)

			// Add components if they exist
			if len(item.Components) > 0 {
				for _, component := range item.Components {
					builder = builder.AddComponents(component)
				}
			}

			msg, err = bn.RestClient.CreateMessage(guild.ChannelID, builder)
		}
	}

	if err != nil {
		slog.Error("Failed to send single notification item",
			slog.String("type", string(bn.NotificationType)),
			slog.Uint64("channel_id", uint64(guild.ChannelID)),
			slog.Any("err", err))
		return err
	}

	if msg != nil {
		slog.Debug("Sent single notification item",
			slog.String("type", string(bn.NotificationType)),
			slog.Uint64("channel_id", uint64(guild.ChannelID)),
			slog.Uint64("message_id", uint64(msg.ID)))
		bn.recordAnnouncement(guild, item, msg)
	}

	return nil
}

// recordAnnouncement remembers where a song announcement landed, so its buttons can be
// corrected later when the song gains a streaming link.
//
// Failures are logged and swallowed. Losing the bookkeeping costs a future edit, which
// is strictly better than failing the announcement over it -- and a posting bot that
// errors out because a side table rejected a row is a worse bot.
//
// msg.ChannelID rather than guild.ChannelID: the same value today, but it is what
// Discord actually says the message is in.
func (bn *BatchNotifier) recordAnnouncement(guild GuildNotificationConfig, item NotificationItem, msg *discord.Message) {
	if item.SongID == 0 {
		return // not a song: YouTube, Reddit and tour items have nothing to correct
	}

	ctx := context.Background()
	if err := bn.Queries.InsertSongAnnouncement(ctx, db.InsertSongAnnouncementParams{
		SongID:     item.SongID,
		GuildID:    int64(guild.GuildID),
		ChannelID:  int64(msg.ChannelID),
		MessageID:  int64(msg.ID),
		ButtonsKey: item.LinkSignature,
	}); err != nil {
		slog.Error("Failed to record where an announcement landed",
			slog.Int64("song_id", item.SongID),
			slog.Uint64("message_id", uint64(msg.ID)),
			slog.Any("err", err))
		return
	}

	// Whatever stopped this channel being editable is over, so un-park the messages
	// that were left waiting on it.
	if err := bn.Queries.ClearAnnouncementFailures(ctx, int64(msg.ChannelID)); err != nil {
		slog.Warn("Failed to clear parked announcements for a channel",
			slog.Uint64("channel_id", uint64(msg.ChannelID)), slog.Any("err", err))
	}
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

	case NotificationTypeTour:
		if itemCount == 1 {
			return rolePing + "New tour date announced! 🎤"
		}
		return fmt.Sprintf("%s%d new tour dates announced! 🎤", rolePing, itemCount)

	default:
		return rolePing + "New notification"
	}
}
