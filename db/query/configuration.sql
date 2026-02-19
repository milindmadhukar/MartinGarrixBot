-- name: GetRedditNotificationChannels :many
SELECT reddit_notifications_channel, reddit_notifications_role 
FROM guilds
WHERE reddit_notifications_channel IS NOT NULL;

-- name: GetYoutubeNotifactionChannels :many
SELECT youtube_notifications_channel, youtube_notifications_role 
FROM guilds
WHERE youtube_notifications_channel IS NOT NULL;

-- name: GetSTMPDNofiticationChannels :many
SELECT stmpd_notifications_channel, stmpd_notifications_role
FROM guilds
WHERE stmpd_notifications_channel IS NOT NULL;

-- name: GetRadioVoiceChannels :many
SELECT guild_id, radio_voice_channel
FROM guilds
WHERE radio_voice_channel IS NOT NULL;

-- name: CreateGuild :one
INSERT INTO guilds(guild_id)
VALUES ($1)
ON CONFLICT (guild_id) DO NOTHING
RETURNING *;

-- name: GetGuild :one
SELECT * FROM guilds WHERE guild_id = $1;