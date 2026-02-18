-- name: GetRedditNotificationChannels :many
SELECT reddit_notifications_channel, reddit_notifications_role 
FROM guild_configurations
WHERE reddit_notifications_channel IS NOT NULL;

-- name: GetYoutubeNotifactionChannels :many
SELECT youtube_notifications_channel, youtube_notifications_role 
FROM guild_configurations
WHERE youtube_notifications_channel IS NOT NULL;

-- name: GetSTMPDNofiticationChannels :many
SELECT stmpd_notifications_channel, stmpd_notifications_role
FROM guild_configurations
WHERE stmpd_notifications_channel IS NOT NULL;

-- name: GetRadioVoiceChannels :many
SELECT guild_id, radio_voice_channel
FROM guild_configurations
WHERE radio_voice_channel IS NOT NULL;