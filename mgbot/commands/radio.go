package commands

import (
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var radio = discord.SlashCommandCreate{
	Name:        "radio",
	Description: "Control the 24/7 Martin Garrix radio",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionSubCommand{
			Name:        "start",
			Description: "Start the 24/7 radio in the configured channel",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "stop",
			Description: "Stop the 24/7 radio",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "nowplaying",
			Description: "Show the currently playing song",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "skip",
			Description: "Vote to skip the current song (requires >50% votes)",
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
		case "nowplaying":
			return handleRadioNowPlaying(b, e)
		case "skip":
			return handleRadioSkip(b, e)
		default:
			return e.CreateMessage(discord.NewMessageCreate().
				WithContent("Unknown subcommand").
				WithEphemeral(true),
			)
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
		_, err := e.UpdateInteractionResponse(discord.NewMessageUpdate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Radio Already Active").
				WithDescription("The 24/7 radio is already running in this server.").
				WithColor(utils.ColorWarning),
			),
		)
		return err
	}

	// Start the radio
	if err := b.StartRadioInGuild(e.Ctx, guildID); err != nil {
		_, updateErr := e.UpdateInteractionResponse(discord.NewMessageUpdate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Failed to Start Radio").
				WithDescription(fmt.Sprintf("Error: %s", err.Error())).
				WithColor(utils.ColorDanger),
			),
		)
		if updateErr != nil {
			return updateErr
		}
		return err
	}

	_, err := e.UpdateInteractionResponse(discord.NewMessageUpdate().
		WithEmbeds(discord.NewEmbed().
			WithTitle("Radio Started").
			WithDescription("The 24/7 Martin Garrix radio has been started!").
			WithColor(utils.ColorSuccess),
		),
	)
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
		_, err := e.UpdateInteractionResponse(discord.NewMessageUpdate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Radio Not Active").
				WithDescription("The 24/7 radio is not currently running in this server.").
				WithColor(utils.ColorWarning),
			),
		)
		return err
	}

	// Stop the radio
	if err := b.StopRadioInGuild(e.Ctx, guildID); err != nil {
		_, updateErr := e.UpdateInteractionResponse(discord.NewMessageUpdate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Failed to Stop Radio").
				WithDescription(fmt.Sprintf("Error: %s", err.Error())).
				WithColor(utils.ColorDanger),
			),
		)
		if updateErr != nil {
			return updateErr
		}
		return err
	}

	_, err := e.UpdateInteractionResponse(discord.NewMessageUpdate().
		WithEmbeds(discord.NewEmbed().
			WithTitle("Radio Stopped").
			WithDescription("The 24/7 radio has been stopped.").
			WithColor(utils.ColorSuccess),
		),
	)
	return err
}

func handleRadioNowPlaying(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	guildID := *e.GuildID()

	// Check if radio is active
	if !b.RadioManager.IsActive(guildID) {
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Radio Not Active").
				WithDescription("The 24/7 radio is not currently running in this server.").
				WithColor(utils.ColorWarning),
			).
			WithEphemeral(true),
		)
	}

	// Get current track info
	trackInfo, exists := b.RadioManager.GetCurrentTrack(guildID)
	if !exists {
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("No Track Playing").
				WithDescription("No track information available.").
				WithColor(utils.ColorWarning),
			).
			WithEphemeral(true),
		)
	}

	// Check if we have a song ID to query
	if trackInfo.SongID == 0 {
		// Fallback: display basic info without database details
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Now Playing").
				WithDescription(fmt.Sprintf("**%s - %s**", trackInfo.Artist, trackInfo.SongName)).
				WithColor(utils.ColorSuccess),
			),
		)
	}

	// Get the song from database by ID to fetch links and thumbnail
	song, err := b.Queries.GetSongByID(e.Ctx, trackInfo.SongID)
	if err != nil {
		// Fallback: send basic info without database details
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Now Playing").
				WithDescription(fmt.Sprintf("**%s - %s**", trackInfo.Artist, trackInfo.SongName)).
				WithColor(utils.ColorSuccess),
			),
		)
	}

	// Build embed with full song info
	embed := discord.NewEmbed().
		WithTitle("Now Playing").
		WithDescription(fmt.Sprintf("**%s - %s**", song.Artists, song.Name)).
		WithColor(utils.ColorSuccess)

	if song.ThumbnailUrl.Valid {
		embed = embed.WithImage(song.ThumbnailUrl.String)
	}

	messageBuilder := discord.NewMessageCreate().WithEmbeds(embed)

	// Add buttons if any streaming links are available
	if song.SpotifyUrl.Valid || song.YoutubeUrl.Valid || song.AppleMusicUrl.Valid {
		messageBuilder = messageBuilder.AddActionRow(utils.GetSongButtons(song)...)
	}

	return e.CreateMessage(messageBuilder)
}

