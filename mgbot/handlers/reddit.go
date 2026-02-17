package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

var imageRegex = regexp.MustCompile(`https://.*\.(?:jpg|jpeg|gif|png)`)

func AuthenticateReddit(b *mgbot.MartinGarrixBot) error {
	data := url.Values{}
	data.Set("grant_type", "password")
	data.Set("username", b.Cfg.Bot.RedditBotUsername)
	data.Set("password", b.Cfg.Bot.RedditBotPassword)

	req, err := http.NewRequest("POST", "https://www.reddit.com/api/v1/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create reddit auth request: %w", err)
	}
	req.SetBasicAuth(b.Cfg.Bot.RedditClientID, b.Cfg.Bot.RedditClientSecret)
	req.Header.Set("User-Agent", "MartinGarrixBot")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reddit API error: %s - %s", resp.Status, string(body))
	}

	// Parse response
	var redditToken utils.RedditToken
	if err := json.Unmarshal(body, &redditToken); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	redditToken.ExpiresAt = time.Now().Add(time.Duration(redditToken.ExpiresIn) * time.Second)

	b.RedditToken = redditToken

	return nil

}

func GetRedditPosts(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {

	if b.RedditToken.AccessToken == "" || b.RedditToken.ExpiresAt.Before(time.Now()) {
		slog.Info("Reddit token expired or not set, authenticating...")
		if err := AuthenticateReddit(b); err != nil {
			slog.Error("Failed to authenticate reddit", slog.Any("err", err))
			return
		}
	}

	if b.RedditToken.AccessToken == "" {
		slog.Error("Reddit access token is empty after authentication")
		return
	}

	endpoint := fmt.Sprintf("/r/Martingarrix/new?limit=%d", 5)

	for ; ; <-ticker.C {
		slog.Info("Running reddit post fetcher")

		// Create a batch notifier for this cycle
		notifier := utils.NewBatchNotifier(b.Queries, b.Client.Rest(), utils.NotificationTypeReddit)

		req, err := http.NewRequest("GET", "https://oauth.reddit.com"+endpoint, nil)
		req.Header.Set("User-Agent", "MartinGarrixBot")
		// Access token
		req.Header.Set("Authorization", "bearer "+b.RedditToken.AccessToken)
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
				// Post already exists, skip it
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

			// Add this post to the batch
			embed := redditPostEmbed.Build()
			notifier.AddItem(utils.NotificationItem{
				Embed: &embed,
			})
		}

		// Send all batched notifications once
		if err := notifier.Send(); err != nil {
			slog.Error("Failed to send batched reddit notifications", slog.Any("err", err))
		}
	}
} // TODO: Maybe some logic to restart if it returns / fails? / panics