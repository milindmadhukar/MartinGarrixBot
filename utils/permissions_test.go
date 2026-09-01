package utils_test

import (
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// In Discord the @everyone role shares its ID with the guild, which is how
// CalculateMemberPermissions finds the baseline.
const (
	guildID    = snowflake.ID(690950056202731521)
	ownerID    = snowflake.ID(111)
	memberID   = snowflake.ID(222)
	modRoleID  = snowflake.ID(333)
	adminRole  = snowflake.ID(444)
	uselessRol = snowflake.ID(555)
)

func testGuild(everyonePerms discord.Permissions, roles ...discord.Role) *discord.RestGuild {
	all := append([]discord.Role{
		{ID: guildID, Permissions: everyonePerms}, // @everyone
	}, roles...)

	return &discord.RestGuild{
		Guild: discord.Guild{ID: guildID, OwnerID: ownerID},
		Roles: all,
	}
}

func testMember(id snowflake.ID, roleIDs ...snowflake.ID) *discord.Member {
	return &discord.Member{
		User:    discord.User{ID: id},
		RoleIDs: roleIDs,
	}
}

func TestCalculateMemberPermissions(t *testing.T) {
	t.Parallel()

	t.Run("the owner has every permission regardless of roles", func(t *testing.T) {
		t.Parallel()

		got := utils.CalculateMemberPermissions(
			testGuild(discord.PermissionsNone),
			testMember(ownerID),
		)
		if got != discord.PermissionsAll {
			t.Errorf("owner permissions = %d, want PermissionsAll", got)
		}
	})

	t.Run("a member with no roles inherits @everyone", func(t *testing.T) {
		t.Parallel()

		got := utils.CalculateMemberPermissions(
			testGuild(discord.PermissionSendMessages),
			testMember(memberID),
		)
		if !got.Has(discord.PermissionSendMessages) {
			t.Errorf("permissions = %d, want @everyone's SendMessages", got)
		}
		if got.Has(discord.PermissionKickMembers) {
			t.Error("did not expect KickMembers")
		}
	})

	t.Run("role permissions accumulate on top of @everyone", func(t *testing.T) {
		t.Parallel()

		got := utils.CalculateMemberPermissions(
			testGuild(discord.PermissionSendMessages,
				discord.Role{ID: modRoleID, Permissions: discord.PermissionKickMembers},
				discord.Role{ID: uselessRol, Permissions: discord.PermissionBanMembers},
			),
			testMember(memberID, modRoleID),
		)

		if !got.Has(discord.PermissionSendMessages) {
			t.Error("expected the @everyone baseline to be kept")
		}
		if !got.Has(discord.PermissionKickMembers) {
			t.Error("expected KickMembers from the member's role")
		}
		if got.Has(discord.PermissionBanMembers) {
			t.Error("did not expect permissions from a role the member does not have")
		}
	})

	t.Run("several roles are combined", func(t *testing.T) {
		t.Parallel()

		got := utils.CalculateMemberPermissions(
			testGuild(discord.PermissionsNone,
				discord.Role{ID: modRoleID, Permissions: discord.PermissionKickMembers},
				discord.Role{ID: uselessRol, Permissions: discord.PermissionBanMembers},
			),
			testMember(memberID, modRoleID, uselessRol),
		)

		if !got.Has(discord.PermissionKickMembers) || !got.Has(discord.PermissionBanMembers) {
			t.Errorf("permissions = %d, want both Kick and Ban", got)
		}
	})

	t.Run("administrator short circuits to every permission", func(t *testing.T) {
		t.Parallel()

		got := utils.CalculateMemberPermissions(
			testGuild(discord.PermissionsNone,
				discord.Role{ID: adminRole, Permissions: discord.PermissionAdministrator},
			),
			testMember(memberID, adminRole),
		)
		if got != discord.PermissionsAll {
			t.Errorf("permissions = %d, want PermissionsAll for an administrator", got)
		}
	})

	t.Run("a role the guild does not have is ignored", func(t *testing.T) {
		t.Parallel()

		got := utils.CalculateMemberPermissions(
			testGuild(discord.PermissionSendMessages),
			testMember(memberID, snowflake.ID(999999)),
		)
		if got != discord.PermissionSendMessages {
			t.Errorf("permissions = %d, want just the @everyone baseline", got)
		}
	})

	t.Run("a guild without an @everyone role starts from nothing", func(t *testing.T) {
		t.Parallel()

		guild := &discord.RestGuild{
			Guild: discord.Guild{ID: guildID, OwnerID: ownerID},
			Roles: []discord.Role{
				{ID: modRoleID, Permissions: discord.PermissionKickMembers},
			},
		}

		got := utils.CalculateMemberPermissions(guild, testMember(memberID, modRoleID))
		if !got.Has(discord.PermissionKickMembers) {
			t.Errorf("permissions = %d, want KickMembers", got)
		}
	})
}
