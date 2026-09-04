-- Durable memory for the AI persona agent (cmd/agent). One row is one
-- remembered fact, scoped to either a user (within a guild) or a whole
-- guild. Kept as small discrete rows rather than one growing blob per scope
-- so a single fact can be forgotten and so the eviction cap (enforced in Go,
-- see stmpdbot/ai/memory.go) has something to count.
CREATE TABLE agent_memory (
    id BIGSERIAL PRIMARY KEY,
    scope TEXT NOT NULL CHECK (scope IN ('user', 'guild')),
    -- For scope = 'guild' this equals guild_id; kept as a separate column
    -- rather than nullable so both scopes share one straightforward lookup
    -- query.
    scope_id BIGINT NOT NULL,
    guild_id BIGINT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The lookup this table exists for: "everything remembered about this
-- scope, in this guild, newest first."
CREATE INDEX idx_agent_memory_scope ON agent_memory (guild_id, scope, scope_id, created_at DESC);
