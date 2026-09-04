-- name: DoesTourShowExist :one
SELECT EXISTS(SELECT 1 FROM tour_shows WHERE show_name = $1 AND show_date = $2 AND venue = $3);

-- name: InsertTourShow :one
INSERT INTO tour_shows (show_name, city, country, venue, show_date, ticket_url)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTourNotificationChannels :many
SELECT tour_notifications_channel, tour_notifications_role 
FROM guilds
WHERE tour_notifications_channel IS NOT NULL;

-- name: GetAllTourShows :many
SELECT * FROM tour_shows ORDER BY show_date ASC;

-- name: SearchTourShowsForAgent :many
-- Backs the AI persona feature's "search_tour_shows" tool (stmpdbot/ai).
-- location is optional and matches city or country; the table is small
-- enough (~80 rows) that upcoming/past is filtered in Go rather than here,
-- so one query serves both "when's he playing near me" and "did he already
-- play X".
SELECT show_name, city, country, venue, show_date, ticket_url
FROM tour_shows
WHERE sqlc.narg(location)::text IS NULL
   OR city ILIKE '%' || sqlc.narg(location)::text || '%'
   OR country ILIKE '%' || sqlc.narg(location)::text || '%'
ORDER BY show_date ASC;
