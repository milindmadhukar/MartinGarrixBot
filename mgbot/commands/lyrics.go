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
		var songChoices []utils.UniqueSong
		autocompleteInput := e.Data.String("song")
		if autocompleteInput == "" {
			songs, err := b.Queries.GetRandomSongNamesWithLyrics(e.Ctx)
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
			songs, err := b.Queries.GetSongsWithLyricsLike(e.Ctx, "%"+e.Data.String("song")+"%")
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

func LyricsHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
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

		lyrics := song.Lyrics.String

		if len(lyrics) > 2048 {
			lyrics = lyrics[:2048]
		}

		eb := discord.NewEmbedBuilder().
			SetTitle(fmt.Sprintf("%s - %s", song.Artists, song.Name)).
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
