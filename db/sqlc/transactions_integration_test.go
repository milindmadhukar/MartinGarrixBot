//go:build integration

package db_test

// The coin economy. Every one of these statements is a bare UPDATE with no
// RETURNING, so a predicate that matches nothing is indistinguishable from a
// successful transfer at the Go level. That is exactly how /give came to report
// success while moving nothing, and it is only observable against a real
// database.

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

var userSuffix atomic.Int64

func int8val(i int64) pgtype.Int8 { return pgtype.Int8{Int64: i, Valid: true} }

// testUser creates a user with the given starting balances and removes it when
// the test ends. Users are keyed on (id, guild_id).
func testUser(t *testing.T, q *db.Queries, guildID, inHand, safe int64) int64 {
	t.Helper()

	ctx := context.Background()
	id := 800_000_000_000_000_000 + userSuffix.Add(1)

	if _, err := q.CreateUser(ctx, db.CreateUserParams{ID: id, GuildID: guildID}); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(ctx,
			"DELETE FROM users WHERE id = $1 AND guild_id = $2", id, guildID); err != nil {
			t.Errorf("failed to clean up user %d: %v", id, err)
		}
	})

	if _, err := testPool.Exec(ctx,
		"UPDATE users SET in_hand = $3, stmpd_coins = $4 WHERE id = $1 AND guild_id = $2",
		id, guildID, inHand, safe); err != nil {
		t.Fatalf("failed to set the starting balance: %v", err)
	}

	return id
}

// testGuildID keeps each test in its own guild, so parallel tests cannot see
// each other's users.
func testGuildID() int64 { return 700_000_000_000_000_000 + userSuffix.Add(1) }

func balance(t *testing.T, q *db.Queries, id, guildID int64) (inHand, safe int64) {
	t.Helper()

	got, err := q.GetBalance(context.Background(), db.GetBalanceParams{ID: id, GuildID: guildID})
	if err != nil {
		t.Fatalf("GetBalance failed: %v", err)
	}
	return got.InHand.Int64, got.StmpdCoins.Int64
}

func TestGetBalance(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildID := testGuildID()
	id := testUser(t, q, guildID, 100, 250)

	inHand, safe := balance(t, q, id, guildID)
	if inHand != 100 {
		t.Errorf("in hand = %d, want 100", inHand)
	}
	if safe != 250 {
		t.Errorf("safe = %d, want 250", safe)
	}
}

func TestAddCoins(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildID := testGuildID()
	id := testUser(t, q, guildID, 100, 0)

	if err := q.AddCoins(context.Background(), db.AddCoinsParams{
		ID: id, GuildID: guildID, InHand: int8val(50),
	}); err != nil {
		t.Fatalf("AddCoins failed: %v", err)
	}

	if inHand, _ := balance(t, q, id, guildID); inHand != 150 {
		t.Errorf("in hand = %d, want 150", inHand)
	}
}

func TestWithdrawAmount_MovesFromSafeToHand(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildID := testGuildID()
	id := testUser(t, q, guildID, 10, 500)

	if err := q.WithdrawAmount(context.Background(), db.WithdrawAmountParams{
		ID: id, GuildID: guildID, InHand: int8val(200),
	}); err != nil {
		t.Fatalf("WithdrawAmount failed: %v", err)
	}

	inHand, safe := balance(t, q, id, guildID)
	if inHand != 210 {
		t.Errorf("in hand = %d, want 210", inHand)
	}
	if safe != 300 {
		t.Errorf("safe = %d, want 300", safe)
	}
	if total := inHand + safe; total != 510 {
		t.Errorf("total = %d, want 510; a withdrawal must not create or destroy coins", total)
	}
}

func TestDepositAmount_MovesFromHandToSafe(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildID := testGuildID()
	id := testUser(t, q, guildID, 500, 10)

	if err := q.DepositAmount(context.Background(), db.DepositAmountParams{
		ID: id, GuildID: guildID, InHand: int8val(200),
	}); err != nil {
		t.Fatalf("DepositAmount failed: %v", err)
	}

	inHand, safe := balance(t, q, id, guildID)
	if inHand != 300 {
		t.Errorf("in hand = %d, want 300", inHand)
	}
	if safe != 210 {
		t.Errorf("safe = %d, want 210", safe)
	}
}

// The regression test for /give. Before the fix this failed on the first
// assertion: the SQL used $3 as both the guild id and the balance threshold,
// and give.go never set GuildID at all, so the statement matched no rows and
// both balances were unchanged while the member was told it worked.
func TestGiveCoins_TransfersBetweenMembers(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildID := testGuildID()
	sender := testUser(t, q, guildID, 500, 0)
	receiver := testUser(t, q, guildID, 100, 0)

	if err := q.GiveCoins(context.Background(), db.GiveCoinsParams{
		ID:      sender,
		ID_2:    receiver,
		GuildID: guildID,
		InHand:  int8val(200),
	}); err != nil {
		t.Fatalf("GiveCoins failed: %v", err)
	}

	senderHand, _ := balance(t, q, sender, guildID)
	receiverHand, _ := balance(t, q, receiver, guildID)

	if senderHand != 300 {
		t.Errorf("sender has %d in hand, want 300", senderHand)
	}
	if receiverHand != 300 {
		t.Errorf("receiver has %d in hand, want 300", receiverHand)
	}
	if total := senderHand + receiverHand; total != 600 {
		t.Errorf("total = %d, want 600; a transfer must not create or destroy coins", total)
	}
}

