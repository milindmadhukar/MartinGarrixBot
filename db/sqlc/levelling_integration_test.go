//go:build integration

package db_test

// The XP and leaderboard path. Every bug fixed here was invisible to a unit
// test: NULL sort order, an absolute XP write racing itself, a counter that
// moved on a duplicate delivery, and a multiplier that no code path read. All
// four are properties of the SQL against a real schema.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// testGuild creates a guilds row, which MessageSent reads for xp_multiplier.
func testGuild(t *testing.T, guildID int64, multiplier float64) {
	t.Helper()

	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		"INSERT INTO guilds (guild_id, xp_multiplier) VALUES ($1, $2)", guildID, multiplier); err != nil {
		t.Fatalf("failed to create guild: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx, "DELETE FROM guilds WHERE guild_id = $1", guildID); err != nil {
			t.Errorf("failed to clean up guild %d: %v", guildID, err)
		}
	})
}

func setXP(t *testing.T, id, guildID int64, xp int32) {
	t.Helper()

	if _, err := testPool.Exec(context.Background(),
		"UPDATE users SET total_xp = $3 WHERE id = $1 AND guild_id = $2", id, guildID, xp); err != nil {
		t.Fatalf("failed to set total_xp: %v", err)
	}
}

func readUser(t *testing.T, q *db.Queries, id, guildID int64) db.User {
	t.Helper()

	got, err := q.GetUser(context.Background(), db.GetUserParams{ID: id, GuildID: guildID})
	if err != nil {
		t.Fatalf("GetUser failed: %v", err)
	}
	return got
}

var messageSuffix = 900_000_000_000_000_000

func sendMessage(t *testing.T, q *db.Queries, id, guildID int64, roll int32, messageID int64) (db.MessageSentRow, error) {
	t.Helper()

	return q.MessageSent(context.Background(), db.MessageSentParams{
		MessageID: messageID,
		GuildID:   guildID,
		ChannelID: 1,
		AuthorID:  id,
		Content:   "hello",
		Roll:      roll,
		AwardedAt: pgtype.Timestamp{Time: time.Now().UTC(), Valid: true},
	})
}

