package handlers

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/gocolly/colly/v2"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// TODO: Maybe find some way to get the release date of the song?
// Announce anniversary of the song?

// TODO: Find some way to add lyrics to all stmpd songs
// Then we can do a stmpd level difficulty quiz lmao

// TODO: Add a way to remove songs manually (say before release remove)
// Add a way to add songs manually and annouce??

// TODO: All sets kb, when asked AI can query and send link in chat?

func GetAllStmpdReleases(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	for ; ; <-ticker.C {
		slog.Info("Running STMPD RCRDS releases fetcher")

		err := b.Collector.Visit("https://stmpdrcrds.com/archive")
		if err != nil {
			slog.Error("Failed to visit stmpdrcrds.com", slog.Any("err", err))
			continue
		}

		var releases []utils.StmpdRelease

		b.Collector.OnHTML(".releases", func(e *colly.HTMLElement) {
			e.ForEach(".grid__cell", func(_ int, cell *colly.HTMLElement) {
				var release utils.StmpdRelease

				releaseInfoDate, err := strconv.Atoi(cell.ChildText(".release__info__date"))
				if err == nil {
					release.ReleaseYear = releaseInfoDate
				}

				release.Thumbnail = cell.ChildAttr(".release__figure img", "src")
				parsedURL, err := url.Parse(release.Thumbnail)
				if err != nil {
					panic(err)
				}
				urlPath := parsedURL.Path
				dir, file := path.Split(urlPath)
				newFile := strings.Replace(file, "small", "big", 1)
				parsedURL.Path = dir + newFile

				release.Thumbnail = parsedURL.String()

				h3 := cell.DOM.Find(".release__info__h3")
				if h3.Length() > 0 {
					htmlContent, _ := h3.Html()
					parts := strings.Split(htmlContent, "<br/>")
					if len(parts) >= 2 {
						release.Artists = html.UnescapeString(strings.TrimSpace(parts[0]))
						release.Name = html.UnescapeString(strings.TrimSpace(parts[1]))
					}
				}

				cell.ForEach(".links__links__a", func(_ int, link *colly.HTMLElement) {
					href := link.Attr("href")
					if strings.Contains(href, "spotify") {
						release.SpotifyURL = href
					} else if strings.Contains(href, "apple") {
						release.AppleMusicUrl = href
					} else if strings.Contains(href, "youtube") || strings.Contains(href, "youtu.be") {
						release.YoutubeURL = href
					}
				})

				releases = append(releases, release)
			})
		})

		b.Collector.Wait()

		slices.Reverse(releases)
		releases = releases[len(releases)-5:]

		// Create a batch notifier for this cycle
		notifier := utils.NewBatchNotifier(b.Queries, b.Client.Rest(), utils.NotificationTypeSTMPD)

		for _, release := range releases {

			// PERF: Too many queries. Can I reduce them?
			doesExist, err := b.Queries.DoesSongExist(context.Background(), db.DoesSongExistParams{
				Name:        release.Name,
				Artists:     release.Artists,
				ReleaseYear: int32(release.ReleaseYear),
			})

			if err != nil {
				slog.Error("Failed to check if song exists", slog.Any("err", err))
				continue
			}

			if doesExist {
				continue
			}

			releaseParams := db.InsertReleaseParams{
				Name:        release.Name,
				Artists:     release.Artists,
				ReleaseYear: int32(release.ReleaseYear),
			}

			if release.SpotifyURL != "" {
				releaseParams.SpotifyUrl = pgtype.Text{
					String: release.SpotifyURL,
					Valid:  true,
				}
			}

			if release.AppleMusicUrl != "" {
				releaseParams.AppleMusicUrl = pgtype.Text{
					String: release.AppleMusicUrl,
					Valid:  true,
				}
			}

			if release.YoutubeURL != "" {
				releaseParams.YoutubeUrl = pgtype.Text{
					String: release.YoutubeURL,
					Valid:  true,
				}
			}

			if release.Thumbnail != "" {
				releaseParams.ThumbnailUrl = pgtype.Text{
					String: release.Thumbnail,
					Valid:  true,
				}
			}

			song, err := b.Queries.InsertRelease(
				context.Background(), releaseParams,
			)

			if err != nil {
				slog.Error("Failed to insert release for "+release.Name, slog.Any("err", err))
				continue
			}

			announcementEmbed := discord.NewEmbedBuilder().
				SetTitle(fmt.Sprintf("%s - %s", release.Artists, release.Name)).
				SetImage(release.Thumbnail).
				SetFooter(fmt.Sprintf("Release Year: %d", release.ReleaseYear), "").
				Build()

			// Prepare the components for this song
			var components []discord.ContainerComponent
			if song.SpotifyUrl.Valid || song.YoutubeUrl.Valid || song.AppleMusicUrl.Valid {
				components = []discord.ContainerComponent{
					discord.NewActionRow(utils.GetSongButtons(song)...),
				}
			}

			// Add this release to the batch
			notifier.AddItem(utils.NotificationItem{
				Embed:      &announcementEmbed,
				Components: components,
			})
		}

		// Send all batched notifications once
		if err := notifier.Send(); err != nil {
			slog.Error("Failed to send batched STMPD notifications", slog.Any("err", err))
		}
	}
}
