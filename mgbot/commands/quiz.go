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

// TODO: Make failure embed and use that
var timerExpiredEmbed = discord.NewEmbedBuilder().
	SetTitle("<a:cross:810462920810561556> Oops, you ran out of time").
	SetColor(utils.ColorError).Build()

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
			song, err = b.Queries.GetRandomSongWithLyricsEasy(context.Background())
		} else {
			song, err = b.Queries.GetRandomSongWithLyrics(context.Background())
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

		lyricsGuessEmbed := discord.NewEmbedBuilder().
			SetTitle(fmt.Sprintf("Guess the song title from the lyrics! (%s)", difficulty)).
			SetDescription("Guess the song name within 45 seconds.").
			SetColor(utils.ColorSuccess).
			AddField("Lyrics", lyricsToGuessFrom, false)

		err = e.Respond(
			discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(lyricsGuessEmbed.Build()).
				Build(),
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
				isClose := utils.IsCloseMatch(song.Name, response, 0.6)
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
						ID: int64(e.Member().User.ID),
						InHand: pgtype.Int8{
							Int64: int64(earnings),
							Valid: true,
						},
					})

					if err != nil {
						slog.Error("Could not add earnings to user for quiz", slog.Any("err", err))
						return
					}

					followUpResponseEmbed = discord.NewEmbedBuilder().
						SetTitle(fmt.Sprintf("<a:tick:810462879374770186> Your guess is correct and you earned %d coins.", earnings)).
						SetColor(utils.ColorSuccess).
						AddField("Song Name", fmt.Sprintf("%s - %s", song.Alias.String, song.Name), false).
						SetThumbnail(song.ThumbnailUrl.String).
						Build()

				} else {
					followUpResponseEmbed = discord.NewEmbedBuilder().
						SetTitle("<a:cross:810462920810561556> Your guess is incorrect").
						SetColor(utils.ColorError).
						AddField("Song Name", fmt.Sprintf("%s - %s", song.Alias.String, song.Name), false).
						SetThumbnail(song.ThumbnailUrl.String).
						Build()
				}

				_, err := b.Client.Rest().CreateFollowupMessage(e.ApplicationID(), e.Token(),
					discord.NewMessageCreateBuilder().
						SetEmbeds(followUpResponseEmbed).
						Build(),
				)
				if err != nil {
					slog.Error("Error while sending response to quiz answered by user", slog.Any("err", err))
				}
			}

			ctx, cancel := context.WithTimeout(e.Ctx, 45*time.Second)
			defer cancel()
			bot.WaitForEvent(b.Client, ctx, filterAuthorMessagesFunc, answerCheckFunc, func() {
				_, err := b.Client.Rest().CreateFollowupMessage(e.ApplicationID(), e.Token(),
					discord.NewMessageCreateBuilder().
						SetEmbeds(timerExpiredEmbed).
						Build(),
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