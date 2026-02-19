package commands

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var config = discord.SlashCommandCreate{
	Name:        "config",
	Description: "Configure bot settings for this server",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionSubCommand{
			Name:        "set-moderator-role",
			Description: "Set the moderator role for this server",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionRole{
					Name:        "role",
					Description: "The role that should have moderator permissions",
					Required:    true,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "view",
			Description: "View current server configuration",
		},
	},
}

func ConfigHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		// Check if the user has Administrator permission
		if !e.Member().Permissions.Has(discord.PermissionAdministrator) {
			return e.Respond(discord.InteractionResponseTypeCreateMessage,
				discord.NewMessageCreateBuilder().
					SetEmbeds(utils.FailureEmbed("Permission Denied",
						"Only administrators can configure bot settings.")).
					SetEphemeral(true).
					Build(),
			)
		}

		data := e.SlashCommandInteractionData()
		subcommand := data.SubCommandName

		switch *subcommand {
		case "set-moderator-role":
			return handleSetModeratorRole(b, e)
		case "view":
			return handleViewConfig(b, e)
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

func handleSetModeratorRole(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	data := e.SlashCommandInteractionData()
	role := data.Role("role")
	guildID := *e.GuildID()

	// Update the moderator role in the database
	err := b.Queries.SetModeratorRole(e.Ctx, db.SetModeratorRoleParams{
		GuildID: int64(guildID),
		ModeratorRole: pgtype.Int8{
			Int64: int64(role.ID),
			Valid: true,
		},
	})

	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Configuration Failed",
					fmt.Sprintf("Failed to update moderator role: %s", err.Error()))).
				SetEphemeral(true).
				Build(),
		)
	}

	embed := discord.NewEmbedBuilder().
		SetTitle("Moderator Role Updated").
		SetDescription(fmt.Sprintf("The moderator role has been set to <@&%d>", role.ID)).
		AddField("What this means",
			"Members with this role (or Administrator permission) can now use moderation commands like kick, ban, mute, etc.",
			false).
		SetColor(utils.ColorSuccess)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(embed.Build()).
			Build(),
	)
}

func handleViewConfig(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	guildID := *e.GuildID()

	// Get guild configuration
	config, err := b.Queries.GetGuild(e.Ctx, int64(guildID))
	if err != nil {
		return e.Respond(discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.FailureEmbed("Error", "Failed to fetch server configuration")).
				SetEphemeral(true).
				Build(),
		)
	}

	embed := discord.NewEmbedBuilder().
		SetTitle("Server Configuration").
		SetColor(utils.ColorInfo)

	// Moderator Role
	if config.ModeratorRole.Valid {
		embed.AddField("Moderator Role", fmt.Sprintf("<@&%d>", config.ModeratorRole.Int64), true)
	} else {
		embed.AddField("Moderator Role", "Not set (using default permissions)", true)
	}

	// Modlogs Channel
	if config.ModlogsChannel.Valid {
		embed.AddField("Moderation Logs Channel", fmt.Sprintf("<#%d>", config.ModlogsChannel.Int64), true)
	} else {
		embed.AddField("Moderation Logs Channel", "Not set", true)
	}

	// Bot Channel
	if config.BotChannel.Valid {
		embed.AddField("Bot Channel", fmt.Sprintf("<#%d>", config.BotChannel.Int64), true)
	} else {
		embed.AddField("Bot Channel", "Not set", true)
	}

	// Radio Voice Channel
	if config.RadioVoiceChannel.Valid {
		embed.AddField("Radio Voice Channel", fmt.Sprintf("<#%d>", config.RadioVoiceChannel.Int64), true)
	} else {
		embed.AddField("Radio Voice Channel", "Not set", true)
	}

	// XP Multiplier
	embed.AddField("XP Multiplier", fmt.Sprintf("%.1fx", config.XpMultiplier), true)

	// Notifications section
	notificationsText := ""

	if config.YoutubeNotificationsChannel.Valid {
		notificationsText += fmt.Sprintf("**YouTube:** <#%d>", config.YoutubeNotificationsChannel.Int64)
		if config.YoutubeNotificationsRole.Valid {
			notificationsText += fmt.Sprintf(" (<@&%d>)", config.YoutubeNotificationsRole.Int64)
		}
		notificationsText += "\n"
	}

	if config.RedditNotificationsChannel.Valid {
		notificationsText += fmt.Sprintf("**Reddit:** <#%d>", config.RedditNotificationsChannel.Int64)
		if config.RedditNotificationsRole.Valid {
			notificationsText += fmt.Sprintf(" (<@&%d>)", config.RedditNotificationsRole.Int64)
		}
		notificationsText += "\n"
	}

	if config.StmpdNotificationsChannel.Valid {
		notificationsText += fmt.Sprintf("**STMPD:** <#%d>", config.StmpdNotificationsChannel.Int64)
		if config.StmpdNotificationsRole.Valid {
			notificationsText += fmt.Sprintf(" (<@&%d>)", config.StmpdNotificationsRole.Int64)
		}
		notificationsText += "\n"
	}

	if config.TourNotificationsChannel.Valid {
		notificationsText += fmt.Sprintf("**Tour:** <#%d>", config.TourNotificationsChannel.Int64)
		if config.TourNotificationsRole.Valid {
			notificationsText += fmt.Sprintf(" (<@&%d>)", config.TourNotificationsRole.Int64)
		}
		notificationsText += "\n"
	}

	if notificationsText == "" {
		notificationsText = "No notification channels configured"
	}

	embed.AddField("Notification Channels", notificationsText, false)

	return e.Respond(discord.InteractionResponseTypeCreateMessage,
		discord.NewMessageCreateBuilder().
			SetEmbeds(embed.Build()).
			SetEphemeral(true).
			Build(),
	)
}
