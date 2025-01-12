package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
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

// TODO: Maybe some logic to restart if it returns / fails?
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

		var data utils.RedditResponse
		if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
			slog.Error("Failed to decode reddit response", slog.Any("err", err))
			continue
		}

		posts := data.Data.Children[:5]
		slices.Reverse(posts)

		for _, post := range posts {
			err := b.Queries.InsertRedditPost(context.Background(), post.Data.ID)
			if err != nil {
				continue
			}

			redditPostEmbed := discord.NewEmbedBuilder().
				SetTitle(utils.CutString(post.Data.Title, 256)).
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

			// TODO: Remove hardcoded channel ID, and use configuration
			testChannelID := 864063260915662868

			_, err = b.Client.Rest().CreateMessage(
				snowflake.ID(testChannelID),
				discord.NewMessageCreateBuilder().
					// TODO: Ping the reddit role
					SetContentf(
						"<%s>, New post on %s",
						"@GarrixReddit",
						post.Data.SubredditNamePrefixed,
					).
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