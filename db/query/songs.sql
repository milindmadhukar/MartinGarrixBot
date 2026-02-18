-- name: GetSong :one
SELECT * FROM songs WHERE name = $1 AND artists = $2 AND release_year = $3;

-- name: GetSongsLike :many
SELECT name, artists, release_year
FROM songs
WHERE LOWER(artists || ' - ' || name) LIKE LOWER($1)
LIMIT 20;

-- name: GetRandomSongNames :many
SELECT name, artists, release_year
FROM songs
ORDER BY RANDOM()
LIMIT 20;

-- name: GetSongsWithLyricsLike :many
SELECT name, artists, release_year 
FROM songs
WHERE lyrics IS NOT NULL AND
LOWER(artists || ' - ' || name) LIKE LOWER($1)
LIMIT 20;

-- name: GetRandomSongNamesWithLyrics :many
SELECT name, artists, release_year 
FROM songs
WHERE lyrics IS NOT NULL
ORDER BY RANDOM()
LIMIT 20;

-- name: GetRandomSongWithLyrics :one
SELECT * FROM songs
WHERE lyrics IS NOT NULL
AND (LOWER(artists) LIKE '%martin garrix%' 
   OR LOWER(artists) LIKE '%area21%'
   OR LOWER(artists) LIKE '%ytrram%'
   OR LOWER(artists) LIKE '%grx%')
ORDER BY RANDOM()
LIMIT 1;

-- name: GetRandomSongWithLyricsEasy :one
SELECT * FROM songs
WHERE lyrics IS NOT NULL
AND LOWER(artists) LIKE '%martin garrix%'
ORDER BY RANDOM()
LIMIT 1;

-- name: InsertRelease :one
INSERT INTO songs (name, artists, release_year, thumbnail_url, spotify_url, apple_music_url, youtube_url)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: DoesSongExist :one
SELECT EXISTS(SELECT 1 FROM songs WHERE name = $1 AND artists = $2 AND release_year = $3);

-- name: GetRandomSongForRadio :one
SELECT name, artists, thumbnail_url, youtube_url
FROM songs
WHERE youtube_url IS NOT NULL
ORDER BY RANDOM()
LIMIT 1;