-- name: GetSongLyrics :one
SELECT * FROM songs WHERE name = $1;

-- name: GetSongsLike :many
SELECT name, alias FROM songs WHERE name LIKE $1;

-- name: GetAllSongNames :many
SELECT name, alias FROM songs;

-- name: GetSongsWithLyricsLike :many
SELECT name, alias FROM songs WHERE lyrics IS NOT NULL AND name LIKE $1;

-- name: GetAllSongNamesWithLyrics :many
SELECT name, alias FROM songs WHERE lyrics IS NOT NULL;