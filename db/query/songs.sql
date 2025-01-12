-- name: GetSongLyrics :one
SELECT * FROM songs WHERE name = $1;

-- name: GetSongsLike :many
SELECT name, artists FROM songs WHERE name LIKE $1;

-- name: GetAllSongNames :many
SELECT name, artists FROM songs;

-- name: GetSongsWithLyricsLike :many
SELECT name, artists FROM songs
WHERE lyrics IS NOT NULL AND name LIKE $1
LIMIT 20;

-- name: GetAllSongNamesWithLyrics :many
SELECT name, artists FROM songs
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

-- name: InsertRelease :exec
INSERT INTO songs (name, artists, release_year, thumbnail_url, spotify_url, apple_music_url, youtube_url)
VALUES ($1, $2, $3, $4, $5, $6, $7);