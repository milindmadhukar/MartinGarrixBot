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
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
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

func ModerationHandler(b *stmpdbot.STMPDBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		// Check if the user has moderator permissions
		if !utils.HasModeratorPermissions(e.Ctx, b.DB, b.Client.Rest, *e.GuildID(), e.Member()) {
			return e.Respond(discord.InteractionResponseTypeCreateMessage,
				discord.NewMessageCreate().
					WithEmbeds(utils.FailureEmbed("Permission Denied",
						"You need Administrator permission or the Moderator role to use moderation commands.")).
					WithEphemeral(true),
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
				discord.NewMessageCreate().
					WithEmbeds(utils.FailureEmbed("Invalid Command", "Unknown subcommand")).
					WithEphemeral(true),
			)
		}
	}
}

// Helper function to create a modlog entry
func createModlog(ctx context.Context, queries *db.Queries, params db.CreateModlogParams) error {
	_, err := queries.CreateModlog(ctx, params)
	return err
}

// sendModlogToChannel delegates to stmpdbot.SendModlogEmbed, which the audit-log
// listener also uses so that moderation done through Discord's UI produces an
// identical embed.
func sendModlogToChannel(b *stmpdbot.STMPDBot, guildID, userID, moderatorID snowflake.ID, logType, reason string, expiresAt *time.Time) {
	stmpdbot.SendModlogEmbed(b, guildID, userID, moderatorID, logType, reason, expiresAt)
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

func handleKick(b *stmpdbot.STMPDBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	reason := data.String("reason")
	guildID := *e.GuildID()
	moderator := e.User()

	// Kick the user
	err := b.Client.Rest.RemoveMember(guildID, targetUser.ID, rest.WithReason(reason))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Kick Failed", fmt.Sprintf("Failed to kick user: %s", err.Error()))).
				WithEphemeral(true),
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
		discord.NewMessageCreate().
			WithEmbeds(utils.SuccessEmbed("User Kicked", fmt.Sprintf("<@%d> has been kicked", targetUser.ID))).
			WithEphemeral(true),
	)
}

func handleBan(b *stmpdbot.STMPDBot, e *handler.CommandEvent) error {
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
	err := b.Client.Rest.AddBan(guildID, targetUser.ID, deleteMessageDuration, rest.WithReason(reason))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Ban Failed", fmt.Sprintf("Failed to ban user: %s", err.Error()))).
				WithEphemeral(true),
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
		discord.NewMessageCreate().
			WithEmbeds(utils.SuccessEmbed("User Banned", fmt.Sprintf("<@%d> has been banned", targetUser.ID))).
			WithEphemeral(true),
	)
}

func handleTempBan(b *stmpdbot.STMPDBot, e *handler.CommandEvent) error {
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
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Invalid Duration", err.Error())).
				WithEphemeral(true),
		)
	}

	expiresAt := time.Now().Add(duration)

	// Ban the user
	deleteMessageDuration := time.Duration(deleteMessageDays) * 24 * time.Hour
	err = b.Client.Rest.AddBan(guildID, targetUser.ID, deleteMessageDuration, rest.WithReason(fmt.Sprintf("Tempban (%s): %s", durationStr, reason)))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Ban Failed", fmt.Sprintf("Failed to ban user: %s", err.Error()))).
				WithEphemeral(true),
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
		discord.NewMessageCreate().
			WithEmbeds(utils.SuccessEmbed("User Temporarily Banned",
				fmt.Sprintf("<@%d> has been banned until <t:%d:F>", targetUser.ID, expiresAt.Unix()))).
			WithEphemeral(true),
	)
}

func handleSoftBan(b *stmpdbot.STMPDBot, e *handler.CommandEvent) error {
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
	err := b.Client.Rest.AddBan(guildID, targetUser.ID, deleteMessageDuration, rest.WithReason(fmt.Sprintf("Softban: %s", reason)))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Softban Failed", fmt.Sprintf("Failed to ban user: %s", err.Error()))).
				WithEphemeral(true),
		)
	}

	// Immediately unban
	err = b.Client.Rest.DeleteBan(guildID, targetUser.ID, rest.WithReason("Softban unban"))
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
		discord.NewMessageCreate().
			WithEmbeds(utils.SuccessEmbed("User Softbanned",
				fmt.Sprintf("<@%d> has been softbanned (messages deleted)", targetUser.ID))).
			WithEphemeral(true),
	)
}

func handleUnban(b *stmpdbot.STMPDBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	reason := data.String("reason")
	guildID := *e.GuildID()
	moderator := e.User()

	// Unban the user
	err := b.Client.Rest.DeleteBan(guildID, targetUser.ID, rest.WithReason(reason))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Unban Failed", fmt.Sprintf("Failed to unban user: %s", err.Error()))).
				WithEphemeral(true),
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
		discord.NewMessageCreate().
			WithEmbeds(utils.SuccessEmbed("User Unbanned", fmt.Sprintf("<@%d> has been unbanned", targetUser.ID))).
			WithEphemeral(true),
	)
}

