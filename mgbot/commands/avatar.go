package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var avatar = discord.SlashCommandCreate{
	Name:        "avatar",
	Description: "Get the avatar of a member.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionUser{
			Name:        "user",
			Description: "The user to get the avatar of.",
			Required:    false,
		},
	},
}

func AvatarHandler(e *handler.CommandEvent) error {
	user, ok := e.SlashCommandInteractionData().OptUser("user")
	if !ok {
		user = e.Member().User
	}
	avatarURL := user.AvatarURL(discord.WithFormat(discord.FileFormatJPEG), discord.WithSize(1024))

	eb := discord.NewEmbedBuilder().
		SetTitle("Avatar").
		SetColor(utils.ColorSuccess).
		SetImage(*avatarURL)

	return e.Respond(
		discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
			SetEmbeds(eb.Build()).
			Build(),
	)
}