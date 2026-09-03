package listeners

import (
	"testing"
	"time"
)

func TestXPAward(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		lastAdded      time.Time
		lastAddedValid bool
		roll           int32
		wantAward      int32
		wantAwarded    bool
	}{
		{
			name:        "a member who has never earned XP always earns",
			roll:        20,
			wantAward:   20,
			wantAwarded: true,
		},
		{
			name:           "a message inside the cooldown earns nothing",
			lastAdded:      now.Add(-30 * time.Second),
			lastAddedValid: true,
			roll:           20,
			wantAward:      0,
		},
		{
			name:           "a message exactly on the cooldown boundary earns",
			lastAdded:      now.Add(-time.Minute),
			lastAddedValid: true,
			roll:           15,
			wantAward:      15,
			wantAwarded:    true,
		},
		{
			name:           "a message after the cooldown earns",
			lastAdded:      now.Add(-2 * time.Minute),
			lastAddedValid: true,
			roll:           25,
			wantAward:      25,
			wantAwarded:    true,
		},
		{
			name:           "one millisecond short of the cooldown earns nothing",
			lastAdded:      now.Add(-time.Minute + time.Millisecond),
			lastAddedValid: true,
			roll:           20,
			wantAward:      0,
		},
		{
			// A row written in another zone must still compare correctly; the
			// listener stamps UTC but the column carries no zone.
			name:           "a timestamp in another zone is compared in UTC",
			lastAdded:      now.Add(-2 * time.Minute).In(time.FixedZone("IST", 5*3600+1800)),
			lastAddedValid: true,
			roll:           18,
			wantAward:      18,
			wantAwarded:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotAward, gotAwarded := xpAward(tt.lastAdded, tt.lastAddedValid, now, tt.roll)

			if gotAward != tt.wantAward {
				t.Errorf("award = %d, want %d", gotAward, tt.wantAward)
			}
			if gotAwarded != tt.wantAwarded {
				t.Errorf("awarded = %v, want %v", gotAwarded, tt.wantAwarded)
			}
		})
	}
}

// A member who never stops talking must not out-earn the cooldown.
func TestXPAward_CooldownLimitsFarming(t *testing.T) {
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

		if award, awarded := xpAward(lastAdded, valid, now, xpMin); awarded {
			total += award
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

// The award is a delta, not a running total. If this regresses to returning a
// total, MessageSent's `total_xp = total_xp + $roll` would double-count it.
//
// The guild xp_multiplier is applied in SQL rather than here; it is covered by
// TestMessageSentAppliesMultiplier in the db integration tests.
func TestXPAward_ReturnsDeltaNotTotal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	award, awarded := xpAward(now.Add(-2*time.Minute), true, now, 20)
	if !awarded || award != 20 {
		t.Errorf("xpAward() = (%d, %v), want (20, true)", award, awarded)
	}

	// On cooldown the delta must be zero, so the SQL adds nothing at all.
	award, awarded = xpAward(now.Add(-time.Second), true, now, 20)
	if awarded || award != 0 {
		t.Errorf("xpAward() on cooldown = (%d, %v), want (0, false)", award, awarded)
	}
}