func handleMute(b *stmpdbot.STMPDBot, e *handler.CommandEvent) error {
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
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Invalid Duration", err.Error())).
				WithEphemeral(true),
		)
	}

	// Discord timeout max is 28 days
	maxDuration := 28 * 24 * time.Hour
	if duration > maxDuration {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Duration Too Long", "Maximum timeout duration is 28 days")).
				WithEphemeral(true),
		)
	}

	expiresAt := time.Now().Add(duration)

	// Timeout the user
	_, err = b.Client.Rest.UpdateMember(guildID, targetUser.ID,
		discord.MemberUpdate{
			CommunicationDisabledUntil: omit.NewPtr(expiresAt),
		},
		rest.WithReason(reason),
	)
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Mute Failed", fmt.Sprintf("Failed to timeout user: %s", err.Error()))).
				WithEphemeral(true),
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
		discord.NewMessageCreate().
			WithEmbeds(utils.SuccessEmbed("User Muted",
				fmt.Sprintf("<@%d> has been muted until <t:%d:F>", targetUser.ID, expiresAt.Unix()))).
			WithEphemeral(true),
	)
}

func handleUnmute(b *stmpdbot.STMPDBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	targetUser := data.User("user")
	reason := data.String("reason")
	guildID := *e.GuildID()
	moderator := e.User()

	// Remove timeout. This must be an explicit JSON null: the field is omitted
	// when the Omit is left zero, which is what the pre-v0.19 `nil` did here --
	// it sent an empty PATCH body and never actually cleared the timeout.
	_, err := b.Client.Rest.UpdateMember(guildID, targetUser.ID,
		discord.MemberUpdate{
			CommunicationDisabledUntil: omit.NewNilPtr[time.Time](),
		},
		rest.WithReason(reason),
	)
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Unmute Failed", fmt.Sprintf("Failed to remove timeout: %s", err.Error()))).
				WithEphemeral(true),
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
		discord.NewMessageCreate().
			WithEmbeds(utils.SuccessEmbed("User Unmuted", fmt.Sprintf("<@%d> has been unmuted", targetUser.ID))).
			WithEphemeral(true),
	)
}

func handleLogs(b *stmpdbot.STMPDBot, e *handler.CommandEvent) error {
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
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Error", "Failed to fetch moderation logs")).
				WithEphemeral(true),
		)
	}

	if totalCount == 0 {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(utils.SuccessEmbed("No Logs Found",
					fmt.Sprintf("<@%d> has no moderation logs", targetUser.ID))).
				WithEphemeral(true),
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
			discord.NewMessageCreate().
				WithEmbeds(utils.FailureEmbed("Error", "Failed to fetch moderation logs")).
				WithEphemeral(true),
		)
	}

	totalPages := utils.CalculateTotalPages(int(totalCount), utils.ModlogsPerPage)
	embed := utils.CreateModlogEmbed(logs, int64(targetUser.ID), 1, totalPages)
	buttons := utils.CreatePaginationButtons(1, totalPages, modlogsCustomID(targetUser.ID))

	messageBuilder := discord.NewMessageCreate().
		WithEmbeds(embed).
		WithEphemeral(true)

	if len(buttons) > 0 {
		messageBuilder = messageBuilder.WithComponents(buttons...)
	}

	return e.Respond(discord.InteractionResponseTypeCreateMessage, messageBuilder)
}

// modlogsCustomID builds the router path that the modlog pagination buttons
// carry as their component custom ID.
func modlogsCustomID(userID snowflake.ID) string {
	return fmt.Sprintf("/modlogs/%d", userID)
}

// ModlogsPaginationHandler backs the navigation buttons attached to
// /moderation logs. The buttons existed before but nothing was routed to them,
// so clicking one only produced "This interaction failed".
func ModlogsPaginationHandler(b *stmpdbot.STMPDBot) handler.ComponentHandler {
	return func(e *handler.ComponentEvent) error {
		// The page counter in the middle is a disabled no-op button.
		if e.Vars["action"] == "current" {
			return e.DeferUpdateMessage()
		}

		// The message is ephemeral, so only the invoker can click these. Re-check
		// anyway, in case their moderator role was revoked while it was open.
		if !utils.HasModeratorPermissions(e.Ctx, b.DB, b.Client.Rest, *e.GuildID(), e.Member()) {
			return e.DeferUpdateMessage()
		}

		userID, err := strconv.ParseInt(e.Vars["userID"], 10, 64)
		if err != nil {
			return err
		}
		currentPage, err := strconv.Atoi(e.Vars["page"])
		if err != nil {
			return err
		}
		guildID := *e.GuildID()

		totalCount, err := b.Queries.GetModlogsByUserCount(e.Ctx, db.GetModlogsByUserCountParams{
			UserID:  userID,
			GuildID: int64(guildID),
		})
		if err != nil {
			return err
		}

		totalPages := utils.CalculateTotalPages(int(totalCount), utils.ModlogsPerPage)
		if totalPages < 1 {
			totalPages = 1
		}

		page := currentPage
		switch e.Vars["action"] {
		case "first":
			page = 1
		case "prev":
			page = currentPage - 1
		case "next":
			page = currentPage + 1
		case "last":
			page = totalPages
		}
		page = min(max(page, 1), totalPages)

		logs, err := b.Queries.GetModlogsByUser(e.Ctx, db.GetModlogsByUserParams{
			UserID:  userID,
			GuildID: int64(guildID),
			Limit:   utils.ModlogsPerPage,
			Offset:  int32((page - 1) * utils.ModlogsPerPage),
		})
		if err != nil {
			return err
		}

		return e.UpdateMessage(discord.NewMessageUpdate().
			WithEmbeds(utils.CreateModlogEmbed(logs, userID, page, totalPages)).
			WithComponents(utils.CreatePaginationButtons(page, totalPages, modlogsCustomID(snowflake.ID(userID)))...))
	}
}
