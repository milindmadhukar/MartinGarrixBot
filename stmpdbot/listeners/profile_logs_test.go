package listeners

import (
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

func ptr(s string) *string { return &s }

// TestDiffMemberIgnoresNoise is the one that matters operationally. Discord
// sends GUILD_MEMBER_UPDATE for things nobody wants logged -- boost timestamps,
// flag changes -- and posting on every one would fill the channel with empty
// embeds until somebody turned the feature off.
func TestDiffMemberIgnoresNoise(t *testing.T) {
	member := discord.Member{
		User: discord.User{ID: 1, Username: "milind"},
		Nick: ptr("mg"),
	}

	if changes := diffMember(member, member); len(changes) != 0 {
		t.Errorf("an identical member reported changes: %v", changes)
	}
}

func TestDiffMemberDetectsChanges(t *testing.T) {
	cases := []struct {
		name    string
		old     discord.Member
		updated discord.Member
		want    string
	}{
		{
			name:    "nickname",
			old:     discord.Member{User: discord.User{Username: "a"}, Nick: ptr("old")},
			updated: discord.Member{User: discord.User{Username: "a"}, Nick: ptr("new")},
			want:    "Nickname",
		},
		{
			name:    "nickname removed",
			old:     discord.Member{User: discord.User{Username: "a"}, Nick: ptr("old")},
			updated: discord.Member{User: discord.User{Username: "a"}},
			want:    "Nickname",
		},
		{
			name:    "username",
			old:     discord.Member{User: discord.User{Username: "before"}},
			updated: discord.Member{User: discord.User{Username: "after"}},
			want:    "Username",
		},
		{
			name:    "global name",
			old:     discord.Member{User: discord.User{Username: "a", GlobalName: ptr("Old")}},
			updated: discord.Member{User: discord.User{Username: "a", GlobalName: ptr("New")}},
			want:    "Display name",
		},
		{
			name:    "avatar",
			old:     discord.Member{User: discord.User{Username: "a", Avatar: ptr("hash1")}},
			updated: discord.Member{User: discord.User{Username: "a", Avatar: ptr("hash2")}},
			want:    "Avatar",
		},
		{
			name:    "roles",
			old:     discord.Member{User: discord.User{Username: "a"}, RoleIDs: []snowflake.ID{1}},
			updated: discord.Member{User: discord.User{Username: "a"}, RoleIDs: []snowflake.ID{1, 2}},
			want:    "Roles",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changes := diffMember(tc.old, tc.updated)
			if len(changes) == 0 {
				t.Fatalf("no change detected")
			}
			var found bool
			for _, c := range changes {
				if c.name == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("changes = %v, want one named %q", changes, tc.want)
			}
		})
	}
}

// Role reordering is not a change; only membership is.
func TestDiffRolesIgnoresOrder(t *testing.T) {
	old := discord.Member{User: discord.User{Username: "a"}, RoleIDs: []snowflake.ID{1, 2, 3}}
	updated := discord.Member{User: discord.User{Username: "a"}, RoleIDs: []snowflake.ID{3, 1, 2}}

	if changes := diffMember(old, updated); len(changes) != 0 {
		t.Errorf("reordered roles reported as a change: %v", changes)
	}
}

func TestDiffRolesReportsBothDirections(t *testing.T) {
	old := discord.Member{User: discord.User{Username: "a"}, RoleIDs: []snowflake.ID{1, 2}}
	updated := discord.Member{User: discord.User{Username: "a"}, RoleIDs: []snowflake.ID{2, 3}}

	changes := diffMember(old, updated)
	if len(changes) != 1 {
		t.Fatalf("changes = %v, want one Roles entry", changes)
	}
	if !strings.Contains(changes[0].value, "added") || !strings.Contains(changes[0].value, "removed") {
		t.Errorf("value = %q, want both additions and removals", changes[0].value)
	}
	if !strings.Contains(changes[0].value, "<@&3>") || !strings.Contains(changes[0].value, "<@&1>") {
		t.Errorf("value = %q, want role mentions for 3 (added) and 1 (removed)", changes[0].value)
	}
}
