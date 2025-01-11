package commands

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var withdraw = discord.SlashCommandCreate{
	Name:        "withdraw",
	Description: "Withdraw coins from safe to hold in hand.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionInt{
			Name:        "amount",
			Description: "Amount of coins to withdraw.",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "all",
			Description: "Withdraw all coins from safe to hold in hand.",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "half",
			Description: "Withdraw half coins from safe to hold in hand.",
			Required:    false,
		},
	},
}

func WithdrawHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		amt, amtOk := e.SlashCommandInteractionData().OptInt("amount")

		if amtOk && amt <= 0 {
			embed := utils.FailureEmbed("Amount of coins to withdraw should be positive.", "")
			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
					SetEmbeds(embed).
					SetEphemeral(true).
					Build(),
			)
		}

		isAll := e.SlashCommandInteractionData().Bool("all")
		isHalf := e.SlashCommandInteractionData().Bool("half")

		if !amtOk && !isAll && !isHalf {
			embed := utils.FailureEmbed("Please provide amount of coins to withdraw.", "")
			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
					SetEmbeds(embed).
					SetEphemeral(true).
					Build(),
			)
		}

		var embed discord.Embed
		var amtToWithdraw int64

		balanceInfo, err := b.Queries.GetBalance(e.Ctx, int64(e.Member().User.ID))
		if err != nil {
			return err
		}

		if isHalf {
			amtToWithdraw = balanceInfo.GarrixCoins.Int64 / 2
		} else if isAll {
			amtToWithdraw = balanceInfo.GarrixCoins.Int64
		} else if amtOk {
			if int64(amt) > balanceInfo.GarrixCoins.Int64 {
				embed = utils.FailureEmbed("You don't have enough coins in safe to withdraw.", "")
				return e.Respond(
					discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
						SetEmbeds(embed).
						SetEphemeral(true).
						Build(),
				)
			}

			amtToWithdraw = int64(amt)
		}

		err = b.Queries.WithdrawAmount(e.Ctx, db.WithdrawAmountParams{
			ID: int64(e.Member().User.ID),
			InHand: pgtype.Int8{
				Int64: amtToWithdraw,
				Valid: true,
			},
		})

		if err != nil {
			return err
		}

		embed = utils.SuccessEmbed(
			fmt.Sprintf("Successfully withdrew %d coins from safe to hold in hand.", amtToWithdraw),
			"",
		)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageUpdateBuilder().
				SetEmbeds(embed).
				Build(),
		)
	}
}