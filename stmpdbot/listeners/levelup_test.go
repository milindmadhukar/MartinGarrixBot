package listeners

import (
	"testing"

	"github.com/milindmadhukar/STMPDBot/utils"
)

// Boundaries come from utils.GetTotalXp rather than literals, so a change to the
// XP curve moves the test with it instead of silently invalidating it.
func TestCrossedLevel(t *testing.T) {
	t.Parallel()

	lvl5 := utils.GetTotalXp(5)

	tests := []struct {
		name       string
		previousXP int32
		newXP      int32
		wantLevel  int
		wantCross  bool
	}{
		{
			name:       "an award inside a level does not cross",
			previousXP: lvl5 + 10,
			newXP:      lvl5 + 30,
			wantLevel:  5,
		},
		{
			name:       "landing exactly on the boundary crosses",
			previousXP: lvl5 - 1,
			newXP:      lvl5,
			wantLevel:  5,
			wantCross:  true,
		},
		{
			name:       "one short of the boundary does not cross",
			previousXP: lvl5 - 20,
			newXP:      lvl5 - 1,
			wantLevel:  4,
		},
		{
			// Reachable with a 5x xp_multiplier. It must announce once, at the
			// level actually reached -- which is why the test is `after > before`
			// and not `after == before+1`.
			name:       "a multi-level jump crosses once, at the level reached",
			previousXP: lvl5 - 1,
			newXP:      utils.GetTotalXp(7),
			wantLevel:  7,
			wantCross:  true,
		},
		{
			name:       "a message on cooldown awards nothing and cannot cross",
			previousXP: lvl5,
			newXP:      lvl5,
			wantLevel:  5,
		},
		{
			name:       "a member's very first award does not cross out of level 0",
			previousXP: 0,
			newXP:      20,
			wantLevel:  0,
		},
		{
			name:       "the first level up crosses",
			previousXP: 90,
			newXP:      110,
			wantLevel:  1,
			wantCross:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			level, crossed := crossedLevel(tt.previousXP, tt.newXP)

			if crossed != tt.wantCross {
				t.Errorf("crossed = %v, want %v", crossed, tt.wantCross)
			}
			if level != tt.wantLevel {
				t.Errorf("level = %d, want %d", level, tt.wantLevel)
			}
		})
	}
}
