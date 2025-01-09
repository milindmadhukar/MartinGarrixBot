package commands

import (
	"math/rand/v2"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var eightball = discord.SlashCommandCreate{
	Name:        "8ball",
	Description: "8 ball command to make decisions",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:        "question",
			Description: "The question to ask the 8 ball",
			Required:    true,
		},
	},
}

var responses = []string{
	"As I see it, yes.",
	"Ask again later.",
	"Better not tell you now.",
	"Cannot predict now.",
	"Concentrate and ask again.",
	"Don’t count on it.",
	"It is certain.",
	"It is decidedly so.",
	"Most likely.",
	"My reply is no.",
	"My sources say no.",
	"Outlook not so good.",
	"Outlook good.",
	"Reply hazy, try again.",
	"Signs point to yes.",
	"Very doubtful.",
	"Without a doubt.",
	"Yes.",
	"Yes – definitely.",
	"You may rely on it.",
}

func EightBallHandler(e *handler.CommandEvent) error {
	question := e.SlashCommandInteractionData().String("question")

	// TODO: Update all embed colours
	eb := discord.NewEmbedBuilder().
		SetTitle("The Magic 8 Ball \U0001F3B1 replies").
		SetColor(utils.ColorSuccess).
		AddField("Question", question, false).
		AddField("Answer", responses[rand.IntN(len(responses))], false)

	return e.Respond(
		discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
			SetEmbeds(eb.Build()).
			Build(),
	)
}