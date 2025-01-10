package commands

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var balance = discord.SlashCommandCreate{
	Name:        "balance",
	Description: "Check account balance for Coins",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionUser{
			Name:        "user",
			Description: "The user to get the balance of.",
			Required:    false,
		},
	},
}

func BalanceHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		member := e.SlashCommandInteractionData().Member("user")
		// TODO: Check if it can't resolve a member
		if member.Member.User.ID == 0 {
			member = *e.Member()
		}

		balanceInfo, err := b.Queries.GetBalance(e.Ctx, int64(member.Member.User.ID))

		if err != nil {
			return err
		}

		eb := discord.NewEmbedBuilder().
			SetTitle("Garrix Bank").
			AddField("In Hand", strconv.Itoa(int(balanceInfo.InHand.Int64)), false).
			AddField("In Safe", strconv.Itoa(int(balanceInfo.GarrixCoins.Int64)), false).
			SetColor(utils.ColorSuccess).
			SetThumbnail(*e.Member().User.AvatarURL(discord.WithFormat(discord.FileFormatJPEG), discord.WithSize(512)))

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
				SetEmbeds(eb.Build()).
				Build(),
		)
	}
}
