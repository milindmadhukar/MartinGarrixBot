package mgbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// SendModlogEmbed posts a moderation action to the guild's modlog channel.
//
// It lives here rather than in commands/ because two callers need it: the
// /moderation commands, and the audit-log listener that records moderation
// performed through Discord's own UI. Both should look identical in the channel
// -- a ban is a ban whichever button produced it.
//
// Fire-and-forget by design: a modlog row is already written by the time this
// runs, and failing to post an embed must not fail the moderation action.
func SendModlogEmbed(b *MartinGarrixBot, guildID, userID, moderatorID snowflake.ID, logType, reason string, expiresAt *time.Time) {
	// Reads one column rather than SELECT * on guilds, which is what this did
	// on every single moderation action.
	channel, err := b.Queries.GetModlogsChannel(context.Background(), int64(guildID))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("Failed to read modlogs channel", slog.Any("err", err))
		}
		return
	}
	if !channel.Valid || channel.Int64 == 0 {
		return
	}

	reasonText := reason
	if reasonText == "" {
		reasonText = "No reason provided"
	}

	embed := discord.NewEmbed().
		WithTitle(fmt.Sprintf("Moderation Action: %s", strings.ToUpper(logType))).
		AddField("User", fmt.Sprintf("<@%d>", userID), true).
		AddField("Moderator", fmt.Sprintf("<@%d>", moderatorID), true).
		AddField("Reason", reasonText, false).
		WithTimestamp(time.Now()).
		WithColor(utils.ColorWarning)

	if expiresAt != nil {
		embed = embed.AddField("Expires", fmt.Sprintf("<t:%d:R>", expiresAt.Unix()), false)
	}

	if _, err = b.Client.Rest.CreateMessage(snowflake.ID(channel.Int64),
		discord.NewMessageCreate().WithEmbeds(embed),
	); err != nil {
		slog.Error("Failed to send modlog to channel", slog.Any("err", err))
	}
}
