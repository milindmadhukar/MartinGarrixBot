-- name: GetSong :one
SELECT * FROM songs WHERE name = $1 AND artists = $2 AND release_date = $3;

-- name: GetSongByID :one
SELECT * FROM songs WHERE id = $1;

-- name: GetSongByBeatportID :one
SELECT * FROM songs WHERE beatport_id = $1;

-- name: GetSongsLike :many
SELECT name, artists, release_date
FROM songs
WHERE LOWER(artists || ' - ' || name) LIKE LOWER($1)
LIMIT 20;

-- name: GetRandomSongNames :many
SELECT name, artists, release_date
FROM songs
ORDER BY RANDOM()
LIMIT 20;

-- name: GetSongsWithLyricsLike :many
SELECT name, artists, release_date 
FROM songs
WHERE lyrics IS NOT NULL AND
LOWER(artists || ' - ' || name) LIKE LOWER($1)
LIMIT 20;

-- name: GetRandomSongNamesWithLyrics :many
SELECT name, artists, release_date 
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
INSERT INTO songs (name, artists, release_date, thumbnail_url, spotify_url, apple_music_url, youtube_url, source)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'stmpd')
RETURNING *;

-- name: InsertBeatportSong :one
INSERT INTO songs (
    name, artists, release_date, thumbnail_url, beatport_id, mix_name,
    release_name, genre, sub_genre, bpm, musical_key, length_ms, source
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'beatport')
RETURNING *;

-- name: DoesSongExist :one
SELECT EXISTS(SELECT 1 FROM songs WHERE name = $1 AND artists = $2 AND release_date = $3);

-- name: DoesBeatportSongExist :one
SELECT EXISTS(SELECT 1 FROM songs WHERE beatport_id = $1);

-- name: UpdateSongWithBeatportData :exec
UPDATE songs SET
    name = $2,
    artists = $3,
    thumbnail_url = $4,
    beatport_id = $5,
    mix_name = $6,
    release_date = $7,
    release_name = $8,
    genre = $9,
    sub_genre = $10,
    bpm = $11,
    musical_key = $12,
    length_ms = $13,
    beatport_updated = TRUE
WHERE id = $1;

-- name: UpdateSongWithStmpdLinks :exec
UPDATE songs SET
    spotify_url = COALESCE($2, spotify_url),
    apple_music_url = COALESCE($3, apple_music_url),
    youtube_url = COALESCE($4, youtube_url),
    thumbnail_url = CASE WHEN thumbnail_url IS NULL OR thumbnail_url = '' THEN $5 ELSE thumbnail_url END,
    beatport_updated = TRUE
WHERE id = $1;

-- name: MarkBeatportUpdated :exec
UPDATE songs SET beatport_updated = TRUE WHERE id = $1;

-- name: GetAllSongsForMatching :many
SELECT id, name, artists, beatport_id, source
FROM songs;

-- name: GetRandomSongForRadio :one
SELECT id, name, artists, thumbnail_url, youtube_url
FROM songs
WHERE youtube_url IS NOT NULL
  AND (length_ms IS NULL OR length_ms <= 600000)
ORDER BY RANDOM()
LIMIT 1;