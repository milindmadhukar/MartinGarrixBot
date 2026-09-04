//go:build integration

package db_test

// Backs the AI persona agent's remember/forget tools (stmpdbot/ai/memory.go).
// The eviction query and the guild-scoped delete are both real SQL, not Go,
// so they can only be verified here.

import (
	"context"
	"testing"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

func insertMemory(t *testing.T, q *db.Queries, scope string, scopeID, guildID int64, content string) db.AgentMemory {
	t.Helper()

	row, err := q.InsertAgentMemory(context.Background(), db.InsertAgentMemoryParams{
		Scope: scope, ScopeID: scopeID, GuildID: guildID, Content: content,
	})
	if err != nil {
		t.Fatalf("InsertAgentMemory failed: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM agent_memory WHERE id = $1", row.ID)
	})
	return row
}

func TestAgentMemory_EvictOldKeepsOnlyTheNewest(t *testing.T) {
	q := queries(t)
	ctx := context.Background()

	guildID := uniqueSuffix.Add(1)
	scopeID := uniqueSuffix.Add(1)

	var rows []db.AgentMemory
	for i := range 5 {
		rows = append(rows, insertMemory(t, q, "user", scopeID, guildID, testSongName(t, "fact")))
		_ = i
	}

	if err := q.EvictOldAgentMemory(ctx, db.EvictOldAgentMemoryParams{
		GuildID: guildID, Scope: "user", ScopeID: scopeID, KeepCount: 3,
	}); err != nil {
		t.Fatalf("EvictOldAgentMemory failed: %v", err)
	}

	remaining, err := q.GetAgentMemories(ctx, db.GetAgentMemoriesParams{
		GuildID: guildID, Scope: "user", ScopeID: scopeID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("GetAgentMemories failed: %v", err)
	}
	if len(remaining) != 3 {
		t.Fatalf("got %d memories after eviction, want 3", len(remaining))
	}

	// Newest first, and it must be exactly the last 3 inserted -- the two
	// oldest (rows[0], rows[1]) are the ones eviction should have dropped.
	wantIDs := map[int64]bool{rows[2].ID: true, rows[3].ID: true, rows[4].ID: true}
	for _, m := range remaining {
		if !wantIDs[m.ID] {
			t.Errorf("unexpected memory survived eviction: id=%d content=%q", m.ID, m.Content)
		}
	}
	if remaining[0].ID != rows[4].ID {
		t.Errorf("GetAgentMemories not newest-first: got id=%d first, want id=%d (the last inserted)", remaining[0].ID, rows[4].ID)
	}
}

func TestAgentMemory_DeleteIsScopedToGuild(t *testing.T) {
	q := queries(t)
	ctx := context.Background()

	guildID := uniqueSuffix.Add(1)
	wrongGuildID := uniqueSuffix.Add(1)
	scopeID := uniqueSuffix.Add(1)

	row := insertMemory(t, q, "user", scopeID, guildID, "a fact only guildID should be able to remove")

	n, err := q.DeleteAgentMemory(ctx, db.DeleteAgentMemoryParams{ID: row.ID, GuildID: wrongGuildID})
	if err != nil {
		t.Fatalf("DeleteAgentMemory (wrong guild) failed: %v", err)
	}
	if n != 0 {
		t.Fatalf("DeleteAgentMemory removed a row scoped to a different guild: rows_affected=%d", n)
	}

	n, err = q.DeleteAgentMemory(ctx, db.DeleteAgentMemoryParams{ID: row.ID, GuildID: guildID})
	if err != nil {
		t.Fatalf("DeleteAgentMemory (correct guild) failed: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteAgentMemory did not remove the row: rows_affected=%d", n)
	}
}
