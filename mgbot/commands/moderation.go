package commands

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/json"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var moderation = discord.SlashCommandCreate{
	Name:        "moderation",
	Description: "Moderation commands",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionSubCommand{
			Name:        "kick",
			Description: "Kick a member from the server",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to kick",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "reason",
					Description: "Reason for the kick",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "ban",
			Description: "Ban a member from the server",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to ban",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "reason",
					Description: "Reason for the ban",
					Required:    false,
				},
				discord.ApplicationCommandOptionInt{
					Name:        "delete_message_days",
					Description: "Number of days of messages to delete (0-7)",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "tempban",
			Description: "Temporarily ban a member from the server",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to temporarily ban",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "duration",
					Description: "Ban duration (e.g., 1h, 2d, 1w)",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "reason",
					Description: "Reason for the ban",
					Required:    false,
				},
				discord.ApplicationCommandOptionInt{
					Name:        "delete_message_days",
					Description: "Number of days of messages to delete (0-7)",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "softban",
			Description: "Ban and immediately unban a user to delete their messages",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to softban",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "reason",
					Description: "Reason for the softban",
					Required:    false,
				},
				discord.ApplicationCommandOptionInt{
					Name:        "delete_message_days",
					Description: "Number of days of messages to delete (0-7)",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "unban",
			Description: "Unban a user from the server",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to unban",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "reason",
					Description: "Reason for the unban",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "mute",
			Description: "Timeout a member",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to mute",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "duration",
					Description: "Mute duration (e.g., 10m, 1h, 1d) - max 28 days",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "reason",
					Description: "Reason for the mute",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "unmute",
			Description: "Remove timeout from a member",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to unmute",
					Required:    true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "reason",
					Description: "Reason for the unmute",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "logs",
			Description: "View moderation logs for a user",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to view logs for",
					Required:    true,
				},
			},
		},
	},
}

func ModerationHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		// Check if the user has moderator permissions
		if !utils.HasModeratorPermissions(e.Ctx, b.DB, b.Client.Rest(), *e.GuildID(), e.Member()) {
			return e.Respond(discord.InteractionResponseTypeCreateMessage,
				discord.NewMessageCreateBuilder().
					SetEmbeds(utils.FailureEmbed("Permission Denied",
						"You need Administrator permission or the Moderator role to use moderation commands.")).
					SetEphemeral(true).
					Build(),
			)
		}

		data := e.SlashCommandInteractionData()
		subcommand := data.SubCommandName

		switch *subcommand {
		case "kick":
			return handleKick(b, e)
		case "ban":
			return handleBan(b, e)
		case "tempban":
			return handleTempBan(b, e)
		case "softban":
			return handleSoftBan(b, e)
		case "unban":
			return handleUnban(b, e)
		case "mute":
			return handleMute(b, e)
		case "unmute":
			return handleUnmute(b, e)
		case "logs":
			return handleLogs(b, e)
		default:
			return e.Respond(discord.InteractionResponseTypeCreateMessage,
				discord.NewMessageCreateBuilder().
					SetEmbeds(utils.FailureEmbed("Invalid Command", "Unknown subcommand")).
					SetEphemeral(true).
					Build(),
			)
		}
	}
}

// Helper function to create a modlog entry
func createModlog(ctx context.Context, queries *db.Queries, params db.CreateModlogParams) error {
	_, err := queries.CreateModlog(ctx, params)
	return err
}

