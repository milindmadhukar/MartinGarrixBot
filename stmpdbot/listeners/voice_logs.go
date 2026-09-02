package listeners

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// Voice activity is relayed to a channel and never stored. On a busy server it
// is far and away the highest-volume event the bot sees, and nothing on the
// dashboard reads it back, so a table would be pure cost.
//
// These are deliberately separate listeners from voice.go. That one is radio
// plumbing and returns early when RadioManager is nil or the radio is inactive
// -- folding logging into it would silently skip every guild that is not
// currently playing music.

// VoiceLogJoinListener reports a member joining a voice channel.
func VoiceLogJoinListener(b *stmpdbot.STMPDBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildVoiceJoin) {
		if e.Member.User.Bot {
			return
		}
		channelID := e.VoiceState.ChannelID
		if channelID == nil {
			return
		}
		postVoiceLog(b, e.VoiceState.GuildID, e.Member,
			"Joined Voice", fmt.Sprintf("<#%d>", *channelID), utils.ColorSuccess)
	})
}

// VoiceLogLeaveListener reports a member leaving a voice channel.
func VoiceLogLeaveListener(b *stmpdbot.STMPDBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildVoiceLeave) {
		if e.Member.User.Bot {
			return
		}
		// On leave the new state has no channel; the one they left is on the
		// state disgo hands us.
		where := "a voice channel"
		if e.VoiceState.ChannelID != nil {
			where = fmt.Sprintf("<#%d>", *e.VoiceState.ChannelID)
		}
		postVoiceLog(b, e.VoiceState.GuildID, e.Member,
			"Left Voice", where, utils.ColorError)
	})
}

// VoiceLogMoveListener reports a member moving between voice channels.
func VoiceLogMoveListener(b *stmpdbot.STMPDBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildVoiceMove) {
		if e.Member.User.Bot {
			return
		}
		from := "elsewhere"
		if e.OldVoiceState.ChannelID != nil {
			from = fmt.Sprintf("<#%d>", *e.OldVoiceState.ChannelID)
		}
		to := "elsewhere"
		if e.VoiceState.ChannelID != nil {
			to = fmt.Sprintf("<#%d>", *e.VoiceState.ChannelID)
		}
		postVoiceLog(b, e.VoiceState.GuildID, e.Member,
			"Moved Voice", from+" → "+to, utils.ColorInfo)
	})
}

func postVoiceLog(b *stmpdbot.STMPDBot, guildID snowflake.ID, member discord.Member, title, detail string, color int) {
	channelID, ok := logChannel(b.Queries.GetVoiceLogsChannel, guildID, "voice logs")
	if !ok {
		return
	}

	embed := discord.NewEmbed().
		WithTitle(title).
		WithDescription(fmt.Sprintf("%s %s", member.User.Mention(), detail)).
		AddField("Member", fmt.Sprintf("%s (`%d`)", member.User.Username, member.User.ID), false).
		WithTimestamp(time.Now()).
		WithColor(color)

	if url := member.User.EffectiveAvatarURL(); url != "" {
		embed = embed.WithThumbnail(url)
	}

	if _, err := b.Client.Rest.CreateMessage(channelID,
		discord.NewMessageCreate().WithEmbeds(embed),
	); err != nil {
		slog.Error("Failed to post voice log", slog.Any("err", err))
	}
}

// logChannel reads a nullable log-channel column, applying the convention every
// log channel in this bot follows: NULL or zero means the feature is off, and a
// guild with no config row yet is not an error.
func logChannel(
	query func(context.Context, int64) (pgtype.Int8, error),
	guildID snowflake.ID,
	name string,
) (snowflake.ID, bool) {
	value, err := query(context.Background(), int64(guildID))
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("Failed to read "+name+" channel", slog.Any("err", err))
		}
		return 0, false
	}
	if !value.Valid || value.Int64 == 0 {
		return 0, false
	}
	return snowflake.ID(value.Int64), true
}