func handleRadioSkip(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	guildID := *e.GuildID()
	userID := e.User().ID

	// Check if radio is active
	if !b.RadioManager.IsActive(guildID) {
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Radio Not Active").
				WithDescription("The 24/7 radio is not currently running in this server.").
				WithColor(utils.ColorWarning),
			).
			WithEphemeral(true),
		)
	}

	// Check if user is in a voice channel (uses cache-first-then-REST utility)
	voiceState, err := utils.GetVoiceState(b.Client, guildID, userID)
	if err != nil || voiceState.ChannelID == nil {
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Not in Voice Channel").
				WithDescription("You must be in the radio voice channel to vote for skip.").
				WithColor(utils.ColorDanger),
			).
			WithEphemeral(true),
		)
	}

	if voiceState.ChannelID == nil {
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Not in Voice Channel").
				WithDescription("You must be in the radio voice channel to vote for skip.").
				WithColor(utils.ColorDanger),
			).
			WithEphemeral(true),
		)
	}

	// Get the radio voice channel ID
	player := b.RadioManager.Client.ExistingPlayer(guildID)
	if player == nil || player.Voice.ChannelID == 0 {
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Error").
				WithDescription("Could not find radio channel information.").
				WithColor(utils.ColorDanger),
			).
			WithEphemeral(true),
		)
	}

	// Check if user is in the same channel as the bot
	if *voiceState.ChannelID != player.Voice.ChannelID {
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Wrong Voice Channel").
				WithDescription("You must be in the same voice channel as the bot to vote for skip.").
				WithColor(utils.ColorDanger),
			).
			WithEphemeral(true),
		)
	}

	// Count members in voice channel (excluding bots)
	humanCount := utils.CountHumansInVoiceChannel(b.Client, guildID, player.Voice.ChannelID)

	if humanCount == 0 {
		return e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Error").
				WithDescription("No members found in voice channel.").
				WithColor(utils.ColorDanger),
			).
			WithEphemeral(true),
		)
	}

	// Add the skip vote
	votesNeeded, currentVotes, shouldSkip := b.RadioManager.AddSkipVote(guildID, userID, humanCount)

	if shouldSkip {
		// Reset votes
		b.RadioManager.ResetSkipVotes(guildID)

		// Respond immediately
		if err := e.CreateMessage(discord.NewMessageCreate().
			WithEmbeds(discord.NewEmbed().
				WithTitle("Song Skipped!").
				WithDescription(fmt.Sprintf("Vote passed! (%d/%d votes) Skipping to next song...", currentVotes, votesNeeded)).
				WithColor(utils.ColorSuccess),
			),
		); err != nil {
			return err
		}

		// Skip to next song
		go func() {
			time.Sleep(500 * time.Millisecond)
			b.PlayNextRadioSong(guildID)
		}()

		return nil
	}

	// Vote recorded but not enough yet
	return e.CreateMessage(discord.NewMessageCreate().
		WithEmbeds(discord.NewEmbed().
			WithTitle("Skip Vote Recorded").
			WithDescription(fmt.Sprintf("Vote recorded! Need %d votes to skip (currently %d/%d).", votesNeeded, currentVotes, votesNeeded)).
			WithColor(utils.ColorInfo),
		),
	)
}
