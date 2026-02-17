package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func GetYoutubeVideos(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	playlistIDs := []string{
		"UU5H_KXkPbEsGs0tFt8R35mA",
		"PLwPIORXMGwchuy4DTiIAasWRezahNrbUJ",
	}

	for ; ; <-ticker.C {
		slog.Info("Running youtube video fetcher")

		// Create a batch notifier for this cycle
		notifier := utils.NewBatchNotifier(b.Queries, b.Client.Rest(), utils.NotificationTypeYoutube)

		for _, playlistID := range playlistIDs {
			resp, err := b.YoutubeService.PlaylistItems.
				List([]string{"snippet"}).
				PlaylistId(playlistID).
				MaxResults(5).Do()

			if err != nil {
				slog.Error("Failed to fetch youtube videos", slog.Any("err", err))
				continue
			}

			slices.Reverse(resp.Items)

			for _, item := range resp.Items {
				videoId := item.Snippet.ResourceId.VideoId
				channelTitle := item.Snippet.ChannelTitle

				err := b.Queries.InsertYoutubeVideo(context.Background(), videoId)
				if err != nil {
					// Video already exists, skip it
					continue
				}

				// Add this video to the batch
				content := fmt.Sprintf("%s just posted a new video. Go check it out!\nhttps://www.youtube.com/watch?v=%s", channelTitle, videoId)
				notifier.AddItem(utils.NotificationItem{
					Content: content,
				})
			}
		}

		// Send all batched notifications once
		if err := notifier.Send(); err != nil {
			slog.Error("Failed to send batched youtube notifications", slog.Any("err", err))
		}
	}
}
