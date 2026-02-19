package utils

import (
	"context"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/rest"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HasModeratorPermissions checks if a member has moderator permissions
// Returns true if the member is an administrator OR has the moderator role
func HasModeratorPermissions(ctx context.Context, dbPool *pgxpool.Pool, restClient rest.Rest, guildID snowflake.ID, resolvedMember *discord.ResolvedMember) bool {
	// ResolvedMember has permissions already calculated by Discord
	// If member is administrator, they can use moderation commands
	if resolvedMember.Permissions.Has(discord.PermissionAdministrator) {
		return true
	}

	// Check if the guild has a moderator role configured
	var moderatorRole pgtype.Int8
	err := dbPool.QueryRow(ctx, "SELECT moderator_role FROM guilds WHERE guild_id = $1", int64(guildID)).Scan(&moderatorRole)
	if err != nil || !moderatorRole.Valid {
		// If no moderator role is configured, fall back to checking for moderate members permission
		return resolvedMember.Permissions.Has(discord.PermissionModerateMembers) ||
			resolvedMember.Permissions.Has(discord.PermissionKickMembers) ||
			resolvedMember.Permissions.Has(discord.PermissionBanMembers)
	}

	// Check if member has the moderator role
	moderatorRoleID := snowflake.ID(moderatorRole.Int64)
	for _, roleID := range resolvedMember.RoleIDs {
		if roleID == moderatorRoleID {
			return true
		}
	}

	return false
}

// CalculateMemberPermissions calculates a member's permissions from their roles
func CalculateMemberPermissions(guild *discord.RestGuild, member *discord.Member) discord.Permissions {
	// If member is the guild owner, they have all permissions
	if member.User.ID == guild.OwnerID {
		return discord.PermissionsAll
	}

	// Start with @everyone role permissions
	var permissions discord.Permissions
	for _, role := range guild.Roles {
		if role.ID == guild.ID {
			permissions = role.Permissions
			break
		}
	}

	// Add permissions from member's roles
	for _, memberRoleID := range member.RoleIDs {
		for _, role := range guild.Roles {
			if role.ID == memberRoleID {
				permissions = permissions.Add(role.Permissions)

				// If member has administrator, they have all permissions
				if permissions.Has(discord.PermissionAdministrator) {
					return discord.PermissionsAll
				}
			}
		}
	}

	return permissions
}
