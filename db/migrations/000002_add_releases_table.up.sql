DROP TABLE IF EXISTS songs;

CREATE TABLE IF NOT EXISTS songs(
    id BIGSERIAL PRIMARY KEY,
	name VARCHAR(300) NOT NULL,
	artists VARCHAR(300) NOT NULL,
	release_year INTEGER NOT NULL,
	thumbnail_url TEXT,
	spotify_url TEXT,
	apple_music_url TEXT,
	youtube_url TEXT,
	lyrics TEXT,
	is_unreleased BOOLEAN NOT NULL DEFAULT FALSE,
	pure_title VARCHAR(300),
	CONSTRAINT unique_release UNIQUE (name, artists, release_year)
);