// Helper function to send modlog to channel
func sendModlogToChannel(b *mgbot.MartinGarrixBot, guildID, userID, moderatorID snowflake.ID, logType, reason string, expiresAt *time.Time) {
	config, err := b.Queries.GetGuild(context.Background(), int64(guildID))
	if err != nil || !config.ModlogsChannel.Valid {
		return
	}

	reasonText := reason
	if reasonText == "" {
		reasonText = "No reason provided"
	}

	embed := discord.NewEmbedBuilder().
		SetTitle(fmt.Sprintf("Moderation Action: %s", strings.ToUpper(logType))).
		AddField("User", fmt.Sprintf("<@%d>", userID), true).
		AddField("Moderator", fmt.Sprintf("<@%d>", moderatorID), true).
		AddField("Reason", reasonText, false).
		SetTimestamp(time.Now()).
		SetColor(utils.ColorWarning)

	if expiresAt != nil {
		embed.AddField("Expires", fmt.Sprintf("<t:%d:R>", expiresAt.Unix()), false)
	}

	_, err = b.Client.Rest().CreateMessage(snowflake.ID(config.ModlogsChannel.Int64),
		discord.NewMessageCreateBuilder().
			SetEmbeds(embed.Build()).
			Build(),
	)
	if err != nil {
		slog.Error("Failed to send modlog to channel", slog.Any("err", err))
	}
}

// parseDuration parses duration strings like "1h", "2d", "1w"
func parseDuration(durationStr string) (time.Duration, error) {
	durationStr = strings.TrimSpace(strings.ToLower(durationStr))
	if len(durationStr) < 2 {
		return 0, fmt.Errorf("invalid duration format")
	}

	valueStr := durationStr[:len(durationStr)-1]
	unit := durationStr[len(durationStr)-1:]

	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration value: %w", err)
	}

	if value <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}

	switch unit {
	case "s":
		return time.Duration(value) * time.Second, nil
	case "m":
		return time.Duration(value) * time.Minute, nil
	case "h":
		return time.Duration(value) * time.Hour, nil
	case "d":
		return time.Duration(value) * 24 * time.Hour, nil
	case "w":
		return time.Duration(value) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration unit (use s, m, h, d, or w)")
	}
}

func handleKick(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	reason := data.String("reason")
	guildID := *e.GuildID()
	moderator := e.User()

	// Kick the user
	err := b.Client.Rest().RemoveMember(guildID, targetUser.ID, rest.WithReason(reason))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Kick Failed", fmt.Sprintf("Failed to kick user: %s", err.Error()))).
				SetEphemeral(true).
				Build(),
		)
	}

	// Create modlog entry
	reasonText := pgtype.Text{String: reason, Valid: reason != ""}
	err = createModlog(e.Ctx, b.Queries, db.CreateModlogParams{
		UserID:      int64(targetUser.ID),
		ModeratorID: int64(moderator.ID),
		GuildID:     int64(guildID),
		LogType:     "kick",
		Reason:      reasonText,
		ExpiresAt:   pgtype.Timestamp{Valid: false},
		Active:      pgtype.Bool{Bool: true, Valid: true},
	})

	if err != nil {
		slog.Error("Failed to create modlog entry", slog.Any("err", err))
	}

	// Send to modlog channel
	sendModlogToChannel(b, guildID, targetUser.ID, moderator.ID, "kick", reason, nil)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(utils.SuccessEmbed("User Kicked", fmt.Sprintf("<@%d> has been kicked", targetUser.ID))).
			SetEphemeral(true).
			Build(),
	)
}

func handleBan(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	reason := data.String("reason")
	deleteMessageDays := 0
	if val, ok := data.OptInt("delete_message_days"); ok {
		deleteMessageDays = val
		if deleteMessageDays < 0 {
			deleteMessageDays = 0
		}
		if deleteMessageDays > 7 {
			deleteMessageDays = 7
		}
	}
	guildID := *e.GuildID()
	moderator := e.User()

	// Ban the user (convert days to duration)
	deleteMessageDuration := time.Duration(deleteMessageDays) * 24 * time.Hour
	err := b.Client.Rest().AddBan(guildID, targetUser.ID, deleteMessageDuration, rest.WithReason(reason))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Ban Failed", fmt.Sprintf("Failed to ban user: %s", err.Error()))).
				SetEphemeral(true).
				Build(),
		)
	}

	// Create modlog entry
	reasonText := pgtype.Text{String: reason, Valid: reason != ""}
	err = createModlog(e.Ctx, b.Queries, db.CreateModlogParams{
		UserID:      int64(targetUser.ID),
		ModeratorID: int64(moderator.ID),
		GuildID:     int64(guildID),
		LogType:     "ban",
		Reason:      reasonText,
		ExpiresAt:   pgtype.Timestamp{Valid: false},
		Active:      pgtype.Bool{Bool: true, Valid: true},
	})

	if err != nil {
		slog.Error("Failed to create modlog entry", slog.Any("err", err))
	}

	// Send to modlog channel
	sendModlogToChannel(b, guildID, targetUser.ID, moderator.ID, "ban", reason, nil)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(utils.SuccessEmbed("User Banned", fmt.Sprintf("<@%d> has been banned", targetUser.ID))).
			SetEphemeral(true).
			Build(),
	)
}

