-- name: LogMemberJoin :exec
INSERT INTO join_leave_logs (member_id, guild_id, action, time)
VALUES ($1, $2, 'join', $3);

-- name: LogMemberLeave :exec
INSERT INTO join_leave_logs (member_id, guild_id, action, time)
VALUES ($1, $2, 'leave', $3);

-- name: GetLeaveJoinLogsChannel :one
SELECT leave_join_logs_channel
FROM guilds
WHERE guild_id = $1;
