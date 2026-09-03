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

// xpAward decides what a message earns. It returns a DELTA, not a running
// total: MessageSent adds it inside the UPDATE, so the database is the only
// thing that ever decides a member's total. The old form read the total, added
// to it in Go and wrote the result back, which is a read-modify-write across
// two statements and can lose an award.
//
// It takes the roll and the clock as arguments so the cooldown can be tested
// without waiting a minute or reaching into the global random source.
//
// lastAddedValid carries pgtype.Timestamp's Valid flag: a member who has never
// earned XP always earns on their next message.
//
// The guild's xp_multiplier is deliberately NOT applied here. It lives in
// MessageSent's SQL, where reading it costs no extra round trip on the gateway's
// single event goroutine; see the query's comment.
func xpAward(lastAdded time.Time, lastAddedValid bool, now time.Time, roll int32) (int32, bool) {
	if lastAddedValid && now.Sub(lastAdded.UTC()) < xpCooldown {
		return 0, false
	}
	return roll, true
}
