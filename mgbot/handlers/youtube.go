package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
)

func GetYoutubeVideos(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	playlistIDs := []string{
		"UU5H_KXkPbEsGs0tFt8R35mA",
		"PLwPIORXMGwchuy4DTiIAasWRezahNrbUJ",
	}

	for ; ; <-ticker.C {
		slog.Info("Running youtube video fetcher")

		for _, playlistID := range playlistIDs {
			resp, err := b.YoutubeService.PlaylistItems.
				List([]string{"snippet"}).
				PlaylistId(playlistID).
				MaxResults(5).Do()

			for _, item := range resp.Items {
				videoId := item.Snippet.ResourceId.VideoId
				channelTitle := item.Snippet.ChannelTitle

				err := b.Queries.InsertYoutubeVideo(context.Background(), videoId)
				if err != nil {
					continue
				}

				// TODO: Remove hardcoded channel ID, and use configuration
				testChannelID := 864063260864675841

				// TODO: Ping the reddit role
				_, err = b.Client.Rest().CreateMessage(
					snowflake.ID(testChannelID),
					discord.NewMessageCreateBuilder().
						// TODO: Mention Garrix news role,
						SetContentf(
							"Hey <%s>, %s just posted a new video. Go check it out!\n%s",
							"GarrixNews",
							channelTitle,
							"https://www.youtube.com/watch?v="+videoId,
						).
						Build())

				if err != nil {
					slog.Error("Failed to send youtube video", slog.Any("err", err))
					continue
				}

				time.Sleep(1 * time.Second)

			}

			if err != nil {
				slog.Error("Failed to fetch youtube videos", slog.Any("err", err))
				continue
			}
		}

	}
}