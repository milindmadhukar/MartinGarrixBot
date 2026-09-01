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
		isAll := e.SlashCommandInteractionData().Bool("all")
		isHalf := e.SlashCommandInteractionData().Bool("half")

		var amtToWithdraw int64

		balanceInfo, err := b.Queries.GetBalance(e.Ctx, db.GetBalanceParams{
			ID:      int64(e.Member().User.ID),
			GuildID: int64(*e.GuildID()),
		})
		if err != nil {
			return err
		}

		amtToWithdraw, err = resolveAmount(balanceInfo.GarrixCoins.Int64, amt, amtOk, isAll, isHalf)
		if err != nil {
			message := "Please provide amount of coins to withdraw."
			switch {
			case errors.Is(err, ErrAmountNotPositive):
				message = "Amount of coins to withdraw should be positive."
			case errors.Is(err, ErrInsufficientBalance):
				message = "You don't have enough coins in safe to withdraw."
			}

			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreate().
					WithEmbeds(utils.FailureEmbed(message, "")).
					WithEphemeral(true),
			)
		}

		err = b.Queries.WithdrawAmount(e.Ctx, db.WithdrawAmountParams{
			ID:      int64(e.Member().User.ID),
			GuildID: int64(*e.GuildID()),
			InHand:  pgtype.Int8{Int64: amtToWithdraw, Valid: true},
		})

		if err != nil {
			return err
		}

		embed := utils.SuccessEmbed(
			fmt.Sprintf("Successfully withdrew %d coins from safe to hold in hand.", amtToWithdraw),
			"",
		)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage,
			discord.NewMessageCreate().
				WithEmbeds(embed),
		)
	}
}
