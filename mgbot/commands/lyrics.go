package commands

import (
	"context"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var lyrics = discord.SlashCommandCreate{
	Name:        "lyrics",
	Description: "Get the lyrics of a song.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:         "song",
			Description:  "The user to get the avatar of.",
			Required:     true,
			Autocomplete: true,
		},
	},
}

func LyricsAutocompleteHandler(b *mgbot.MartinGarrixBot) handler.AutocompleteHandler {
	return func(e *handler.AutocompleteEvent) error {
		type Song struct {
			Name  string
			Alias string
		}

		var songChoices []Song
		autocompleteInput := e.Data.String("song")
		if autocompleteInput == "" {
			songs, err := b.Queries.GetAllSongNamesWithLyrics(context.Background())
			if err != nil {
				return err
			}

			for _, song := range songs {
				songChoices = append(songChoices, Song{
					Name:  song.Name,
					Alias: song.Alias.String,
				})
			}

		} else {
			songs, err := b.Queries.GetSongsWithLyricsLike(context.Background(), "%"+e.Data.String("song")+"%")
			if err != nil {
				return err
			}

			for _, song := range songs {
				songChoices = append(songChoices, Song{
					Name:  song.Name,
					Alias: song.Alias.String,
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

		if len(choices) > 20 {
			choices = choices[:20]
		}

		return e.AutocompleteResult(choices)
	}
}

func LyricsHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		songName := e.SlashCommandInteractionData().String("song")
		song, err := b.Queries.GetSongLyrics(context.Background(), songName)
		if err != nil {
			return err
		}

		eb := discord.NewEmbedBuilder().
			SetTitle(fmt.Sprintf("%s - %s", song.Alias.String, songName)).
			SetDescription(song.Lyrics.String).
			SetColor(utils.ColorSuccess).
			SetThumbnail(song.ThumbnailUrl.String)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
				SetEmbeds(eb.Build()).
				Build(),
		)
	}
}