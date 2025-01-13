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
		type Song struct {
			Name  string
			Alias string
		}

		var songChoices []Song
		autocompleteInput := e.Data.String("song")
		if autocompleteInput == "" {
			songs, err := b.Queries.GetAllSongNamesWithLyrics(e.Ctx)
			if err != nil {
				slog.Error("Failed to get all song names with lyrics", slog.Any("err", err))
				return err
			}

			for _, song := range songs {
				songChoices = append(songChoices, Song{
					Name:  song.Name,
					Alias: song.Artists,
				})
			}

		} else {
			songs, err := b.Queries.GetSongsWithLyricsLike(e.Ctx, "%"+e.Data.String("song")+"%")
			if err != nil {
				slog.Error("Failed to get songs with lyrics like", slog.Any("err", err))
				return err
			}

			for _, song := range songs {
				songChoices = append(songChoices, Song{
					Name:  song.Name,
					Alias: song.Artists,
				})
			}
		}

		choices := make([]discord.AutocompleteChoice, len(songChoices))
		for i, song := range songChoices {
			choices[i] = discord.AutocompleteChoiceString{
				Name:  fmt.Sprintf("%s - %s", song.Alias, song.Name),
				Value: song.Name,
			}
		}

		return e.AutocompleteResult(choices)
	}
}

func LyricsHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		songName := e.SlashCommandInteractionData().String("song")
		song, err := b.Queries.GetSongLyrics(e.Ctx, songName)
		if err != nil {
			return err
		}

		lyrics := song.Lyrics.String

		if len(lyrics) > 2048 {
			lyrics = lyrics[:2048]
		}

		eb := discord.NewEmbedBuilder().
			SetTitle(fmt.Sprintf("%s - %s", song.Artists, songName)).
			SetDescription(lyrics).
			SetColor(utils.ColorSuccess).
			SetThumbnail(song.ThumbnailUrl.String)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
				SetEmbeds(eb.Build()).
				AddActionRow(
					utils.GetSongButtons(song)...,
				).
				Build(),
		)
	}
}