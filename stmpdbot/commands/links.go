package commands

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
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
func LinksAutocompleteHandler(b *stmpdbot.STMPDBot) handler.AutocompleteHandler {
	return songAutocomplete("links", func(ctx context.Context, input string) ([]utils.SongChoice, error) {
		// Renditions are listed here, unlike /lyrics: a remix has its own streaming
		// links, and hiding it puts them out of reach.
		if input == "" {
			rows, err := b.Queries.GetRandomSongNames(ctx)
			return songChoicesFrom(rows, func(r db.GetRandomSongNamesRow) utils.SongChoice {
				return utils.SongChoice{ID: r.ID, Name: r.Name, Artists: r.Artists, Mix: r.MixName.String}
			}), err
		}

		rows, err := b.Queries.GetSongsLike(ctx, utils.SearchTerms(input))
		return songChoicesFrom(rows, func(r db.GetSongsLikeRow) utils.SongChoice {
			return utils.SongChoice{ID: r.ID, Name: r.Name, Artists: r.Artists, Mix: r.MixName.String}
		}), err
	})
}

func LinksHandler(b *stmpdbot.STMPDBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		songID, ok := utils.ParseSongChoice(e.SlashCommandInteractionData().String("song"))
		if !ok {
			// The user submitted free text instead of picking a suggestion.
			return e.Respond(discord.InteractionResponseTypeCreateMessage,
				discord.NewMessageCreate().
					WithEmbeds(discord.NewEmbed().
						WithDescription("Please pick a song from the suggestions.").
						WithColor(utils.ColorWarning)).
					WithEphemeral(true))
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
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreate().
					WithEmbeds(
						utils.FailureEmbed("No streaming links found for this song.", ""),
					),
			)
		}

		embed := discord.NewEmbed().
			WithTitle(utils.SongHeading(song.Artists, song.Name, song.MixName.String)).
			WithColor(utils.ColorSuccess).
			WithImage(song.ThumbnailUrl.String)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreate().
				WithEmbeds(embed).
				AddComponents(buttonRows...),
		)
	}
}
