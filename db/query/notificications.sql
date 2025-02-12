-- name: InsertRedditPost :exec
INSERT INTO reddit_posts(post_id) VALUES ($1);

-- name: InsertYoutubeVideo :exec
INSERT INTO youtube_videos(video_id) VALUES  ($1);