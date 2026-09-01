package listeners

import (
	"math/rand/v2"
	"time"
)

const (
	// xpCooldown is how long a member must wait between XP awards. Without it a
	// member could farm levels by spamming.
	xpCooldown = time.Minute

	// A message is worth 15 to 25 XP inclusive.
	xpMin = 15
	xpMax = 25
)

// rollXP picks the XP a single message is worth.
func rollXP() int32 {
	return xpMin + rand.Int32N(xpMax-xpMin+1)
}

// nextXP decides whether a message earns XP and what the running total becomes.
// It takes the roll and the clock as arguments so the cooldown can be tested
// without waiting a minute or reaching into the global random source.
//
// lastAddedValid carries pgtype.Int8's Valid flag: a member who has never
// earned XP always earns on their next message.
//
// Note: guilds have an xp_multiplier column, but nothing has ever applied it.
// It is deliberately not a parameter here; see TestNextXP_AppliesMultiplier.
func nextXP(currentXP int32, lastAdded time.Time, lastAddedValid bool, now time.Time, roll int32) (int32, bool) {
	if lastAddedValid && now.Sub(lastAdded.UTC()) < xpCooldown {
		return currentXP, false
	}
	return currentXP + roll, true
}
