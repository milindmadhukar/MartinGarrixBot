package commands

import (
	"errors"
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
	Description: "Deposit coins from hand to safe.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionInt{
			Name:        "amount",
			Description: "Amount of coins to deposit.",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "all",
			Description: "Deposit all coins in hand to safe.",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "half",
			Description: "Deposit half coins in hand to safe.",
			Required:    false,
		},
	},
}

func DepositHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		amt, amtOk := e.SlashCommandInteractionData().OptInt("amount")
		isAll := e.SlashCommandInteractionData().Bool("all")
		isHalf := e.SlashCommandInteractionData().Bool("half")

		balanceInfo, err := b.Queries.GetBalance(e.Ctx, db.GetBalanceParams{
			ID:      int64(e.Member().User.ID),
			GuildID: int64(*e.GuildID()),
		})
		if err != nil {
			return err
		}

		amtToDeposit, err := resolveAmount(balanceInfo.InHand.Int64, amt, amtOk, isAll, isHalf)
		if err != nil {
			message := "Please provide amount of coins to deposit."
			switch {
			case errors.Is(err, ErrAmountNotPositive):
				message = "Amount of coins to deposit should be positive."
			case errors.Is(err, ErrInsufficientBalance):
				message = "You don't have enough coins in hand to deposit."
			}

			return e.Respond(
				discord.InteractionResponseTypeCreateMessage,
				discord.NewMessageCreateBuilder().
					SetEmbeds(utils.FailureEmbed(message, "")).
					SetEphemeral(true).
					Build(),
			)
		}

		err = b.Queries.DepositAmount(e.Ctx, db.DepositAmountParams{
			ID:      int64(e.Member().User.ID),
			GuildID: int64(*e.GuildID()),
			InHand: pgtype.Int8{
				Int64: amtToDeposit,
				Valid: true,
			},
		})
		if err != nil {
			return err
		}

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreateBuilder().
				SetEmbeds(utils.SuccessEmbed(
					fmt.Sprintf("Successfully deposited %d coins from hand to safe.", amtToDeposit),
					"")).
				Build(),
		)
	}
}
