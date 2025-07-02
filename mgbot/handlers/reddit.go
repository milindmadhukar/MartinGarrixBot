package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var imageRegex = regexp.MustCompile(`https://.*\.(?:jpg|jpeg|gif|png)`)

func GetRedditPosts(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	url := "https://www.reddit.com/r/Martingarrix/new.json"
	for ; ; <-ticker.C {
		slog.Info("Running reddit post fetcher")
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			slog.Error("Failed to create reddit request", slog.Any("err", err))
			continue
		}
		req.Header.Set("User-Agent", "MartinGarrixBot")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			slog.Error("Failed to fetch reddit posts", slog.Any("err", err))
			continue
		}
		defer resp.Body.Close()

		// Read the body into a byte slice for potential debugging
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("Failed to read response body", slog.Any("err", err))
			continue
		}

		var data utils.RedditResponse
		if err = json.Unmarshal(bodyBytes, &data); err != nil {
			// Log the response body for debugging
			slog.Error("Failed to decode reddit response", 
				slog.Any("err", err),
				slog.String("response_body", string(bodyBytes)),
				slog.Int("status_code", resp.StatusCode))
			continue
		}
		
		posts := data.Data.Children
		if len(posts) > 5 {
			posts = posts[:5]
		}
		slices.Reverse(posts)
		for _, post := range posts {
			err := b.Queries.InsertRedditPost(context.Background(), post.Data.ID)
			if err != nil {
				continue
			}
			redditPostEmbed := discord.NewEmbedBuilder().
				SetTitle(html.UnescapeString(utils.CutString(post.Data.Title, 256))).
				SetURL("https://www.reddit.com"+post.Data.Permalink).
				SetTimestamp(time.Unix(int64(post.Data.CreatedUtc), 0)).
				SetDescription(utils.CutString(html.UnescapeString(post.Data.Selftext), 2048)).
				SetFooter(fmt.Sprintf("Author u/%s on Subreddit %s", post.Data.Author, post.Data.SubredditNamePrefixed), "").
				// TODO: Change to reddit orange
				SetColor(utils.ColorSuccess)
			if imageRegex.MatchString(post.Data.URL) {
				redditPostEmbed.Image = &discord.EmbedResource{
					URL: post.Data.URL,
				}
			}
			redditNotificationsGuilds, err := b.Queries.GetRedditNotificationChannels(context.Background())
			if err != nil {
				slog.Error("Failed to get reddit notification channels", slog.Any("err", err))
				continue
			}
			for _, guild := range redditNotificationsGuilds {
				var content string
				if guild.RedditNotificationsRole.Valid {
					content = fmt.Sprintf("<@&%d>, New post on %s", guild.RedditNotificationsRole.Int64, post.Data.SubredditNamePrefixed)
				} else {
					content = fmt.Sprintf("New post on %s", post.Data.SubredditNamePrefixed)
				}
				_, err = b.Client.Rest().CreateMessage(
					snowflake.ID(guild.RedditNotificationsChannel.Int64),
					discord.NewMessageCreateBuilder().
						// TODO: Ping the reddit role
						SetContent(content).
						SetEmbeds(redditPostEmbed.Build()).
						Build())
				// TODO: Add channel ID to the log
				if err != nil {
					slog.Error("Failed to send reddit post", slog.Any("err", err))
					continue
				}
				time.Sleep(1 * time.Second)
			}
		}
	}
}// TODO: Maybe some logic to restart if it returns / fails? / panics