func handleTempBan(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	durationStr := data.String("duration")
	reason := data.String("reason")
	deleteMessageDays := 0
	if val, ok := data.OptInt("delete_message_days"); ok {
		deleteMessageDays = val
		if deleteMessageDays < 0 {
			deleteMessageDays = 0
		}
		if deleteMessageDays > 7 {
			deleteMessageDays = 7
		}
	}
	guildID := *e.GuildID()
	moderator := e.User()

	// Parse duration
	duration, err := parseDuration(durationStr)
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Invalid Duration", err.Error())).
				SetEphemeral(true).
				Build(),
		)
	}

	expiresAt := time.Now().Add(duration)

	// Ban the user
	deleteMessageDuration := time.Duration(deleteMessageDays) * 24 * time.Hour
	err = b.Client.Rest().AddBan(guildID, targetUser.ID, deleteMessageDuration, rest.WithReason(fmt.Sprintf("Tempban (%s): %s", durationStr, reason)))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Ban Failed", fmt.Sprintf("Failed to ban user: %s", err.Error()))).
				SetEphemeral(true).
				Build(),
		)
	}

	// Create modlog entry
	reasonText := pgtype.Text{String: reason, Valid: reason != ""}
	err = createModlog(e.Ctx, b.Queries, db.CreateModlogParams{
		UserID:      int64(targetUser.ID),
		ModeratorID: int64(moderator.ID),
		GuildID:     int64(guildID),
		LogType:     "tempban",
		Reason:      reasonText,
		ExpiresAt:   pgtype.Timestamp{Time: expiresAt, Valid: true},
		Active:      pgtype.Bool{Bool: true, Valid: true},
	})

	if err != nil {
		slog.Error("Failed to create modlog entry", slog.Any("err", err))
	}

	// Send to modlog channel
	sendModlogToChannel(b, guildID, targetUser.ID, moderator.ID, "tempban", reason, &expiresAt)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(utils.SuccessEmbed("User Temporarily Banned",
				fmt.Sprintf("<@%d> has been banned until <t:%d:F>", targetUser.ID, expiresAt.Unix()))).
			SetEphemeral(true).
			Build(),
	)
}

func handleSoftBan(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	reason := data.String("reason")
	deleteMessageDays := 7 // Default to 7 for softban
	if val, ok := data.OptInt("delete_message_days"); ok {
		deleteMessageDays = val
		if deleteMessageDays < 0 {
			deleteMessageDays = 0
		}
		if deleteMessageDays > 7 {
			deleteMessageDays = 7
		}
	}
	guildID := *e.GuildID()
	moderator := e.User()

	// Ban the user
	deleteMessageDuration := time.Duration(deleteMessageDays) * 24 * time.Hour
	err := b.Client.Rest().AddBan(guildID, targetUser.ID, deleteMessageDuration, rest.WithReason(fmt.Sprintf("Softban: %s", reason)))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Softban Failed", fmt.Sprintf("Failed to ban user: %s", err.Error()))).
				SetEphemeral(true).
				Build(),
		)
	}

	// Immediately unban
	err = b.Client.Rest().DeleteBan(guildID, targetUser.ID, rest.WithReason("Softban unban"))
	if err != nil {
		slog.Error("Failed to unban after softban", slog.Any("err", err))
		// Continue anyway, the ban was successful
	}

	// Create modlog entry
	reasonText := pgtype.Text{String: reason, Valid: reason != ""}
	err = createModlog(e.Ctx, b.Queries, db.CreateModlogParams{
		UserID:      int64(targetUser.ID),
		ModeratorID: int64(moderator.ID),
		GuildID:     int64(guildID),
		LogType:     "softban",
		Reason:      reasonText,
		ExpiresAt:   pgtype.Timestamp{Valid: false},
		Active:      pgtype.Bool{Bool: true, Valid: true},
	})

	if err != nil {
		slog.Error("Failed to create modlog entry", slog.Any("err", err))
	}

	// Send to modlog channel
	sendModlogToChannel(b, guildID, targetUser.ID, moderator.ID, "softban", reason, nil)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(utils.SuccessEmbed("User Softbanned",
				fmt.Sprintf("<@%d> has been softbanned (messages deleted)", targetUser.ID))).
			SetEphemeral(true).
			Build(),
	)
}

