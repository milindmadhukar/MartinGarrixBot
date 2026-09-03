package commands

import (
	"errors"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
)

var give = discord.SlashCommandCreate{
	Name:        "give",
	Description: "Give STMPD coins to a member.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionUser{
			Name:        "user",
			Description: "The user you want to give coins to.",
			Required:    true,
		},
		discord.ApplicationCommandOptionInt{
			Name:        "amount",
			Description: "The amount of coins you want to give.",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "all",
			Description: "Give all coins in hand to the user.",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "half",
			Description: "Give half of the coins in hand to the user.",
			Required:    false,
		},
	},
}

func GiveHandler(b *stmpdbot.STMPDBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		member := e.SlashCommandInteractionData().Member("user")
		// TODO: Check if it can't resolve a member

		amt, amtOk := e.SlashCommandInteractionData().OptInt("amount")
		isAll := e.SlashCommandInteractionData().Bool("all")
		isHalf := e.SlashCommandInteractionData().Bool("half")

		guildID := int64(*e.GuildID())

		balanceInfo, err := b.Queries.GetBalance(e.Ctx, db.GetBalanceParams{
			ID:      int64(e.Member().User.ID),
			GuildID: guildID,
		})

		if err != nil {
			return err
		}

		amtToGive, err := resolveAmount(balanceInfo.InHand, amt, amtOk, isAll, isHalf)
		if err != nil {
			message := "Please provide amount of coins to give."
			switch {
			case errors.Is(err, ErrAmountNotPositive):
				message = "Amount of coins to give should be positive."
			case errors.Is(err, ErrInsufficientBalance):
				message = "You don't have enough coins in hand to give."
			}

			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreate().
					WithEmbeds(utils.FailureEmbed(message, "")).
					WithEphemeral(true),
			)
		}

		// GuildID is required: users are keyed on (id, guild_id), so leaving it
		// zero matched no row and the transfer silently moved nothing.
		err = b.Queries.GiveCoins(e.Ctx, db.GiveCoinsParams{
			ID:      int64(e.Member().User.ID),
			ID_2:    int64(member.User.ID),
			GuildID: guildID,
			InHand:  amtToGive,
		})

		if err != nil {
			return err
		}

		embed := utils.SuccessEmbed(
			fmt.Sprintf("Successfully gave %d coins to %s", amtToGive, member.User.EffectiveName()),
			"",
		)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreate().
				WithEmbeds(embed),
		)
	}
}
