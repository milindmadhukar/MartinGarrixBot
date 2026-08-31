package commands

import (
	"fmt"
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var lyrics = discord.SlashCommandCreate{
	Name:        "lyrics",
	Description: "Get the lyrics of any Martin Garrix, Area 21, GRX or YTRAM song.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:         "song",
			Description:  "The name of the song to get the lyrics of.",
			Required:     true,
			Autocomplete: true,
		},
	},
}

// PERF: Implement some sort of caching, we are hitting the database for every autocomplete request.
func LyricsAutocompleteHandler(b *mgbot.MartinGarrixBot) handler.AutocompleteHandler {
	return func(e *handler.AutocompleteEvent) error {
		var songChoices []utils.SongChoice
		autocompleteInput := e.Data.String("song")

		if autocompleteInput == "" {
			songs, err := b.Queries.GetRandomSongNamesWithLyrics(e.Ctx)
			if err != nil {
				slog.Error("Failed to get random song names with lyrics", slog.Any("err", err))
				return err
			}
			for _, song := range songs {
				songChoices = append(songChoices, utils.SongChoice{
					ID: song.ID, Name: song.Name, Artists: song.Artists,
				})
			}
		} else {
			songs, err := b.Queries.GetSongsWithLyricsLike(e.Ctx, "%"+autocompleteInput+"%")
			if err != nil {
				slog.Error("Failed to get songs with lyrics like", slog.Any("err", err))
				return err
			}
			for _, song := range songs {
				songChoices = append(songChoices, utils.SongChoice{
					ID: song.ID, Name: song.Name, Artists: song.Artists,
				})
			}
		}

		return e.AutocompleteResult(utils.BuildSongChoices(songChoices))
	}
}

func LyricsHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
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

		lyrics := song.Lyrics.String

		if len(lyrics) > 2048 {
			lyrics = lyrics[:2048]
		}

		eb := discord.NewEmbedBuilder().
			SetTitle(fmt.Sprintf("%s - %s", song.Artists, song.Name)).
			SetDescription(lyrics).
			SetColor(utils.ColorSuccess).
			SetThumbnail(song.ThumbnailUrl.String)

		lyricsMessage := discord.NewMessageCreateBuilder().
			SetEmbeds(eb.Build())

		// GetSongButtonRows returns nil when the song carries no links, so the
		// hand-written "does it have any" guard this replaced is redundant.
		lyricsMessage = lyricsMessage.AddContainerComponents(utils.GetSongButtonRows(song)...)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage,
			lyricsMessage.Build(),
		)
	}
}
