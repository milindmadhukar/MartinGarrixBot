package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// GetBeatportReleases periodically fetches new songs from the Beatport API
func GetBeatportReleases(b *mgbot.MartinGarrixBot, ticker *time.Ticker, fetchAll bool) {
	if b.BeatportClient == nil {
		slog.Warn("Beatport client not initialized, skipping beatport releases fetcher")
		return
	}

	for ; ; <-ticker.C {
		slog.Info("Running Beatport releases fetcher")

		maxTracks := b.Cfg.Bot.BeatportMaxTracks
		if fetchAll {
			maxTracks = 0 // 0 = unlimited
			slog.Info("Fetching ALL beatport tracks (--fetch-all-beatport mode)")
		}

		var allTracks []utils.BeatportTrack

		// Fetch from label
		if b.Cfg.Bot.BeatportLabelID != "" {
			slog.Info("Fetching Beatport tracks from label", slog.String("label_id", b.Cfg.Bot.BeatportLabelID))
			labelTracks, err := b.BeatportClient.GetAllLabelTracks(b.Cfg.Bot.BeatportLabelID, maxTracks)
			if err != nil {
				slog.Error("Failed to fetch beatport label tracks", slog.Any("err", err))
			} else {
				allTracks = append(allTracks, labelTracks...)
			}
		}

		// Fetch from artists
		for _, artistID := range b.Cfg.Bot.BeatportArtistIDs {
			slog.Info("Fetching Beatport tracks from artist", slog.String("artist_id", artistID))
			artistTracks, err := b.BeatportClient.GetAllArtistTracks(artistID, maxTracks)
			if err != nil {
				slog.Error("Failed to fetch beatport artist tracks",
					slog.String("artist_id", artistID), slog.Any("err", err))
				continue
			}
			allTracks = append(allTracks, artistTracks...)
		}

		// Deduplicate by beatport track ID
		trackMap := make(map[int]utils.BeatportTrack)
		for _, track := range allTracks {
			trackMap[track.ID] = track
		}

		slog.Info("Beatport total unique tracks fetched", slog.Int("count", len(trackMap)))

		// Load existing songs for similarity matching
		existingSongs, err := b.Queries.GetAllSongsForMatching(context.Background())
		if err != nil {
			slog.Error("Failed to load existing songs for matching", slog.Any("err", err))
			continue
		}

		// Create a batch notifier (only used when NOT in fetchAll mode)
		notifier := utils.NewBatchNotifier(b.Queries, b.Client.Rest(), utils.NotificationTypeSTMPD)

		newCount := 0
		updatedCount := 0
		skippedCount := 0

		for _, track := range trackMap {
			artistsStr := utils.FormatBeatportArtists(track.Artists)

			// Check if this beatport track already exists by beatport_id
			existingSong, err := b.Queries.GetSongByBeatportID(context.Background(), pgtype.Int4{
				Int32: int32(track.ID),
				Valid: true,
			})
			if err == nil { // Song exists with this beatport_id
				if existingSong.BeatportUpdated {
					skippedCount++
					continue
				}

				// Check if updating would cause a duplicate key conflict
				conflicts, _ := b.Queries.DoesSongExist(context.Background(), db.DoesSongExistParams{
					Name:        track.Name,
					Artists:     artistsStr,
					ReleaseDate: track.ReleaseDate,
				})
				if conflicts {
					// Another row already has this (name, artists, release_date) — just mark done
					_ = b.Queries.MarkBeatportUpdated(context.Background(), existingSong.ID)
					skippedCount++
					continue
				}

				err = b.Queries.UpdateSongWithBeatportData(context.Background(), db.UpdateSongWithBeatportDataParams{
					ID:      existingSong.ID,
					Name:    track.Name,
					Artists: artistsStr,
					ThumbnailUrl: pgtype.Text{
						String: track.ThumbnailURL,
						Valid:  track.ThumbnailURL != "",
					},
					BeatportID: pgtype.Int4{
						Int32: int32(track.ID),
						Valid: true,
					},
					MixName: pgtype.Text{
						String: track.MixName,
						Valid:  track.MixName != "",
					},
					ReleaseDate: track.ReleaseDate,
					ReleaseName: pgtype.Text{
						String: track.Release.Name,
						Valid:  track.Release.Name != "",
					},
					Genre: pgtype.Text{
						String: track.Genre.Name,
						Valid:  track.Genre.Name != "",
					},
					SubGenre: pgtype.Text{
						String: track.SubGenre.Name,
						Valid:  track.SubGenre.Name != "",
					},
					Bpm: pgtype.Int4{
						Int32: int32(track.BPM),
						Valid: track.BPM > 0,
					},
					MusicalKey: pgtype.Text{
						String: track.Key.Name,
						Valid:  track.Key.Name != "",
					},
					LengthMs: pgtype.Int4{
						Int32: int32(track.LengthMs),
						Valid: track.LengthMs > 0,
					},
				})

				if err != nil {
					slog.Error("Failed to update song with beatport data",
						slog.String("name", track.Name), slog.Any("err", err))
				} else {
					updatedCount++
				}
				continue
			}

			// Check similarity with existing songs (only if not found by beatport_id)
			matchedSong := findSimilarExistingSong(existingSongs, track.Name, artistsStr)

			if matchedSong != nil {
				// Check if updating would cause a duplicate key conflict
				conflicts, _ := b.Queries.DoesSongExist(context.Background(), db.DoesSongExistParams{
					Name:        track.Name,
					Artists:     artistsStr,
					ReleaseDate: track.ReleaseDate,
				})
				if conflicts {
					_ = b.Queries.MarkBeatportUpdated(context.Background(), matchedSong.ID)
					skippedCount++
					continue
				}

				// Similar song exists (from STMPD) — update it with beatport data
				err = b.Queries.UpdateSongWithBeatportData(context.Background(), db.UpdateSongWithBeatportDataParams{
					ID:      matchedSong.ID,
					Name:    track.Name,
					Artists: artistsStr,
					ThumbnailUrl: pgtype.Text{
						String: track.ThumbnailURL,
						Valid:  track.ThumbnailURL != "",
					},
					BeatportID: pgtype.Int4{
						Int32: int32(track.ID),
						Valid: true,
					},
					MixName: pgtype.Text{
						String: track.MixName,
						Valid:  track.MixName != "",
					},
					ReleaseDate: track.ReleaseDate,
					ReleaseName: pgtype.Text{
						String: track.Release.Name,
						Valid:  track.Release.Name != "",
					},
					Genre: pgtype.Text{
						String: track.Genre.Name,
						Valid:  track.Genre.Name != "",
					},
					SubGenre: pgtype.Text{
						String: track.SubGenre.Name,
						Valid:  track.SubGenre.Name != "",
					},
					Bpm: pgtype.Int4{
						Int32: int32(track.BPM),
						Valid: track.BPM > 0,
					},
					MusicalKey: pgtype.Text{
						String: track.Key.Name,
						Valid:  track.Key.Name != "",
					},
					LengthMs: pgtype.Int4{
						Int32: int32(track.LengthMs),
						Valid: track.LengthMs > 0,
					},
				})

				if err != nil {
					slog.Error("Failed to update song with beatport data",
						slog.String("name", track.Name), slog.Any("err", err))
				} else {
					updatedCount++
				}
				continue
			}

			// No similar song exists — insert new
			song, err := b.Queries.InsertBeatportSong(context.Background(), db.InsertBeatportSongParams{
				Name:        track.Name,
				Artists:     artistsStr,
				ReleaseDate: track.ReleaseDate,
				ThumbnailUrl: pgtype.Text{
					String: track.ThumbnailURL,
					Valid:  track.ThumbnailURL != "",
				},
				BeatportID: pgtype.Int4{
					Int32: int32(track.ID),
					Valid: true,
				},
				MixName: pgtype.Text{
					String: track.MixName,
					Valid:  track.MixName != "",
				},
				ReleaseName: pgtype.Text{
					String: track.Release.Name,
					Valid:  track.Release.Name != "",
				},
				Genre: pgtype.Text{
					String: track.Genre.Name,
					Valid:  track.Genre.Name != "",
				},
				SubGenre: pgtype.Text{
					String: track.SubGenre.Name,
					Valid:  track.SubGenre.Name != "",
				},
				Bpm: pgtype.Int4{
					Int32: int32(track.BPM),
					Valid: track.BPM > 0,
				},
				MusicalKey: pgtype.Text{
					String: track.Key.Name,
					Valid:  track.Key.Name != "",
				},
				LengthMs: pgtype.Int4{
					Int32: int32(track.LengthMs),
					Valid: track.LengthMs > 0,
				},
			})

			if err != nil {
				slog.Error("Failed to insert beatport song",
					slog.String("name", track.Name), slog.Any("err", err))
				continue
			}

			newCount++

			// Add to existing songs list so subsequent tracks can match against it
			existingSongs = append(existingSongs, db.GetAllSongsForMatchingRow{
				ID:      song.ID,
				Name:    song.Name,
				Artists: song.Artists,
				BeatportID: pgtype.Int4{
					Int32: int32(track.ID),
					Valid: true,
				},
				Source: "beatport",
			})

			// Only send announcements in normal mode (not bulk import)
			if !fetchAll {
				// Build announcement embed
				title := fmt.Sprintf("%s - %s", artistsStr, track.Name)
				if track.MixName != "" && track.MixName != "Original Mix" {
					title = fmt.Sprintf("%s (%s)", title, track.MixName)
				}

				embedBuilder := discord.NewEmbedBuilder().
					SetTitle(title).
					SetColor(0x1DB954) // Green for beatport

				if track.ThumbnailURL != "" {
					embedBuilder.SetImage(track.ThumbnailURL)
				}

				// Build footer with metadata
				var footerParts []string
				if track.ReleaseDate != "" {
					footerParts = append(footerParts, fmt.Sprintf("📅 %s", track.ReleaseDate))
				}
				if track.Genre.Name != "" {
					footerParts = append(footerParts, fmt.Sprintf("🎵 %s", track.Genre.Name))
				}
				if track.BPM > 0 {
					footerParts = append(footerParts, fmt.Sprintf("💓 %d BPM", track.BPM))
				}
				if track.Key.Name != "" {
					footerParts = append(footerParts, fmt.Sprintf("🔑 %s", track.Key.Name))
				}
				if track.LengthMs > 0 {
					footerParts = append(footerParts, fmt.Sprintf("⏱ %s", utils.FormatBeatportDuration(track.LengthMs)))
				}

				if len(footerParts) > 0 {
					embedBuilder.SetFooter(strings.Join(footerParts, " | "), "")
				}

				announcementEmbed := embedBuilder.Build()

				// Add beatport link button
				var components []discord.ContainerComponent
				beatportURL := fmt.Sprintf("https://www.beatport.com/track/%d", track.ID)
				components = append(components, discord.NewActionRow(
					discord.NewLinkButton("Beatport", beatportURL),
				))

				// Also add streaming links if available
				if song.SpotifyUrl.Valid || song.YoutubeUrl.Valid || song.AppleMusicUrl.Valid {
					buttons := utils.GetSongButtons(song)
					if len(buttons) > 0 {
						components[0] = discord.NewActionRow(
							append([]discord.InteractiveComponent{
								discord.NewLinkButton("Beatport", beatportURL),
							}, buttons...)...,
						)
					}
				}

				notifier.AddItem(utils.NotificationItem{
					Embed:      &announcementEmbed,
					Components: components,
				})
			}
		}

		slog.Info("Beatport sync complete",
			slog.Int("new", newCount),
			slog.Int("updated", updatedCount),
			slog.Int("skipped", skippedCount))

		// Send notifications
		if !fetchAll {
			if err := notifier.Send(); err != nil {
				slog.Error("Failed to send batched beatport notifications", slog.Any("err", err))
			}
		} else {
			slog.Info("Skipping notifications in --fetch-all-beatport mode")
		}

		// If fetchAll, only run once then switch to normal mode
		if fetchAll {
			slog.Info("Initial beatport bulk import complete, switching to normal periodic mode")
			fetchAll = false
		}
	}
}

// findSimilarExistingSong uses Levenshtein similarity to find a matching song
func findSimilarExistingSong(existingSongs []db.GetAllSongsForMatchingRow, trackName, trackArtists string) *db.GetAllSongsForMatchingRow {
	const similarityThreshold = 0.85

	// Build the combined string for the beatport track
	beatportCombined := trackArtists + " - " + trackName

	for i, existing := range existingSongs {
		existingCombined := existing.Artists + " - " + existing.Name

		if utils.IsCloseMatch(beatportCombined, existingCombined, similarityThreshold) {
			return &existingSongs[i]
		}
	}

	return nil
}
