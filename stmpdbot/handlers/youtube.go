package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
)

func GetYoutubeVideos(b *stmpdbot.STMPDBot, ticker *time.Ticker) {
	playlistIDs := []string{
		"UU5H_KXkPbEsGs0tFt8R35mA",           // Martin Garrix uploads
		"PLwPIORXMGwchuy4DTiIAasWRezahNrbUJ", // Martin Garrix custom playlist
		"UUB-7IEpKGIdXkgGUObE5D5A",           // STMPD RCRDS uploads
	}

	for ; ; <-ticker.C {
		slog.Info("Running youtube video fetcher")

		// Create a batch notifier for this cycle
		notifier := utils.NewBatchNotifier(b.Queries, b.Client.Rest, utils.NotificationTypeYoutube)

		var lastYoutubeErr error

		// A cycle counts as healthy if at least one playlist responded. All three
		// failing means the API key or quota is the problem, not one bad playlist.
		playlistsOK := 0

		for _, playlistID := range playlistIDs {
			resp, err := b.YoutubeService.PlaylistItems.
				List([]string{"snippet"}).
				PlaylistId(playlistID).
				MaxResults(5).Do()

			if err != nil {
				slog.Error("Failed to fetch youtube videos", slog.Any("err", err))
				lastYoutubeErr = err
				continue
			}
			playlistsOK++

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

		if playlistsOK == 0 {
			utils.RecordSourceFailure(utils.SourceYouTube, lastYoutubeErr)
		} else {
			utils.RecordSourceSuccess(utils.SourceYouTube)
		}

		// Send all batched notifications once
		if err := notifier.Send(); err != nil {
			slog.Error("Failed to send batched youtube notifications", slog.Any("err", err))
		}
	}
}
