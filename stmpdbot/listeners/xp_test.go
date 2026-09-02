package listeners

import (
	"testing"
	"time"
)

func TestNextXP(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		currentXP      int32
		lastAdded      time.Time
		lastAddedValid bool
		roll           int32
		wantXP         int32
		wantAwarded    bool
	}{
		{
			name:        "a member who has never earned XP always earns",
			currentXP:   0,
			roll:        20,
			wantXP:      20,
			wantAwarded: true,
		},
		{
			name:           "a message inside the cooldown earns nothing",
			currentXP:      100,
			lastAdded:      now.Add(-30 * time.Second),
			lastAddedValid: true,
			roll:           20,
			wantXP:         100,
		},
		{
			name:           "a message exactly on the cooldown boundary earns",
			currentXP:      100,
			lastAdded:      now.Add(-time.Minute),
			lastAddedValid: true,
			roll:           15,
			wantXP:         115,
			wantAwarded:    true,
		},
		{
			name:           "a message after the cooldown earns",
			currentXP:      100,
			lastAdded:      now.Add(-2 * time.Minute),
			lastAddedValid: true,
			roll:           25,
			wantXP:         125,
			wantAwarded:    true,
		},
		{
			name:           "one millisecond short of the cooldown earns nothing",
			currentXP:      100,
			lastAdded:      now.Add(-time.Minute + time.Millisecond),
			lastAddedValid: true,
			roll:           20,
			wantXP:         100,
		},
		{
			// A row written in another zone must still compare correctly; the
			// listener stamps UTC but the column carries no zone.
			name:           "a timestamp in another zone is compared in UTC",
			currentXP:      50,
			lastAdded:      now.Add(-2 * time.Minute).In(time.FixedZone("IST", 5*3600+1800)),
			lastAddedValid: true,
			roll:           18,
			wantXP:         68,
			wantAwarded:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotXP, gotAwarded := nextXP(tt.currentXP, tt.lastAdded, tt.lastAddedValid, now, tt.roll)

			if gotXP != tt.wantXP {
				t.Errorf("XP = %d, want %d", gotXP, tt.wantXP)
			}
			if gotAwarded != tt.wantAwarded {
				t.Errorf("awarded = %v, want %v", gotAwarded, tt.wantAwarded)
			}
		})
	}
}

// A member who never stops talking must not out-earn the cooldown.
func TestNextXP_CooldownLimitsFarming(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	var (
		total     int32
		lastAdded time.Time
		valid     bool
		awards    int
	)

	// One message a second for ten minutes.
	for i := range 600 {
		now := start.Add(time.Duration(i) * time.Second)

		if next, awarded := nextXP(total, lastAdded, valid, now, xpMin); awarded {
			total = next
			lastAdded = now
			valid = true
			awards++
		}
	}

	// The first message earns, then one per minute after it.
	if awards != 10 {
		t.Errorf("got %d awards over ten minutes of constant messages, want 10", awards)
	}
}

func TestRollXP_StaysInRange(t *testing.T) {
	t.Parallel()

	seen := make(map[int32]bool)
	for range 2000 {
		roll := rollXP()
		if roll < xpMin || roll > xpMax {
			t.Fatalf("rollXP() = %d, outside [%d, %d]", roll, xpMin, xpMax)
		}
		seen[roll] = true
	}

	// Every value in the range should be reachable; an off-by-one in the bound
	// would leave one out.
	for want := int32(xpMin); want <= xpMax; want++ {
		if !seen[want] {
			t.Errorf("rollXP() never produced %d over 2000 rolls", want)
		}
	}
}

func TestNextXP_AppliesMultiplier(t *testing.T) {
	t.Skip("BUG: guilds.xp_multiplier is stored and shown by /config view, but " +
		"nothing has ever applied it. Wiring it up means threading the guild " +
		"config into the message listener and deciding how to round.")

	// A guild with a 2x multiplier should double the roll.
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if got, _ := nextXP(0, time.Time{}, false, now, 20); got != 40 {
		t.Errorf("XP = %d, want 40 with a 2x multiplier", got)
	}
}
