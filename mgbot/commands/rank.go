package commands

import (
	"errors"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var rank = discord.SlashCommandCreate{
	Name:        "rank",
	Description: "Get the rank of a member.",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionUser{
			Name:        "user",
			Description: "The user to get the rank of.",
			Required:    false,
		},
	},
}

func RankHandler(b *mgbot.MartinGarrixBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		member := e.SlashCommandInteractionData().Member("user")
		// TODO: Check if it can't resolve a member
		if member.Member.User.ID == 0 {
			member = *e.Member()
		}

		if member.User.Bot {
			embed := utils.FailureEmbed("You cannot check the rank of a bot", "")
			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreateBuilder().
					SetEmbeds(embed).
					SetEphemeral(true).
					Build(),
			)
		}

		e.DeferCreateMessage(false)

		avatarURL := member.User.AvatarURL(discord.WithFormat(discord.FileFormatPNG), discord.WithSize(256))

		if avatarURL == nil {
			return errors.New("Failed to get avatar url")
		}

		user, err := b.Queries.GetUserLevelData(e.Ctx, db.GetUserLevelDataParams{
			ID:      int64(member.User.ID),
			GuildID: int64(*e.GuildID()),
		})
		if err != nil {
			return err
		}

		picture, err := utils.RankPicture(user, member.User.Username, *avatarURL)
		if err != nil {
			return err
		}

		pictureReader, err := utils.ImageToReader(picture)
		if err != nil {
			return err
		}

		_, err = e.UpdateInteractionResponse(
			discord.NewMessageUpdateBuilder().
				SetFiles(discord.NewFile("rank.png", "Rank", pictureReader)).
				Build(),
		)
		if err != nil {
			return err
		}

		return nil
	}
}
