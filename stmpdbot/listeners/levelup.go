package listeners

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// crossedLevel reports the level a member has just reached and whether this
// award actually crossed a boundary.
//
// Both totals come from MessageSent's RETURNING, so the decision is made from
// what the database holds rather than from a value read before the update -- the
// same reason the award itself moved into the UPDATE.
//
// The test is `after > before`, not `after == before+1`: a single award tops out
// at 25 XP today and the cheapest level costs 100, but a 5x xp_multiplier makes
// a two-level jump reachable, and that must still announce exactly once.
func crossedLevel(previousXP, newXP int32) (int, bool) {
	before := utils.GetUserLevel(previousXP)
	after := utils.GetUserLevel(newXP)
	return after, after > before
}

// announceLevelUp posts the level-up message and grants the level-up role.
//
// Every failure in here is logged and swallowed. This runs on the tail of the
// message listener, and a missing channel, a deleted role or a permissions error
// must never cost the member the XP they just earned.
func announceLevelUp(b *stmpdbot.STMPDBot, e *events.MessageCreate, guild db.Guild, level int) {
	userID := e.Message.Author.ID

	// An unset bot_channel is the off switch, matching how every other optional
	// channel in guilds works. Five of the six configured guilds have none.
	if guild.BotChannel.Valid {
		embed := discord.NewEmbed().
			WithDescription(fmt.Sprintf("GG <@%d>, you just reached **level %d**!", userID, level)).
			WithColor(utils.ColorSuccess)

		if _, err := b.Client.Rest.CreateMessage(
			snowflake.ID(guild.BotChannel.Int64),
			discord.NewMessageCreate().WithEmbeds(embed),
		); err != nil {
			slog.Error("Failed to post level up message",
				slog.Any("err", err), slog.Any("user", userID), slog.Int("level", level))
		}
	}

	grantLevelUpRole(b, e, guild, level)
}

// grantLevelUpRole hands out the configured role once a member is at or past the
// configured level.
//
// `>=` rather than `==` so a member who jumps two levels, or whose threshold is
// lowered later, still gets it.
func grantLevelUpRole(b *stmpdbot.STMPDBot, e *events.MessageCreate, guild db.Guild, level int) {
	if !guild.LevelUpRole.Valid || level < int(guild.LevelUpRoleLevel) {
		return
	}

	roleID := snowflake.ID(guild.LevelUpRole.Int64)

	// A guild MessageCreate carries the member, so the "do they already have it"
	// check costs no lookup at all -- cache or REST.
	if e.Message.Member != nil && slices.Contains(e.Message.Member.RoleIDs, roleID) {
		return
	}

	if err := b.Client.Rest.AddMemberRole(*e.GuildID, e.Message.Author.ID, roleID); err != nil {
		slog.Error("Failed to grant level up role",
			slog.Any("err", err), slog.Any("user", e.Message.Author.ID), slog.Any("role", roleID))
	}
}

// levelUpConfig fetches the guild row behind the announcement and the role.
//
// This is read only when a level was actually crossed, not on every message, so
// it does not need the caching the XP multiplier would have needed.
func levelUpConfig(ctx context.Context, b *stmpdbot.STMPDBot, guildID snowflake.ID) (db.Guild, bool) {
	guild, err := b.Queries.GetGuild(ctx, int64(guildID))
	if err != nil {
		slog.Error("Failed to read guild config for level up",
			slog.Any("err", err), slog.Any("guild", guildID))
		return db.Guild{}, false
	}
	return guild, true
}
