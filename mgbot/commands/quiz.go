package commands

// TODO: Yikes what spagetti code, cleanup

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/handler"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var quiz = discord.SlashCommandCreate{
	Name:        "quiz",
	Description: "Guess the name of the song from the lyrics!",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:        "difficulty",
			Description: "The difficulty of the quiz.",
			Required:    true,
			Choices: []discord.ApplicationCommandOptionChoiceString{
				{
					Name:  "Easy - 50 Coins",
					Value: "easy",
				},
				{
					Name:  "Medium - 100 Coins",
					Value: "medium",
				},
				{
					Name:  "Hard - 150 Coins",
					Value: "hard",
				},
				{
					Name:  "Extreme - 200 Coins",
					Value: "extreme",
				},
			},
		},
	},
}

// TODO: Implement a cooldown
// TODO: Maybe use components and like a dialog box for the quiz, idk?
func QuizHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		difficulty := e.SlashCommandInteractionData().String("difficulty")
		var song db.Song
		var err error
		if difficulty == "easy" {
			song, err = b.Queries.GetRandomSongWithLyricsEasy(e.Ctx)
		} else {
			song, err = b.Queries.GetRandomSongWithLyrics(e.Ctx)
		}
		if err != nil {
			return err
		}

		lines := strings.Split(song.Lyrics.String, "\n")
		validLines := filterValidLines(lines, song.Name)
		if len(validLines) == 0 {
			return fmt.Errorf("no valid lines found in lyrics")
		}

		selectedLines := selectLyricLines(validLines, difficulty)
		lyricsToGuessFrom := strings.Join(selectedLines, "\n")

		lyricsGuessEmbed := discord.NewEmbed().
			WithTitle(fmt.Sprintf("Guess the song title from the lyrics! (%s)", difficulty)).
			WithDescription("Guess the song name within 45 seconds.").
			WithColor(utils.ColorSuccess).
			AddField("Lyrics", lyricsToGuessFrom, false)

		err = e.Respond(
			discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(lyricsGuessEmbed),
		)
		if err != nil {
			return err
		}

		go func() {
			filterAuthorMessagesFunc := func(messageEvent *events.MessageCreate) bool {
				if messageEvent.Message.Author.ID == e.Member().User.ID {
					return true
				}
				return false
			}

			answerCheckFunc := func(messageEvent *events.MessageCreate) {
				response := messageEvent.Message.Content
				nameToCheck := song.Name
				isClose := utils.IsCloseMatch(nameToCheck, response, 0.6)
				var followUpResponseEmbed discord.Embed
				if isClose {
					// TODO: Maybe define it in constants?
					earningsForDifficulty := map[string]int{
						"easy":    50,
						"medium":  100,
						"hard":    150,
						"extreme": 200,
					}
					earnings := earningsForDifficulty[difficulty]

					err := b.Queries.AddCoins(e.Ctx, db.AddCoinsParams{
						ID:      int64(e.Member().User.ID),
						GuildID: int64(*e.GuildID()),
						InHand:  pgtype.Int8{Int64: int64(earnings), Valid: true},
					})

					if err != nil {
						slog.Error("Could not add earnings to user for quiz", slog.Any("err", err))
						return
					}

					followUpResponseEmbed = discord.NewEmbed().
						WithTitle(fmt.Sprintf("<a:tick:810462879374770186> Your guess is correct and you earned %d coins.", earnings)).
						WithColor(utils.ColorSuccess).
						AddField("Song Name", fmt.Sprintf("%s - %s", song.Artists, song.Name), false).
						WithThumbnail(song.ThumbnailUrl.String)

				} else {
					followUpResponseEmbed = discord.NewEmbed().
						WithTitle("<a:cross:810462920810561556> Your guess is incorrect").
						WithColor(utils.ColorError).
						AddField("Song Name", fmt.Sprintf("%s - %s", song.Artists, song.Name), false).
						WithThumbnail(song.ThumbnailUrl.String)

				}

				followUpMessage := discord.NewMessageCreate().
					WithEmbeds(followUpResponseEmbed)

				if song.SpotifyUrl.Valid || song.YoutubeUrl.Valid || song.AppleMusicUrl.Valid {
					followUpMessage = followUpMessage.AddActionRow(
						utils.GetSongButtons(song)...,
					)
				}

				_, err := b.Client.Rest.CreateFollowupMessage(e.ApplicationID(), e.Token(),
					followUpMessage,
				)
				if err != nil {
					slog.Error("Error while sending response to quiz answered by user", slog.Any("err", err))
				}
			}

			ctx, cancel := context.WithTimeout(e.Ctx, 45*time.Second)
			defer cancel()

			bot.WaitForEvent(b.Client, ctx, filterAuthorMessagesFunc, answerCheckFunc, func() {
				timerExpiredEmbed := discord.NewEmbed().
					WithTitle("<a:cross:810462920810561556> Oops, you ran out of time").
					WithColor(utils.ColorError).
					AddField("Song Name", fmt.Sprintf("%s - %s", song.Artists, song.Name), false).
					WithThumbnail(song.ThumbnailUrl.String)

				_, err := b.Client.Rest.CreateFollowupMessage(e.ApplicationID(), e.Token(),
					discord.NewMessageCreate().
						WithEmbeds(timerExpiredEmbed),
				)

				if err != nil {
					slog.Error("Error while sending timeout response for quiz", slog.Any("err", err))
				}
			})
		}()

		return nil
	}
}

func filterValidLines(lines []string, songName string) []string {
	var validLines []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) >= 5 && !strings.Contains(strings.ToLower(line),
			strings.ToLower(songName)) {
			validLines = append(validLines, line)
		}
	}
	return validLines
}

func selectLyricLines(lines []string, difficulty string) []string {
	if len(lines) == 0 {
		return []string{}
	}

	numLines := map[string]int{
		"easy":    4,
		"medium":  3,
		"hard":    2,
		"extreme": 1,
	}

	count := numLines[difficulty]
	if count == 0 {
		count = 4
	}

	if count > len(lines) {
		count = len(lines)
	}

	maxStart := len(lines) - count
	if maxStart < 0 {
		maxStart = 0
	}
	start := rand.IntN(maxStart + 1)

	return lines[start : start+count]
}
