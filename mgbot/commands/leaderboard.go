package commands

import (
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var leaderboard = discord.SlashCommandCreate{
	Name:        "leaderboard",
	Description: "Get the leaderboard for a specific category.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:        "category",
			Description: "The category of the leaderboard.",
			Required:    true,
			Choices: []discord.ApplicationCommandOptionChoiceString{
				{
					Name:  "Coins",
					Value: "Coins",
				},
				{
					Name:  "Levels",
					Value: "Levels",
				},
				{
					Name:  "Messages",
					Value: "Messages",
				},
				{
					Name:  "In Hand Coins",
					Value: "In Hand Coins",
				},
			},
		},
	},
}

type leaderboardDetails struct {
	member discord.Member
	value  string
}

func LeaderboardHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		category := e.SlashCommandInteractionData().String("category")

		var leaderboard []leaderboardDetails

		switch category {
		// TODO: Use offset and pagination to show pages for leaderboards
		case "Coins":
			records, err := b.Queries.GetCoinsLeaderboard(e.Ctx, db.GetCoinsLeaderboardParams{
				GuildID: int64(*e.GuildID()),
				Offset:  0,
			})
			if err != nil {
				return err
			}

			for _, record := range records {
				var discordMember discord.Member
				var ok bool
				discordMember, ok = b.Client.Caches().Member(*e.GuildID(), snowflake.ID(record.ID))
				if !ok {
					discordMemberPtr, err := b.Client.Rest().GetMember(*e.GuildID(), snowflake.ID(record.ID))
					if err != nil {
						return err
					}
					discordMember = *discordMemberPtr
				}

				leaderboard = append(leaderboard, leaderboardDetails{
					member: discordMember,
					value:  strconv.Itoa(int(record.GarrixCoins.Int64 + record.InHand.Int64)),
				})
			}

		case "Levels":
			records, err := b.Queries.GetLevelsLeaderboard(e.Ctx, db.GetLevelsLeaderboardParams{
				GuildID: int64(*e.GuildID()),
				Offset:  0,
			})
			if err != nil {
				return err
			}

			for _, record := range records {
				var discordMember discord.Member
				var ok bool
				discordMember, ok = b.Client.Caches().Member(*e.GuildID(), snowflake.ID(record.ID))
				if !ok {
					discordMemberPtr, err := b.Client.Rest().GetMember(*e.GuildID(), snowflake.ID(record.ID))
					if err != nil {
						return err
					}
					discordMember = *discordMemberPtr
				}

				leaderboard = append(leaderboard, leaderboardDetails{
					member: discordMember,
					value:  utils.Humanize(record.TotalXp.Int32),
				})
			}

		case "Messages":
			records, err := b.Queries.GetMessagesSentLeaderboard(e.Ctx, db.GetMessagesSentLeaderboardParams{
				GuildID: int64(*e.GuildID()),
				Offset:  0,
			})
			if err != nil {
				return err
			}

			for _, record := range records {
				var discordMember discord.Member
				var ok bool
				discordMember, ok = b.Client.Caches().Member(*e.GuildID(), snowflake.ID(record.ID))
				if !ok {
					discordMemberPtr, err := b.Client.Rest().GetMember(*e.GuildID(), snowflake.ID(record.ID))
					if err != nil {
						return err
					}
					discordMember = *discordMemberPtr
				}

				leaderboard = append(leaderboard, leaderboardDetails{
					member: discordMember,
					value:  strconv.Itoa(int(record.MessagesSent.Int32)),
				})
			}

		case "In Hand Coins":
			records, err := b.Queries.GetInHandLeaderboard(e.Ctx, db.GetInHandLeaderboardParams{
				GuildID: int64(*e.GuildID()),
				Offset:  0,
			})
			if err != nil {
				return err
			}

			for _, record := range records {
				var discordMember discord.Member
				var ok bool
				discordMember, ok = b.Client.Caches().Member(*e.GuildID(), snowflake.ID(record.ID))
				if !ok {
					discordMemberPtr, err := b.Client.Rest().GetMember(*e.GuildID(), snowflake.ID(record.ID))
					if err != nil {
						return err
					}
					discordMember = *discordMemberPtr
				}

				leaderboard = append(leaderboard, leaderboardDetails{
					member: discordMember,
					value:  strconv.Itoa(int(record.InHand.Int64)),
				})
			}

		}

		var description []string

		// TODO: Fix indentations? Make indentation same for all
		for idx, leaderboardDetail := range leaderboard {
			description = append(description, strconv.Itoa(idx+1)+". "+leaderboardDetail.member.Mention()+" - "+leaderboardDetail.value)
		}

		embed := discord.NewEmbedBuilder().
			SetTitle(category + " Leaderboard").
			SetDescription(strings.Join(description, "\n")).
			SetColor(utils.ColorSuccess).
			Build()

		return e.Respond(
			discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
				SetEmbeds(embed).
				Build(),
		)
	}
}