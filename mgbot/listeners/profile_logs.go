package listeners

import (
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// GuildMemberProfileListener reports avatar, name, nickname and role changes to
// the configured member-log channel. Like the voice logs, nothing is persisted.
//
// disgo hands us OldMember because the member cache is enabled, so every change
// is a straight diff -- no state of our own to keep. Discord sends
// GUILD_MEMBER_UPDATE for changes the bot does not care about (premium boost
// timestamps, flags), so posting unconditionally would fill the channel with
// empty embeds. Only a diff that produced at least one field is posted.
func GuildMemberProfileListener(b *mgbot.MartinGarrixBot) bot.EventListener {
	return bot.NewListenerFunc(func(e *events.GuildMemberUpdate) {
		if e.Member.User.Bot {
			return
		}

		fields := diffMember(e.OldMember, e.Member)
		if len(fields) == 0 {
			return
		}

		channelID, ok := logChannel(b.Queries.GetMemberLogsChannel, e.GuildID, "member logs")
		if !ok {
			return
		}

		embed := discord.NewEmbed().
			WithTitle("Member Updated").
			WithDescription(fmt.Sprintf("%s (`%d`)", e.Member.User.Mention(), e.Member.User.ID)).
			WithTimestamp(time.Now()).
			WithColor(utils.ColorInfo)

		for _, f := range fields {
			embed = embed.AddField(f.name, f.value, false)
		}
		if url := e.Member.User.EffectiveAvatarURL(); url != "" {
			embed = embed.WithThumbnail(url)
		}

		if _, err := b.Client.Rest.CreateMessage(channelID,
			discord.NewMessageCreate().WithEmbeds(embed),
		); err != nil {
			slog.Error("Failed to post member log", slog.Any("err", err))
		}
	})
}

type memberChange struct {
	name  string
	value string
}

// diffMember reports only what actually changed between two member snapshots.
func diffMember(old, updated discord.Member) []memberChange {
	var changes []memberChange

	if from, to := derefOr(old.Nick, "none"), derefOr(updated.Nick, "none"); from != to {
		changes = append(changes, memberChange{"Nickname", from + " → " + to})
	}

	if old.User.Username != updated.User.Username {
		changes = append(changes, memberChange{
			"Username", old.User.Username + " → " + updated.User.Username,
		})
	}

	if from, to := derefOr(old.User.GlobalName, "none"), derefOr(updated.User.GlobalName, "none"); from != to {
		changes = append(changes, memberChange{"Display name", from + " → " + to})
	}

	// Avatar hashes are opaque, so the useful thing to report is that it changed
	// plus a link to the new one -- the embed thumbnail already shows it.
	if derefOr(old.User.Avatar, "") != derefOr(updated.User.Avatar, "") {
		changes = append(changes, memberChange{
			"Avatar", "[new avatar](" + updated.User.EffectiveAvatarURL() + ")",
		})
	}
	if derefOr(old.Avatar, "") != derefOr(updated.Avatar, "") {
		changes = append(changes, memberChange{"Server avatar", "changed"})
	}

	if added, removed := diffRoles(old.RoleIDs, updated.RoleIDs); added != "" || removed != "" {
		var value string
		if added != "" {
			value += "added " + added
		}
		if removed != "" {
			if value != "" {
				value += "\n"
			}
			value += "removed " + removed
		}
		changes = append(changes, memberChange{"Roles", value})
	}

	return changes
}

func diffRoles(old, updated []snowflake.ID) (added, removed string) {
	return mentionRoles(missingFrom(updated, old)), mentionRoles(missingFrom(old, updated))
}

// missingFrom returns the entries of a that are absent from b.
func missingFrom(a, b []snowflake.ID) []snowflake.ID {
	var out []snowflake.ID
	for _, id := range a {
		if !slices.Contains(b, id) {
			out = append(out, id)
		}
	}
	return out
}

func mentionRoles(ids []snowflake.ID) string {
	var out string
	for i, id := range ids {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("<@&%d>", id)
	}
	return out
}

func derefOr(s *string, fallback string) string {
	if s == nil || *s == "" {
		return fallback
	}
	return *s
}