// The bug that made /leaderboard Levels useless: ORDER BY total_xp DESC sorts
// NULLs FIRST in Postgres, so a guild with any NULL rows showed ten members at
// "Level 0". Migration 000025 makes the column NOT NULL and the query says
// NULLS LAST, so neither half can bring it back on its own.
func TestGetLevelsLeaderboardOrdersByXPDescending(t *testing.T) {
	guildID := testGuildID()
	q := queries(t)

	low := testUser(t, q, guildID, 0, 0)
	high := testUser(t, q, guildID, 0, 0)
	mid := testUser(t, q, guildID, 0, 0)

	setXP(t, low, guildID, 10)
	setXP(t, high, guildID, 5000)
	setXP(t, mid, guildID, 500)

	rows, err := q.GetLevelsLeaderboard(context.Background(), db.GetLevelsLeaderboardParams{
		GuildID: guildID, Offset: 0, Limit: 10,
	})
	if err != nil {
		t.Fatalf("GetLevelsLeaderboard failed: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	want := []int64{high, mid, low}
	for i, id := range want {
		if rows[i].ID != id {
			t.Errorf("row %d = %d, want %d (order: %v)", i, rows[i].ID, id, rows)
		}
	}
}

// The same defect in RANK(): it put the guild's real top XP holder at #185,
// behind every member with a NULL total.
func TestGetUserLevelDataRanksTopMemberFirst(t *testing.T) {
	guildID := testGuildID()
	q := queries(t)

	top := testUser(t, q, guildID, 0, 0)
	for range 5 {
		other := testUser(t, q, guildID, 0, 0)
		setXP(t, other, guildID, 100)
	}
	setXP(t, top, guildID, 99999)

	got, err := q.GetUserLevelData(context.Background(), db.GetUserLevelDataParams{
		ID: top, GuildID: guildID,
	})
	if err != nil {
		t.Fatalf("GetUserLevelData failed: %v", err)
	}
	if got.Rank != 1 {
		t.Errorf("rank = %d, want 1", got.Rank)
	}
}

// MessageSent must ADD its roll. The old query wrote an absolute total computed
// in Go from an earlier read.
func TestMessageSentAddsXPAndReturnsBothTotals(t *testing.T) {
	guildID := testGuildID()
	testGuild(t, guildID, 1.0)
	q := queries(t)

	id := testUser(t, q, guildID, 0, 0)
	setXP(t, id, guildID, 90)

	messageSuffix++
	row, err := sendMessage(t, q, id, guildID, 20, int64(messageSuffix))
	if err != nil {
		t.Fatalf("MessageSent failed: %v", err)
	}

	if row.OldXp != 90 {
		t.Errorf("old_xp = %d, want 90", row.OldXp)
	}
	if row.NewXp != 110 {
		t.Errorf("new_xp = %d, want 110", row.NewXp)
	}
	if got := readUser(t, q, id, guildID); got.TotalXp != 110 {
		t.Errorf("stored total_xp = %d, want 110", got.TotalXp)
	}
}

// A roll of 0 is how the listener says "on cooldown". It must move neither the
// XP nor the cooldown stamp -- stamping it anyway is what would starve a member
// who talks faster than the cooldown.
func TestMessageSentZeroRollAwardsNothing(t *testing.T) {
	guildID := testGuildID()
	testGuild(t, guildID, 1.0)
	q := queries(t)

	id := testUser(t, q, guildID, 0, 0)
	setXP(t, id, guildID, 500)

	messageSuffix++
	row, err := sendMessage(t, q, id, guildID, 0, int64(messageSuffix))
	if err != nil {
		t.Fatalf("MessageSent failed: %v", err)
	}
	if row.OldXp != 500 || row.NewXp != 500 {
		t.Errorf("got (%d, %d), want (500, 500)", row.OldXp, row.NewXp)
	}

	got := readUser(t, q, id, guildID)
	if got.LastXpAdded.Valid {
		t.Error("last_xp_added was stamped for a message that earned nothing")
	}
	if got.MessagesSent != 1 {
		t.Errorf("messages_sent = %d, want 1", got.MessagesSent)
	}
}

// guilds.xp_multiplier has been settable since it was added and was never once
// read. This is the test that was skipped with a BUG note.
func TestMessageSentAppliesMultiplier(t *testing.T) {
	guildID := testGuildID()
	testGuild(t, guildID, 2.0)
	q := queries(t)

	id := testUser(t, q, guildID, 0, 0)

	messageSuffix++
	row, err := sendMessage(t, q, id, guildID, 20, int64(messageSuffix))
	if err != nil {
		t.Fatalf("MessageSent failed: %v", err)
	}
	if row.NewXp != 40 {
		t.Errorf("new_xp = %d, want 40 under a 2x multiplier", row.NewXp)
	}
}

// The smallest multiplier the dashboard allows is 0.1, which rounds a 15 roll to
// 2 -- but the floor must hold even below that, because an award of 0 would stop
// a member advancing forever while still stamping their cooldown.
func TestMessageSentNeverAwardsZeroUnderTinyMultiplier(t *testing.T) {
	guildID := testGuildID()
	testGuild(t, guildID, 0.01)
	q := queries(t)

	id := testUser(t, q, guildID, 0, 0)

	messageSuffix++
	row, err := sendMessage(t, q, id, guildID, 15, int64(messageSuffix))
	if err != nil {
		t.Fatalf("MessageSent failed: %v", err)
	}
	if row.NewXp < 1 {
		t.Errorf("new_xp = %d, want at least 1", row.NewXp)
	}
}

// A guild with no config row must still award the full roll. Without the
// COALESCE the multiplier subquery is NULL, the product is NULL, and GREATEST
// silently collapses every award to 1 XP.
func TestMessageSentWithoutGuildConfigAwardsFullRoll(t *testing.T) {
	guildID := testGuildID()
	q := queries(t)

	id := testUser(t, q, guildID, 0, 0)

	messageSuffix++
	row, err := sendMessage(t, q, id, guildID, 20, int64(messageSuffix))
	if err != nil {
		t.Fatalf("MessageSent failed: %v", err)
	}
	if row.NewXp != 20 {
		t.Errorf("new_xp = %d, want 20 with no guild config row", row.NewXp)
	}
}

// A redelivered gateway event hits ON CONFLICT and inserts nothing, but the
// counter used to increment anyway because the two CTEs are independent.
func TestMessageSentDuplicateDeliveryDoesNotDoubleCount(t *testing.T) {
	guildID := testGuildID()
	testGuild(t, guildID, 1.0)
	q := queries(t)

	id := testUser(t, q, guildID, 0, 0)

	messageSuffix++
	messageID := int64(messageSuffix)

	if _, err := sendMessage(t, q, id, guildID, 20, messageID); err != nil {
		t.Fatalf("first MessageSent failed: %v", err)
	}
	if _, err := sendMessage(t, q, id, guildID, 0, messageID); err != nil {
		t.Fatalf("redelivered MessageSent failed: %v", err)
	}

	if got := readUser(t, q, id, guildID); got.MessagesSent != 1 {
		t.Errorf("messages_sent = %d after a duplicate delivery, want 1", got.MessagesSent)
	}
}

// The lost-update regression test. The old absolute write could drop an award
// when two messages were in flight; an increment inside the UPDATE cannot.
func TestMessageSentConcurrentAwardsAreNotLost(t *testing.T) {
	guildID := testGuildID()
	testGuild(t, guildID, 1.0)
	q := queries(t)

	id := testUser(t, q, guildID, 0, 0)

	const concurrent = 20
	var wg sync.WaitGroup
	for i := range concurrent {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if _, err := sendMessage(t, q, id, guildID, 10, int64(950_000_000_000_000_000+n)); err != nil {
				t.Errorf("concurrent MessageSent failed: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if got := readUser(t, q, id, guildID); got.TotalXp != concurrent*10 {
		t.Errorf("total_xp = %d after %d concurrent awards, want %d",
			got.TotalXp, concurrent, concurrent*10)
	}
}
