package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

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

		for _, post := range data.Data.Children[:5] {
			err := b.Queries.InsertRedditPost(context.Background(), post.Data.ID)
			if err != nil {
				continue
			}

			redditPostEmbed := discord.NewEmbedBuilder().
				SetTitle(post.Data.Title).
				SetURL("https://www.reddit.com" + post.Data.Permalink).
				// TODO: Change to reddit orange
				SetColor(utils.ColorSuccess)

			if post.Data.Selftext != "" {
				redditPostEmbed = redditPostEmbed.SetDescription(post.Data.Selftext)
			}

			if len(post.Data.Preview.Images) > 0 {
				imageUrl := post.Data.Preview.Images[0].Source.URL
				redditPostEmbed = redditPostEmbed.SetImage(imageUrl)
			}

			redditPostEmbed = redditPostEmbed.SetFooter(fmt.Sprintf("Author u/%s on Subreddit %s", post.Data.Author, post.Data.SubredditNamePrefixed), "")

			// TODO: Remove hardcoded channel ID, and use configuration
			testChannelID := 864063260915662868

			// TODO: Ping the reddit role
			_, err = b.Client.Rest().CreateMessage(
				snowflake.ID(testChannelID),
				discord.NewMessageCreateBuilder().
					SetEmbeds(redditPostEmbed.Build()).
					Build())
			if err != nil {
				slog.Error("Failed to send reddit post", slog.Any("err", err))
				continue
			}

			time.Sleep(1 * time.Second)
		}
	}
}