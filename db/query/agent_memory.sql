-- name: InsertAgentMemory :one
INSERT INTO agent_memory (scope, scope_id, guild_id, content)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAgentMemories :many
-- Newest first, capped by the caller -- this is what gets folded into every
-- system prompt, so the caller (stmpdbot/ai/memory.go) always passes a small
-- limit.
SELECT * FROM agent_memory
WHERE guild_id = $1 AND scope = $2 AND scope_id = $3
ORDER BY created_at DESC
LIMIT $4;

-- name: DeleteAgentMemory :execrows
-- Scoped to guild_id so the "forget" tool can only remove a row the caller's
-- own guild owns, even though a memory id is otherwise a bare integer.
DELETE FROM agent_memory WHERE id = $1 AND guild_id = $2;

-- name: EvictOldAgentMemory :exec
-- Runs after every insert so a single scope can never accumulate unbounded
-- rows -- every kept row is folded into every future system prompt, so this
-- is a cost control as much as tidiness.
DELETE FROM agent_memory AS m
WHERE m.guild_id = sqlc.arg(guild_id) AND m.scope = sqlc.arg(scope) AND m.scope_id = sqlc.arg(scope_id)
  AND m.id NOT IN (
      SELECT id FROM agent_memory
      WHERE guild_id = sqlc.arg(guild_id) AND scope = sqlc.arg(scope) AND scope_id = sqlc.arg(scope_id)
      ORDER BY created_at DESC
      LIMIT sqlc.arg(keep_count)
  );
