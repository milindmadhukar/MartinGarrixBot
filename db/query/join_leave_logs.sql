-- name: LogMemberJoin :exec
INSERT INTO join_leave_logs (member_id, action, time)
VALUES ($1, 'join', $2);

-- name: LogMemberLeave :exec
INSERT INTO join_leave_logs (member_id, action, time)
VALUES ($1, 'leave', $2);

-- name: GetLeaveJoinLogsChannel :one
SELECT leave_join_logs_channel
FROM guilds
WHERE guild_id = $1;
