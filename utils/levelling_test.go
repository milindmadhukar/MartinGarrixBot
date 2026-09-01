package utils_test

import (
	"math"
	"testing"

	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// maxSafeLevel bounds every property below. GetTotalXp accumulates into an
// int32, and the running sum passes MaxInt32 somewhere above level 1000
// (GetTotalXp(1000) is about 1.69e9). Past that the sum wraps negative and
// GetUserLevel's `totalSum <= totalXp` condition stays true, so it churns
// effectively forever. See TestGetUserLevel_DoesNotHangOnHugeXP.
const maxSafeLevel = 1000

func TestFXpForNextLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lvl  int
		want int32
	}{
		{"level 0 costs the base amount", 0, 100},
		{"level 1", 1, 155},
		{"level 2", 2, 220},
		{"level 10", 10, 1100},
		{"level 100", 100, 55100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.FXpForNextLevel(tt.lvl); got != tt.want {
				t.Errorf("FXpForNextLevel(%d) = %d, want %d", tt.lvl, got, tt.want)
			}
		})
	}
}

func TestGetTotalXp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		lvl  int
		want int32
	}{
		{"level 0 needs nothing", 0, 0},
		{"level 1 needs the level 0 cost", 1, 100},
		{"level 2 needs levels 0 and 1", 2, 255},
		{"level 3", 3, 475},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.GetTotalXp(tt.lvl); got != tt.want {
				t.Errorf("GetTotalXp(%d) = %d, want %d", tt.lvl, got, tt.want)
			}
		})
	}
}

// The definitional relationship between the two functions. If GetTotalXp is
// ever rewritten as a closed form, this catches an off-by-one immediately.
func TestGetTotalXp_IsCumulativeSumOfFXp(t *testing.T) {
	t.Parallel()

	for lvl := 0; lvl < maxSafeLevel; lvl++ {
		got := utils.GetTotalXp(lvl+1) - utils.GetTotalXp(lvl)
		want := utils.FXpForNextLevel(lvl)
		if got != want {
			t.Fatalf("GetTotalXp(%d) - GetTotalXp(%d) = %d, want FXpForNextLevel(%d) = %d",
				lvl+1, lvl, got, lvl, want)
		}
	}
}

func TestGetTotalXp_IsMonotonic(t *testing.T) {
	t.Parallel()

	prev := utils.GetTotalXp(0)
	for lvl := 1; lvl <= maxSafeLevel; lvl++ {
		cur := utils.GetTotalXp(lvl)
		if cur <= prev {
			t.Fatalf("GetTotalXp is not increasing: level %d = %d, level %d = %d",
				lvl-1, prev, lvl, cur)
		}
		prev = cur
	}
}

func TestGetUserLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		totalXp int32
		want    int
	}{
		{"zero xp is level 0", 0, 0},
		{"just below the level 1 threshold", 99, 0},
		{"exactly the level 1 threshold promotes", 100, 1},
		{"just above the level 1 threshold", 101, 1},
		{"just below the level 2 threshold", 254, 1},
		{"exactly the level 2 threshold promotes", 255, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.GetUserLevel(tt.totalXp); got != tt.want {
				t.Errorf("GetUserLevel(%d) = %d, want %d", tt.totalXp, got, tt.want)
			}
		})
	}
}

// The round trip that matters: the XP needed to reach a level must report that
// level back, and one XP short must report the level below.
func TestGetUserLevel_RoundTripsWithGetTotalXp(t *testing.T) {
	t.Parallel()

	for lvl := 0; lvl <= maxSafeLevel; lvl++ {
		total := utils.GetTotalXp(lvl)

		if got := utils.GetUserLevel(total); got != lvl {
			t.Fatalf("GetUserLevel(GetTotalXp(%d)) = GetUserLevel(%d) = %d, want %d",
				lvl, total, got, lvl)
		}

		if lvl > 0 {
			if got := utils.GetUserLevel(total - 1); got != lvl-1 {
				t.Fatalf("GetUserLevel(GetTotalXp(%d)-1) = GetUserLevel(%d) = %d, want %d",
					lvl, total-1, got, lvl-1)
			}
		}
	}
}

// BUG: negative XP yields level -1, which then drives a negative progress
// fraction in RankPicture and a progress bar with a negative width. Nothing
// writes negative XP today, so this is pinned rather than fixed.
func TestGetUserLevel_NegativeXpYieldsNegativeLevel(t *testing.T) {
	t.Parallel()

	if got := utils.GetUserLevel(-1); got != -1 {
		t.Errorf("GetUserLevel(-1) = %d, want -1 (current behaviour); if this is "+
			"now clamped to 0, update RankPicture's progress maths too", got)
	}
}

func TestGetUserLevelData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		totalXp     int32
		wantLvl     int
		wantForNext int32
		wantCurrent int32
	}{
		{"fresh user", 0, 0, 100, 0},
		{"part way through level 0", 50, 0, 100, 50},
		{"exactly level 1", 100, 1, 155, 0},
		{"part way through level 1", 200, 1, 155, 100},
		{"exactly level 2", 255, 2, 220, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := utils.GetUserLevelData(tt.totalXp)
			if got.Lvl != tt.wantLvl {
				t.Errorf("Lvl = %d, want %d", got.Lvl, tt.wantLvl)
			}
			if got.XpForNextLvl != tt.wantForNext {
				t.Errorf("XpForNextLvl = %d, want %d", got.XpForNextLvl, tt.wantForNext)
			}
			if got.CurrentXp != tt.wantCurrent {
				t.Errorf("CurrentXp = %d, want %d", got.CurrentXp, tt.wantCurrent)
			}
		})
	}
}

// CurrentXp must always be a valid position inside the current level, because
// RankPicture divides it by XpForNextLvl to size the progress bar. A fraction
// outside [0,1) would draw a bar wider than the card or of negative width.
func TestGetUserLevelData_ProgressIsAlwaysInRange(t *testing.T) {
	t.Parallel()

	// Sample across the range rather than every value; the step is deliberately
	// not a factor of any level threshold.
	for xp := int32(0); xp < 200_000_000; xp += 9973 {
		data := utils.GetUserLevelData(xp)

		if data.CurrentXp < 0 || data.CurrentXp >= data.XpForNextLvl {
			t.Fatalf("GetUserLevelData(%d): CurrentXp = %d, outside [0, %d)",
				xp, data.CurrentXp, data.XpForNextLvl)
		}

		if sum := utils.GetTotalXp(data.Lvl) + data.CurrentXp; sum != xp {
			t.Fatalf("GetUserLevelData(%d): GetTotalXp(%d) + %d = %d, want %d",
				xp, data.Lvl, data.CurrentXp, sum, xp)
		}

		fraction := float64(data.CurrentXp) / float64(data.XpForNextLvl)
		if math.IsNaN(fraction) || fraction < 0 || fraction >= 1 {
			t.Fatalf("GetUserLevelData(%d): progress fraction %.4f outside [0,1)", xp, fraction)
		}
	}
}

func TestGetUserLevel_DoesNotHangOnHugeXP(t *testing.T) {
	t.Skip("BUG: GetUserLevel accumulates the running total into an int32. Above " +
		"roughly level 1000 the sum overflows, wraps negative, and the loop " +
		"condition stays true, so this does not terminate in reasonable time. " +
		"The fix is to accumulate into an int64 and cap the level.")

	if got := utils.GetUserLevel(math.MaxInt32); got < 0 {
		t.Errorf("GetUserLevel(MaxInt32) = %d, want a sensible positive level", got)
	}
}
