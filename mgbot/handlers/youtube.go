package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
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

			slices.Reverse(resp.Items)

			for _, item := range resp.Items {
				videoId := item.Snippet.ResourceId.VideoId
				channelTitle := item.Snippet.ChannelTitle

				err := b.Queries.InsertYoutubeVideo(context.Background(), videoId)
				if err != nil {
					continue
				}

				youtubeNotificationsGuilds, err := b.Queries.GetYoutubeNotifactionChannels(context.Background())
				if err != nil {
					slog.Error("Failed to get youtube notification channels", slog.Any("err", err))
					continue
				}

				for _, guild := range youtubeNotificationsGuilds {

					var content string

					if guild.YoutubeNotificationsRole.Valid {
						content = fmt.Sprintf("Hey <@&%d>, %s just posted a new video. Go check it out!\nhttps://www.youtube.com/watch?v=%s", guild.YoutubeNotificationsRole.Int64, channelTitle, videoId)
					} else {
						content = fmt.Sprintf("Hey, %s just posted a new video. Go check it out!\nhttps://www.youtube.com/watch?v=%s", channelTitle, videoId)
					}

					_, err = b.Client.Rest().CreateMessage(
						snowflake.ID(guild.YoutubeNotificationsChannel.Int64),
						discord.NewMessageCreateBuilder().
							// TODO: Mention Garrix news role,
							SetContent(content).
							Build())

					// TODO: Add channel ID to the log
					if err != nil {
						slog.Error("Failed to send youtube video", slog.Any("err", err))
						continue
					}

					time.Sleep(1 * time.Second)
				}

			}

			if err != nil {
				slog.Error("Failed to fetch youtube videos", slog.Any("err", err))
				continue
			}
		}

	}
}