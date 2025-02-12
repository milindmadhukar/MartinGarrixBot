package commands

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
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
		var songChoices []utils.UniqueSong
		autocompleteInput := e.Data.String("song")
		if autocompleteInput == "" {
			// BUG: Says invalid form body on empty input?
			songs, err := b.Queries.GetRandomSongNames(e.Ctx)
			if err != nil {
				slog.Error("Failed to get all song names with lyrics", slog.Any("err", err))
				return err
			}

			for _, song := range songs {
				songChoices = append(songChoices, utils.UniqueSong{
					Name:        song.Name,
					Artists:     song.Artists,
					ReleaseYear: song.ReleaseYear,
				})
			}

		} else {
			// BUG: Sometimes goes invalid form body? Even with input, I suspect its the json, also fix the lyrics autocomplete then.
			songs, err := b.Queries.GetSongsLike(e.Ctx, "%"+e.Data.String("song")+"%")
			if err != nil {
				slog.Error("Failed to get songs with lyrics like", slog.Any("err", err))
				return err
			}

			for _, song := range songs {
				songChoices = append(songChoices, utils.UniqueSong{
					Name:        song.Name,
					Artists:     song.Artists,
					ReleaseYear: song.ReleaseYear,
				})
			}
		}

		choices := make([]discord.AutocompleteChoice, len(songChoices))
		for i, song := range songChoices {
			choiceJson, _ := json.Marshal(song)
			choices[i] = discord.AutocompleteChoiceString{
				Name:  fmt.Sprintf("%s - %s", song.Artists, song.Name),
				Value: string(choiceJson),
			}
		}

		return e.AutocompleteResult(choices)
	}
}

func LinksHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		songDataJson := e.SlashCommandInteractionData().String("song")
		var songData utils.UniqueSong
		json.Unmarshal([]byte(songDataJson), &songData)
		song, err := b.Queries.GetSong(e.Ctx, db.GetSongParams{
			Name:        songData.Name,
			Artists:     songData.Artists,
			ReleaseYear: songData.ReleaseYear,
		})
		if err != nil {
			return err
		}

		embed := discord.NewEmbedBuilder().
			SetTitle(fmt.Sprintf("%s - %s", song.Artists, song.Name)).
			SetColor(utils.ColorSuccess).
			SetImage(song.ThumbnailUrl.String).
			Build()

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
				SetEmbeds(embed).
				AddActionRow(
					utils.GetSongButtons(song)...,
				).
				Build(),
		)
	}
}