func handleUnban(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	reason := data.String("reason")
	guildID := *e.GuildID()
	moderator := e.User()

	// Unban the user
	err := b.Client.Rest().DeleteBan(guildID, targetUser.ID, rest.WithReason(reason))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Unban Failed", fmt.Sprintf("Failed to unban user: %s", err.Error()))).
				SetEphemeral(true).
				Build(),
		)
	}

	// Deactivate any active tempbans
	activeTempBan, err := b.Queries.GetActiveTempBanForUser(e.Ctx, db.GetActiveTempBanForUserParams{
		UserID:  int64(targetUser.ID),
		GuildID: int64(guildID),
	})
	if err == nil {
		_ = b.Queries.DeactivateModlog(e.Ctx, activeTempBan.ID)
	}

	// Create modlog entry
	reasonText := pgtype.Text{String: reason, Valid: reason != ""}
	err = createModlog(e.Ctx, b.Queries, db.CreateModlogParams{
		UserID:      int64(targetUser.ID),
		ModeratorID: int64(moderator.ID),
		GuildID:     int64(guildID),
		LogType:     "unban",
		Reason:      reasonText,
		ExpiresAt:   pgtype.Timestamp{Valid: false},
		Active:      pgtype.Bool{Bool: true, Valid: true},
	})

	if err != nil {
		slog.Error("Failed to create modlog entry", slog.Any("err", err))
	}

	// Send to modlog channel
	sendModlogToChannel(b, guildID, targetUser.ID, moderator.ID, "unban", reason, nil)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(utils.SuccessEmbed("User Unbanned", fmt.Sprintf("<@%d> has been unbanned", targetUser.ID))).
			SetEphemeral(true).
			Build(),
	)
}

func handleMute(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	durationStr := data.String("duration")
	reason := data.String("reason")
	guildID := *e.GuildID()
	moderator := e.User()

	// Parse duration
	duration, err := parseDuration(durationStr)
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Invalid Duration", err.Error())).
				SetEphemeral(true).
				Build(),
		)
	}

	// Discord timeout max is 28 days
	maxDuration := 28 * 24 * time.Hour
	if duration > maxDuration {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Duration Too Long", "Maximum timeout duration is 28 days")).
				SetEphemeral(true).
				Build(),
		)
	}

	expiresAt := time.Now().Add(duration)

	// Timeout the user
	timeoutUntil := json.NewNullable(expiresAt)
	_, err = b.Client.Rest().UpdateMember(guildID, targetUser.ID,
		discord.MemberUpdate{
			CommunicationDisabledUntil: &timeoutUntil,
		},
		rest.WithReason(reason),
	)
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Mute Failed", fmt.Sprintf("Failed to timeout user: %s", err.Error()))).
				SetEphemeral(true).
				Build(),
		)
	}

	// Create modlog entry
	reasonText := pgtype.Text{String: reason, Valid: reason != ""}
	err = createModlog(e.Ctx, b.Queries, db.CreateModlogParams{
		UserID:      int64(targetUser.ID),
		ModeratorID: int64(moderator.ID),
		GuildID:     int64(guildID),
		LogType:     "mute",
		Reason:      reasonText,
		ExpiresAt:   pgtype.Timestamp{Time: expiresAt, Valid: true},
		Active:      pgtype.Bool{Bool: true, Valid: true},
	})

	if err != nil {
		slog.Error("Failed to create modlog entry", slog.Any("err", err))
	}

	// Send to modlog channel
	sendModlogToChannel(b, guildID, targetUser.ID, moderator.ID, "mute", reason, &expiresAt)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(utils.SuccessEmbed("User Muted",
				fmt.Sprintf("<@%d> has been muted until <t:%d:F>", targetUser.ID, expiresAt.Unix()))).
			SetEphemeral(true).
			Build(),
	)
}

