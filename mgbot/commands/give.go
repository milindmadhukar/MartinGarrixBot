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

var give = discord.SlashCommandCreate{
	Name:        "give",
	Description: "Give Garrix coins to a member.",
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

func GiveHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		member := e.SlashCommandInteractionData().Member("user")
		// TODO: Check if it can't resolve a member

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
			embed := utils.FailureEmbed("Please provide amount of coins to give.", "")
			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
					SetEmbeds(embed).
					SetEphemeral(true).
					Build(),
			)
		}

		var embed discord.Embed
		var amtToGive int64

		balanceInfo, err := b.Queries.GetBalance(e.Ctx, int64(e.Member().User.ID))
		if err != nil {
			return err
		}

		if isHalf {
			amtToGive = balanceInfo.InHand.Int64 / 2
		} else if isAll {
			amtToGive = balanceInfo.InHand.Int64
		} else if amtOk {
			if int64(amt) > balanceInfo.InHand.Int64 {
				embed = utils.FailureEmbed("You don't have enough coins in hand to give.", "")
				return e.Respond(
					discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
						SetEmbeds(embed).
						SetEphemeral(true).
						Build(),
				)
			}

			amtToGive = int64(amt)
		}

		err = b.Queries.GiveCoins(e.Ctx, db.GiveCoinsParams{
			ID:   int64(e.Member().User.ID),
			ID_2: int64(member.User.ID),
			InHand: pgtype.Int8{
				Int64: amtToGive,
				Valid: true,
			},
		})

		if err != nil {
			return err
		}

		embed = utils.SuccessEmbed(
			fmt.Sprintf("Successfully gave %d coins to %s", amtToGive, member.User.EffectiveName()),
			"",
		)

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
				SetEmbeds(embed).
				Build(),
		)
	}
}
