package commands

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
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
func LyricsAutocompleteHandler(b *stmpdbot.STMPDBot) handler.AutocompleteHandler {
	return songAutocomplete("lyrics", func(ctx context.Context, input string) ([]utils.SongChoice, error) {
		// Canonical rows only, on both paths: a remix's words are the original's.
		if input == "" {
			rows, err := b.Queries.GetRandomSongNamesWithLyrics(ctx)
			return songChoicesFrom(rows, func(r db.GetRandomSongNamesWithLyricsRow) utils.SongChoice {
				return utils.SongChoice{ID: r.ID, Name: r.Name, Artists: r.Artists, Mix: r.MixName.String}
			}), err
		}

		rows, err := b.Queries.GetSongsWithLyricsLike(ctx, searchPattern(input))
		return songChoicesFrom(rows, func(r db.GetSongsWithLyricsLikeRow) utils.SongChoice {
			return utils.SongChoice{ID: r.ID, Name: r.Name, Artists: r.Artists, Mix: r.MixName.String}
		}), err
	})
}

func LyricsHandler(b *stmpdbot.STMPDBot) handler.CommandHandler {
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

		lyrics := truncateLyrics(song.Lyrics.String)

		eb := discord.NewEmbed().
			WithTitle(utils.SongHeading(song.Artists, song.Name, song.MixName.String)).
			WithDescription(lyrics).
			WithColor(utils.ColorSuccess).
			WithThumbnail(song.ThumbnailUrl.String)

		lyricsMessage := discord.NewMessageCreate().
			WithEmbeds(eb)

		// GetSongButtonRows returns nil when the song carries no links, so the
		// hand-written "does it have any" guard this replaced is redundant.
		lyricsMessage = lyricsMessage.AddComponents(utils.GetSongButtonRows(song)...)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage,
			lyricsMessage,
		)
	}
}

// embedDescriptionLimit is Discord's cap on an embed description, in characters.
// Exceeding it rejects the whole message rather than trimming it.
const embedDescriptionLimit = 2048

// truncateLyrics fits lyrics into an embed description.
//
// The cut is by rune, not by byte. Discord counts characters, so a byte slice both
// trims more than it needs to and can land in the middle of a multi-byte rune -- which
// renders as a replacement character at the end of every long non-ASCII song. That was
// survivable while lyrics were 79 rows entered by hand; it is not now that the LRCLIB
// backfill fills hundreds, a few of them past the limit.
func truncateLyrics(lyrics string) string {
	runes := []rune(lyrics)
	if len(runes) <= embedDescriptionLimit {
		return lyrics
	}
	// The ellipsis is worth a character of the budget: a song that simply stops
	// mid-line reads as missing data rather than as trimmed.
	return string(runes[:embedDescriptionLimit-1]) + "…"
}
