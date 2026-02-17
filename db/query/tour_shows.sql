-- name: DoesTourShowExist :one
SELECT EXISTS(SELECT 1 FROM tour_shows WHERE show_name = $1 AND show_date = $2 AND venue = $3);

-- name: InsertTourShow :one
INSERT INTO tour_shows (show_name, city, country, venue, show_date, ticket_url)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTourNotificationChannels :many
SELECT tour_notifications_channel, tour_notifications_role 
FROM guild_configurations
WHERE tour_notifications_channel IS NOT NULL;

-- name: GetAllTourShows :many
SELECT * FROM tour_shows ORDER BY show_date ASC;
