package commands

import (
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var links = discord.SlashCommandCreate{
	Name:        "links",
	Description: "Get the streaming links to songs",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:         "song",
			Description:  "The name of the song to get the links of.",
			Required:     true,
			Autocomplete: true,
		},
	},
}

// PERF: Implement some sort of caching, we are hitting the database for every autocomplete request.
func LinksAutocompleteHandler(b *mgbot.MartinGarrixBot) handler.AutocompleteHandler {
	return func(e *handler.AutocompleteEvent) error {
		var songChoices []utils.SongChoice
		autocompleteInput := e.Data.String("song")

		if autocompleteInput == "" {
			songs, err := b.Queries.GetRandomSongNames(e.Ctx)
			if err != nil {
				slog.Error("Failed to get random song names", slog.Any("err", err))
				return err
			}
			for _, song := range songs {
				songChoices = append(songChoices, utils.SongChoice{
					ID: song.ID, Name: song.Name, Artists: song.Artists,
					Mix: song.MixName.String,
				})
			}
		} else {
			songs, err := b.Queries.GetSongsLike(e.Ctx, "%"+autocompleteInput+"%")
			if err != nil {
				slog.Error("Failed to get songs like", slog.Any("err", err))
				return err
			}
			for _, song := range songs {
				songChoices = append(songChoices, utils.SongChoice{
					ID: song.ID, Name: song.Name, Artists: song.Artists,
					Mix: song.MixName.String,
				})
			}
		}

		return e.AutocompleteResult(utils.BuildSongChoices(songChoices))
	}
}

func LinksHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		songID, ok := utils.ParseSongChoice(e.SlashCommandInteractionData().String("song"))
		if !ok {
			// The user submitted free text instead of picking a suggestion.
			return e.Respond(discord.InteractionResponseTypeCreateMessage,
				discord.NewMessageCreateBuilder().
					SetEmbeds(discord.NewEmbedBuilder().
						SetDescription("Please pick a song from the suggestions.").
						SetColor(utils.ColorWarning).
						Build()).
					SetEphemeral(true).
					Build())
		}

		song, err := b.Queries.GetSongByID(e.Ctx, songID)
		if err != nil {
			return err
		}

		// Ask for the buttons rather than testing three columns by hand: a song with
		// only a Beatport or Deezer link has something to show, and the old check
		// declared it linkless.
		buttonRows := utils.GetSongButtonRows(song)
		if len(buttonRows) == 0 {
			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
					SetEmbeds(
						utils.FailureEmbed("No streaming links found for this song.", ""),
					).
					Build(),
			)
		}

		embed := discord.NewEmbedBuilder().
			SetTitle(utils.SongHeading(song.Artists, song.Name, song.MixName.String)).
			SetColor(utils.ColorSuccess).
			SetImage(song.ThumbnailUrl.String).
			Build()

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
				SetEmbeds(embed).
				AddContainerComponents(buttonRows...).
				Build(),
		)
	}
}