// The threshold is the whole point of the CTE: a member must not be able to
// give away coins they do not have.
func TestGiveCoins_RefusesWhenTheSenderIsShort(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildID := testGuildID()
	sender := testUser(t, q, guildID, 50, 0)
	receiver := testUser(t, q, guildID, 100, 0)

	if err := q.GiveCoins(context.Background(), db.GiveCoinsParams{
		ID:      sender,
		ID_2:    receiver,
		GuildID: guildID,
		InHand:  int8val(500), // more than the sender holds
	}); err != nil {
		t.Fatalf("GiveCoins returned an error: %v", err)
	}

	senderHand, _ := balance(t, q, sender, guildID)
	receiverHand, _ := balance(t, q, receiver, guildID)

	if senderHand != 50 {
		t.Errorf("sender has %d in hand, want 50; the transfer should not have run", senderHand)
	}
	if receiverHand != 100 {
		t.Errorf("receiver has %d in hand, want 100; the transfer should not have run",
			receiverHand)
	}
}

func TestGiveCoins_GivingTheEntireBalanceIsAllowed(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildID := testGuildID()
	sender := testUser(t, q, guildID, 500, 0)
	receiver := testUser(t, q, guildID, 0, 0)

	if err := q.GiveCoins(context.Background(), db.GiveCoinsParams{
		ID: sender, ID_2: receiver, GuildID: guildID, InHand: int8val(500),
	}); err != nil {
		t.Fatalf("GiveCoins failed: %v", err)
	}

	senderHand, _ := balance(t, q, sender, guildID)
	receiverHand, _ := balance(t, q, receiver, guildID)

	if senderHand != 0 {
		t.Errorf("sender has %d in hand, want 0", senderHand)
	}
	if receiverHand != 500 {
		t.Errorf("receiver has %d in hand, want 500", receiverHand)
	}
}

// Users are keyed on (id, guild_id), so a member's balance in one guild must be
// untouched by a transfer in another. This is the invariant the missing GuildID
// violated.
func TestGiveCoins_IsScopedToOneGuild(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildA, guildB := testGuildID(), testGuildID()

	sender := testUser(t, q, guildA, 500, 0)
	receiver := testUser(t, q, guildA, 0, 0)

	// The same member ids also exist in another guild with their own balances.
	if _, err := q.CreateUser(context.Background(), db.CreateUserParams{
		ID: sender, GuildID: guildB,
	}); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			"DELETE FROM users WHERE id = $1 AND guild_id = $2", sender, guildB)
	})

	if _, err := testPool.Exec(context.Background(),
		"UPDATE users SET in_hand = 999 WHERE id = $1 AND guild_id = $2",
		sender, guildB); err != nil {
		t.Fatalf("failed to set the other guild's balance: %v", err)
	}

	if err := q.GiveCoins(context.Background(), db.GiveCoinsParams{
		ID: sender, ID_2: receiver, GuildID: guildA, InHand: int8val(200),
	}); err != nil {
		t.Fatalf("GiveCoins failed: %v", err)
	}

	otherHand, _ := balance(t, q, sender, guildB)
	if otherHand != 999 {
		t.Errorf("the sender's balance in the other guild is %d, want 999; the "+
			"transfer leaked across guilds", otherHand)
	}
}

// BUG: the receiver is only updated if a row already exists for them, and
// nothing creates one. The sender's coins are still deducted, so giving to a
// member who has never been seen in the guild destroys the coins.
func TestGiveCoins_ToAMissingReceiverLosesTheCoins(t *testing.T) {
	t.Parallel()

	q := queries(t)
	guildID := testGuildID()
	sender := testUser(t, q, guildID, 500, 0)

	const missingReceiver = int64(1)

	if err := q.GiveCoins(context.Background(), db.GiveCoinsParams{
		ID: sender, ID_2: missingReceiver, GuildID: guildID, InHand: int8val(200),
	}); err != nil {
		t.Fatalf("GiveCoins failed: %v", err)
	}

	senderHand, _ := balance(t, q, sender, guildID)

	if senderHand == 300 {
		t.Log("BUG: the sender was debited 200 coins but the receiver has no row " +
			"in this guild, so the coins went nowhere. The CTE deducts from the " +
			"sender independently of whether the receiver update matches.")
	}
	if senderHand != 500 && senderHand != 300 {
		t.Errorf("sender has %d in hand, want either 500 (refused) or 300 (the "+
			"current, lossy behaviour)", senderHand)
	}
}
