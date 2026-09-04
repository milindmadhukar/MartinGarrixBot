package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

const (
	// maxMemoryContentLen bounds one remembered fact. Every kept row is
	// folded into every future system prompt for that scope, so this is a
	// cost control, not just tidiness.
	maxMemoryContentLen = 300
	// maxMemoriesPerScope caps how many facts one user (or one guild) can
	// accumulate. EvictOldAgentMemory enforces this in the database after
	// every insert; LoadMemoryContext fetches up to the same number.
	maxMemoriesPerScope = 20
)

// LoadMemoryContext fetches this user's and this guild's remembered facts
// and renders them as a system-prompt section, or "" if there are none.
// Called once per request, before the tool loop starts -- memory is always
// read, never left to the model to proactively recall, so it can't forget to
// check.
func LoadMemoryContext(ctx context.Context, queries *db.Queries, guildID, userID int64) (string, error) {
	userMemories, err := queries.GetAgentMemories(ctx, db.GetAgentMemoriesParams{
		GuildID: guildID, Scope: "user", ScopeID: userID, Limit: maxMemoriesPerScope,
	})
	if err != nil {
		return "", fmt.Errorf("memory: failed to load user memories: %w", err)
	}
	guildMemories, err := queries.GetAgentMemories(ctx, db.GetAgentMemoriesParams{
		GuildID: guildID, Scope: "guild", ScopeID: guildID, Limit: maxMemoriesPerScope,
	})
	if err != nil {
		return "", fmt.Errorf("memory: failed to load guild memories: %w", err)
	}
	if len(userMemories) == 0 && len(guildMemories) == 0 {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("## What you remember\n")
	if len(userMemories) > 0 {
		b.WriteString("\nAbout this specific person, from past conversations:\n")
		for _, m := range userMemories {
			fmt.Fprintf(&b, "- %s\n", m.Content)
		}
	}
	if len(guildMemories) > 0 {
		b.WriteString("\nAbout this server generally:\n")
		for _, m := range guildMemories {
			fmt.Fprintf(&b, "- %s\n", m.Content)
		}
	}
	return b.String(), nil
}

func memoryTools() []Tool {
	return []Tool{
		{Type: "function", Function: ToolFunction{
			Name:        "remember",
			Description: "Save a durable fact for next time: a preference, a running joke with this person, a correction. Use \"user\" scope for something about the specific person you're talking to, \"guild\" scope for something true of the server generally. Don't call this for anything sensitive or private, and not on every message -- only when something's actually worth carrying forward.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"scope": {"type": "string", "enum": ["user", "guild"]},
					"content": {"type": "string", "description": "one short, self-contained fact, under 300 characters"}
				},
				"required": ["scope", "content"]
			}`),
		}},
		{Type: "function", Function: ToolFunction{
			Name:        "forget",
			Description: "Remove a previously remembered fact by its memory_id (shown alongside anything remembered about this person/server in your context).",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"memory_id": {"type": "integer"}
				},
				"required": ["memory_id"]
			}`),
		}},
	}
}

// dispatchMemoryTool handles remember/forget. userID is the Discord user who
// triggered this conversation -- "user" scope always targets them, never an
// id the model supplies, so a message can't be crafted to make the bot store
// a memory against someone else.
func dispatchMemoryTool(ctx context.Context, queries *db.Queries, guildID, userID int64, name, argsJSON string) (string, error) {
	switch name {
	case "remember":
		var args struct {
			Scope   string `json:"scope"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("remember: bad arguments: %w", err)
		}
		if args.Scope != "user" && args.Scope != "guild" {
			return "", fmt.Errorf("remember: scope must be \"user\" or \"guild\", got %q", args.Scope)
		}
		content := args.Content
		if len(content) > maxMemoryContentLen {
			content = content[:maxMemoryContentLen]
		}

		scopeID := userID
		if args.Scope == "guild" {
			scopeID = guildID
		}

		row, err := queries.InsertAgentMemory(ctx, db.InsertAgentMemoryParams{
			Scope: args.Scope, ScopeID: scopeID, GuildID: guildID, Content: content,
		})
		if err != nil {
			return "", err
		}
		if err := queries.EvictOldAgentMemory(ctx, db.EvictOldAgentMemoryParams{
			GuildID: guildID, Scope: args.Scope, ScopeID: scopeID, KeepCount: maxMemoriesPerScope,
		}); err != nil {
			return "", err
		}
		return marshal(map[string]any{"remembered": true, "memory_id": row.ID})

	case "forget":
		var args struct {
			MemoryID int64 `json:"memory_id"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("forget: bad arguments: %w", err)
		}
		n, err := queries.DeleteAgentMemory(ctx, db.DeleteAgentMemoryParams{ID: args.MemoryID, GuildID: guildID})
		if err != nil {
			return "", err
		}
		return marshal(map[string]any{"forgotten": n > 0})

	default:
		return "", fmt.Errorf("ai: unknown memory tool %q", name)
	}
}