func handleUnmute(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	reason := data.String("reason")
	guildID := *e.GuildID()
	moderator := e.User()

	// Remove timeout
	_, err := b.Client.Rest().UpdateMember(guildID, targetUser.ID,
		discord.MemberUpdate{
			CommunicationDisabledUntil: nil,
		},
		rest.WithReason(reason),
	)
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Unmute Failed", fmt.Sprintf("Failed to remove timeout: %s", err.Error()))).
				SetEphemeral(true).
				Build(),
		)
	}

	// Deactivate any active mutes
	activeMute, err := b.Queries.GetActiveTempMuteForUser(e.Ctx, db.GetActiveTempMuteForUserParams{
		UserID:  int64(targetUser.ID),
		GuildID: int64(guildID),
	})
	if err == nil {
		_ = b.Queries.DeactivateModlog(e.Ctx, activeMute.ID)
	}

	// Create modlog entry
	reasonText := pgtype.Text{String: reason, Valid: reason != ""}
	err = createModlog(e.Ctx, b.Queries, db.CreateModlogParams{
		UserID:      int64(targetUser.ID),
		ModeratorID: int64(moderator.ID),
		GuildID:     int64(guildID),
		LogType:     "unmute",
		Reason:      reasonText,
		ExpiresAt:   pgtype.Timestamp{Valid: false},
		Active:      pgtype.Bool{Bool: true, Valid: true},
	})

	if err != nil {
		slog.Error("Failed to create modlog entry", slog.Any("err", err))
	}

	// Send to modlog channel
	sendModlogToChannel(b, guildID, targetUser.ID, moderator.ID, "unmute", reason, nil)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(utils.SuccessEmbed("User Unmuted", fmt.Sprintf("<@%d> has been unmuted", targetUser.ID))).
			SetEphemeral(true).
			Build(),
	)
}

func handleLogs(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	guildID := *e.GuildID()

	// Get total count
	totalCount, err := b.Queries.GetModlogsByUserCount(e.Ctx, db.GetModlogsByUserCountParams{
		UserID:  int64(targetUser.ID),
		GuildID: int64(guildID),
	})
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Error", "Failed to fetch moderation logs")).
				SetEphemeral(true).
				Build(),
		)
	}

	if totalCount == 0 {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.SuccessEmbed("No Logs Found",
					fmt.Sprintf("<@%d> has no moderation logs", targetUser.ID))).
				SetEphemeral(true).
				Build(),
		)
	}

	// Get first page of logs
	logs, err := b.Queries.GetModlogsByUser(e.Ctx, db.GetModlogsByUserParams{
		UserID:  int64(targetUser.ID),
		GuildID: int64(guildID),
		Limit:   utils.ModlogsPerPage,
		Offset:  0,
	})
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Error", "Failed to fetch moderation logs")).
				SetEphemeral(true).
				Build(),
		)
	}

	totalPages := utils.CalculateTotalPages(int(totalCount), utils.ModlogsPerPage)
	embed := utils.CreateModlogEmbed(logs, int64(targetUser.ID), 1, totalPages)
	buttons := utils.CreatePaginationButtons(1, totalPages, fmt.Sprintf("modlogs:%d", targetUser.ID))

	messageBuilder := discord.NewMessageCreateBuilder().
		SetEmbeds(embed).
		SetEphemeral(true)

	if len(buttons) > 0 {
		messageBuilder.SetContainerComponents(buttons...)
	}

	return e.Respond(discord.InteractionResponseTypeCreateMessage, messageBuilder.Build())
}
