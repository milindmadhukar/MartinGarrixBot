package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
)

var whois = discord.SlashCommandCreate{
	Name:        "whois",
	Description: "Get the info about a member or bot in the server.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionUser{
			Name:        "user",
			Description: "The user to get the info of.",
			Required:    false,
		},
	},
}

func WhoisHandler(e *handler.CommandEvent) error {
	member := e.SlashCommandInteractionData().Member("user")
	// TODO: Check if it can't resolve a member
	if member.Member.User.ID == 0 {
		member = *e.Member()
	}

	eb := discord.NewEmbedBuilder().
		SetTitle("Whois").
		SetDescription(member.Mention()).
		SetColor(*member.User.AccentColor).
		AddField("Permissions", member.Permissions.String(), false).
		// TODO: Fetch user from DB Messages sent, status
		// FIXME: Complete this
		AddField("Nickname", *member.Nick, false)

	return e.Respond(
		discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
			SetEmbeds(eb.Build()).
			Build(),
	)
}