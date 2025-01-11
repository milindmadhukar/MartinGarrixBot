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

var deposit = discord.SlashCommandCreate{
	Name:        "deposit",
	Description: "deposit coins from safe to hold in hand.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionInt{
			Name:        "amount",
			Description: "Amount of coins to deposit.",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "all",
			Description: "Deposit all coins from safe to hold in hand.",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "half",
			Description: "Deposit half coins from safe to hold in hand.",
			Required:    false,
		},
	},
}

func DepositHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		amt, amtOk := e.SlashCommandInteractionData().OptInt("amount")

		if amtOk && amt <= 0 {
			embed := utils.FailureEmbed("Amount of coins to deposit should be positive.", "")
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
			embed := utils.FailureEmbed("Please provide amount of coins to deposit.", "")
			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
					SetEmbeds(embed).
					SetEphemeral(true).
					Build(),
			)
		}

		var embed discord.Embed
		var amtToDeposit int64

		balanceInfo, err := b.Queries.GetBalance(e.Ctx, int64(e.Member().User.ID))
		if err != nil {
			return nil
		}

		if isHalf {
			amtToDeposit = balanceInfo.InHand.Int64 / 2
		} else if isAll {
			amtToDeposit = balanceInfo.InHand.Int64
		} else if amtOk {
			if int64(amt) > balanceInfo.InHand.Int64 {
				embed = utils.FailureEmbed("You don't have enough coins in hand to deposit.", "")
				return e.Respond(
					discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
						SetEmbeds(embed).
						SetEphemeral(true).
						Build(),
				)
			}

			amtToDeposit = int64(amt)
		}

		err = b.Queries.DepositAmount(e.Ctx, db.DepositAmountParams{
			ID: int64(e.Member().User.ID),
			InHand: pgtype.Int8{
				Int64: amtToDeposit,
				Valid: true,
			},
		})

		if err != nil {
			return err
		}

		embed = utils.SuccessEmbed(
			fmt.Sprintf("Successfully deposited %d coins from safe to hold in hand.", amtToDeposit),
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