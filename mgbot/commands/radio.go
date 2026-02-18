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

func handleRadioNowPlaying(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	guildID := *e.GuildID()

	// Check if radio is active
	if !b.RadioManager.IsActive(guildID) {
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Radio Not Active").
				SetDescription("The 24/7 radio is not currently running in this server.").
				SetColor(utils.ColorWarning).
				Build()).
			SetEphemeral(true).
			Build())
	}

	// Get current track info
	trackInfo, exists := b.RadioManager.GetCurrentTrack(guildID)
	if !exists {
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("No Track Playing").
				SetDescription("No track information available.").
				SetColor(utils.ColorWarning).
				Build()).
			SetEphemeral(true).
			Build())
	}

	// Check if we have a song ID to query
	if trackInfo.SongID == 0 {
		// Fallback: display basic info without database details
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Now Playing").
				SetDescription(fmt.Sprintf("**%s - %s**", trackInfo.Artist, trackInfo.SongName)).
				SetColor(utils.ColorSuccess).
				Build()).
			Build())
	}

	// Get the song from database by ID to fetch links and thumbnail
	song, err := b.Queries.GetSongByID(e.Ctx, trackInfo.SongID)
	if err != nil {
		// Fallback: send basic info without database details
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Now Playing").
				SetDescription(fmt.Sprintf("**%s - %s**", trackInfo.Artist, trackInfo.SongName)).
				SetColor(utils.ColorSuccess).
				Build()).
			Build())
	}

	// Build embed with full song info
	embed := discord.NewEmbedBuilder().
		SetTitle("Now Playing").
		SetDescription(fmt.Sprintf("**%s - %s**", song.Artists, song.Name)).
		SetColor(utils.ColorSuccess)

	if song.ThumbnailUrl.Valid {
		embed.SetImage(song.ThumbnailUrl.String)
	}

	messageBuilder := discord.NewMessageCreateBuilder().SetEmbeds(embed.Build())

	// Add buttons if any streaming links are available
	if song.SpotifyUrl.Valid || song.YoutubeUrl.Valid || song.AppleMusicUrl.Valid {
		messageBuilder.AddActionRow(utils.GetSongButtons(song)...)
	}

	return e.CreateMessage(messageBuilder.Build())
}

func handleRadioSkip(b *mgbot.MartinGarrixBot, e *handler.CommandEvent) error {
	guildID := *e.GuildID()
	userID := e.User().ID

	// Check if radio is active
	if !b.RadioManager.IsActive(guildID) {
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Radio Not Active").
				SetDescription("The 24/7 radio is not currently running in this server.").
				SetColor(utils.ColorWarning).
				Build()).
			SetEphemeral(true).
			Build())
	}

	// Check if user is in the voice channel
	voiceState, ok := b.Client.Caches().VoiceState(*e.GuildID(), userID)
	if !ok || voiceState.ChannelID == nil {
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Not in Voice Channel").
				SetDescription("You must be in the radio voice channel to vote for skip.").
				SetColor(utils.ColorDanger).
				Build()).
			SetEphemeral(true).
			Build())
	}

	// Get the radio voice channel ID
	player := b.RadioManager.Client.ExistingPlayer(guildID)
	if player == nil || player.ChannelID() == nil {
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Error").
				SetDescription("Could not find radio channel information.").
				SetColor(utils.ColorDanger).
				Build()).
			SetEphemeral(true).
			Build())
	}

	// Check if user is in the same channel as the bot
	if *voiceState.ChannelID != *player.ChannelID() {
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Wrong Voice Channel").
				SetDescription("You must be in the same voice channel as the bot to vote for skip.").
				SetColor(utils.ColorDanger).
				Build()).
			SetEphemeral(true).
			Build())
	}

	// Count members in voice channel (excluding bots)
	humanCount := 0
	b.Client.Caches().VoiceStatesForEach(guildID, func(vs discord.VoiceState) {
		if vs.ChannelID != nil && *vs.ChannelID == *player.ChannelID() {
			member, ok := b.Client.Caches().Member(guildID, vs.UserID)
			if ok && !member.User.Bot {
				humanCount++
			}
		}
	})

	if humanCount == 0 {
		return e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Error").
				SetDescription("No members found in voice channel.").
				SetColor(utils.ColorDanger).
				Build()).
			SetEphemeral(true).
			Build())
	}

	// Add the skip vote
	votesNeeded, currentVotes, shouldSkip := b.RadioManager.AddSkipVote(guildID, userID, humanCount)

	if shouldSkip {
		// Reset votes
		b.RadioManager.ResetSkipVotes(guildID)

		// Respond immediately
		if err := e.CreateMessage(discord.NewMessageCreateBuilder().
			SetEmbeds(discord.NewEmbedBuilder().
				SetTitle("Song Skipped!").
				SetDescription(fmt.Sprintf("Vote passed! (%d/%d votes) Skipping to next song...", currentVotes, votesNeeded)).
				SetColor(utils.ColorSuccess).
				Build()).
			Build()); err != nil {
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
	return e.CreateMessage(discord.NewMessageCreateBuilder().
		SetEmbeds(discord.NewEmbedBuilder().
			SetTitle("Skip Vote Recorded").
			SetDescription(fmt.Sprintf("Vote recorded! Need %d votes to skip (currently %d/%d).", votesNeeded, currentVotes, votesNeeded)).
			SetColor(utils.ColorInfo).
			Build()).
		Build())
}
