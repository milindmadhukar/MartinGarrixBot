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
		if len(releases) > 5 {
			releases = releases[len(releases)-5:]
		}

		// Load existing songs for similarity matching
		existingSongs, err := b.Queries.GetAllSongsForMatching(context.Background())
		if err != nil {
			slog.Error("Failed to load existing songs for STMPD matching", slog.Any("err", err))
			continue
		}

		// Create a batch notifier for this cycle
		notifier := utils.NewBatchNotifier(b.Queries, b.Client.Rest(), utils.NotificationTypeSTMPD)

		for _, release := range releases {
			// Convert release year to release_date format
			releaseDate := fmt.Sprintf("%d-01-01", release.ReleaseYear)

			// First check exact match in DB
			doesExist, err := b.Queries.DoesSongExist(context.Background(), db.DoesSongExistParams{
				Name:        release.Name,
				Artists:     release.Artists,
				ReleaseDate: releaseDate,
			})

			if err != nil {
				slog.Error("Failed to check if song exists", slog.Any("err", err))
				continue
			}

			if doesExist {
				continue
			}

			// Check similarity with existing songs (especially beatport songs)
			matchedSong := findSimilarExistingSong(existingSongs, release.Name, release.Artists)

			if matchedSong != nil && matchedSong.BeatportID.Valid {
				// Check if already updated — avoid re-updating every run
				fullSong, lookupErr := b.Queries.GetSongByID(context.Background(), matchedSong.ID)
				if lookupErr == nil && fullSong.BeatportUpdated {
					continue
				}

				// A similar beatport song exists — update it with STMPD links silently
				err = b.Queries.UpdateSongWithStmpdLinks(context.Background(), db.UpdateSongWithStmpdLinksParams{
					ID: matchedSong.ID,
					SpotifyUrl: pgtype.Text{
						String: release.SpotifyURL,
						Valid:  release.SpotifyURL != "",
					},
					AppleMusicUrl: pgtype.Text{
						String: release.AppleMusicUrl,
						Valid:  release.AppleMusicUrl != "",
					},
					YoutubeUrl: pgtype.Text{
						String: release.YoutubeURL,
						Valid:  release.YoutubeURL != "",
					},
					ThumbnailUrl: pgtype.Text{
						String: release.Thumbnail,
						Valid:  release.Thumbnail != "",
					},
				})

				if err != nil {
					slog.Error("Failed to update song with STMPD links",
						slog.String("name", release.Name), slog.Any("err", err))
				} else {
					slog.Debug("Updated beatport song with STMPD links",
						slog.String("name", release.Name),
						slog.String("artists", release.Artists),
						slog.Int64("song_id", matchedSong.ID))
				}
				continue
			}

			// No similar song exists — insert new STMPD song
			releaseParams := db.InsertReleaseParams{
				Name:        release.Name,
				Artists:     release.Artists,
				ReleaseDate: releaseDate,
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

			// Add to existing songs list
			existingSongs = append(existingSongs, db.GetAllSongsForMatchingRow{
				ID:      song.ID,
				Name:    song.Name,
				Artists: song.Artists,
				Source:  "stmpd",
			})

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
