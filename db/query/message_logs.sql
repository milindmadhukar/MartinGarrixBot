-- name: GetDeleteLogsChannel :one
SELECT delete_logs_channel
FROM guild_configurations
WHERE guild_id = $1;

-- name: GetEditLogsChannel :one
SELECT edit_logs_channel
FROM guild_configurations
WHERE guild_id = $1;
