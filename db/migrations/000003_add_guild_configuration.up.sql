CREATE TABLE IF NOT EXISTS guild_configurations (
	guild_id BIGINT PRIMARY KEY CHECK (guild_id > 0),
	modlogs_channel BIGINT,
	leave_join_logs_channel BIGINT,
	youtube_notifications_channel BIGINT,
	youtube_notifications_role BIGINT,
	reddit_notifications_channel BIGINT,
	reddit_notifications_role BIGINT,
	stmpd_notifications_channel BIGINT,
	stmpd_notifications_role BIGINT,
	welcomes_channel BIGINT,
	delete_logs_channel BIGINT,
	edit_logs_channel BIGINT,
	bot_channel BIGINT,
	radio_voice_channel BIGINT,
	news_role BIGINT,
	xp_multiplier FLOAT NOT NULL DEFAULT 1.0
);

ALTER TABLE messages ADD COLUMN guild_id BIGINT NOT NULL DEFAULT 690950056202731521;

ALTER TABLE users ADD COLUMN guild_id BIGINT NOT NULL DEFAULT 690950056202731521;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_id_key;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_pkey;
ALTER TABLE users ADD PRIMARY KEY (id, guild_id);