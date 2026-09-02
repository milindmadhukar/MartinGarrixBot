package commands

import (
	"errors"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
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

// TODO: Maybe use the assets as embeded and all the migrations

func RankHandler(b *stmpdbot.STMPDBot) handler.CommandHandler {
	return func(e *handler.CommandEvent) error {
		member := e.SlashCommandInteractionData().Member("user")
		// TODO: Check if it can't resolve a member
		if member.User.ID == 0 {
			member = *e.Member()
		}

		if member.User.Bot {
			embed := utils.FailureEmbed("You cannot check the rank of a bot", "")
			return e.Respond(
				discord.InteractionResponseTypeCreateMessage, discord.NewMessageCreate().
					WithEmbeds(embed).
					WithEphemeral(true),
			)
		}

		e.DeferCreateMessage(false)

		avatarURL := member.User.AvatarURL(discord.WithFormat(discord.FileFormatPNG), discord.WithSize(256))

		if avatarURL == nil {
			return errors.New("failed to get avatar url")
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
			discord.NewMessageUpdate().
				WithFiles(discord.NewFile("rank.png", "Rank", pictureReader)),
		)
		if err != nil {
			return err
		}

		return nil
	}
}
