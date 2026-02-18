package commands

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/json"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var radio = discord.SlashCommandCreate{
	Name:                     "radio",
	Description:              "Control the 24/7 Martin Garrix radio",
	DefaultMemberPermissions: json.NewNullablePtr(discord.PermissionAdministrator),
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionSubCommand{
			Name:        "start",
			Description: "Start the 24/7 radio in the configured channel",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "stop",
			Description: "Stop the 24/7 radio",
		},
	},
}

func RadioHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		data := e.SlashCommandInteractionData()
		subcommand := data.SubCommandName

		switch *subcommand {
		case "start":
			return handleRadioStart(b, e)
		case "stop":
			return handleRadioStop(b, e)
		default:
			return e.CreateMessage(discord.NewMessageCreateBuilder().
				SetContent("Unknown subcommand").
				SetEphemeral(true).
				Build())
		}
	}
}

func handleRadioStart(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	// Defer the response since starting the radio might take time
	if err := e.DeferCreateMessage(false); err != nil {
		return err
	}

	guildID := *e.GuildID()

	// Check if radio is already active
	if b.RadioManager.IsActive(guildID) {
		_, err := e.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Radio Already Active").
				SetDescription("The 24/7 radio is already running in this server.").
				SetColor(utils.ColorWarning).
				Build()).
			Build())
		return err
	}

	// Start the radio
	if err := b.StartRadioInGuild(e.Ctx, guildID); err != nil {
		_, updateErr := e.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Failed to Start Radio").
				SetDescription(fmt.Sprintf("Error: %s", err.Error())).
				SetColor(utils.ColorDanger).
				Build()).
			Build())
		if updateErr != nil {
			return updateErr
		}
		return err
	}

	_, err := e.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
		SetEmbeds(discord.NewEmbedBuilder().
			SetTitle("Radio Started").
			SetDescription("The 24/7 Martin Garrix radio has been started!").
			SetColor(utils.ColorSuccess).
			Build()).
		Build())
	return err
}

func handleRadioStop(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	// Defer the response
	if err := e.DeferCreateMessage(false); err != nil {
		return err
	}

	guildID := *e.GuildID()

	// Check if radio is active
	if !b.RadioManager.IsActive(guildID) {
		_, err := e.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Radio Not Active").
				SetDescription("The 24/7 radio is not currently running in this server.").
				SetColor(utils.ColorWarning).
				Build()).
			Build())
		return err
	}

	// Stop the radio
	if err := b.StopRadioInGuild(e.Ctx, guildID); err != nil {
		_, updateErr := e.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Failed to Stop Radio").
				SetDescription(fmt.Sprintf("Error: %s", err.Error())).
				SetColor(utils.ColorDanger).
				Build()).
			Build())
		if updateErr != nil {
			return updateErr
		}
		return err
	}

	_, err := e.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
		SetEmbeds(discord.NewEmbedBuilder().
			SetTitle("Radio Stopped").
			SetDescription("The 24/7 radio has been stopped.").
			SetColor(utils.ColorSuccess).
			Build()).
		Build())
	return err
}
