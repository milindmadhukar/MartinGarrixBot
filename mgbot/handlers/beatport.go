package handlers

import (
	"context"
	"errors"
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
func GetBeatportReleases(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	// Credentials only change with a restart, so there is nothing to retry for.
	if b.Cfg.Bot.BeatportUsername == "" || b.Cfg.Bot.BeatportPassword == "" {
		slog.Warn("Beatport credentials not configured, skipping beatport releases fetcher")
		return
	}

	for ; ; <-ticker.C {
		slog.Info("Running Beatport releases fetcher")

		// Initialise lazily. NewBeatportClient performs network calls to obtain a
		// client ID and log in, so a transient failure while the bot was starting
		// must not disable the fetcher for the lifetime of the process.
		if b.BeatportClient == nil {
			if err := b.SetupBeatport(); err != nil {
				slog.Error("Failed to initialize beatport client, retrying next cycle", slog.Any("err", err))
				continue
			}
			if b.BeatportClient == nil {
				slog.Error("Beatport client unavailable after setup, retrying next cycle")
				continue
			}
		}

		// Authenticate once per cycle rather than letting each of the five source
		// fetches below do it on demand. A rejected credential used to surface as
		// five identical login attempts and five ERROR lines every 15 minutes; now
		// it costs one attempt, and the client's cooldown suppresses even that.
		if err := b.BeatportClient.EnsureAuthenticated(); err != nil {
			slog.Error("Beatport authentication unavailable, skipping this cycle", slog.Any("err", err))
			utils.RecordSourceFailure(utils.SourceBeatport, err)
			continue
		}

		maxTracks := b.Cfg.Bot.BeatportMaxTracks

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

		// Zero tracks after a successful auth means every source call failed or the
		// catalogue shape changed. Either way it is not a healthy cycle, and saying
		// so is what turns four days of "sync complete new=0" into an alert.
		if len(trackMap) == 0 {
			utils.RecordSourceFailure(utils.SourceBeatport, errors.New("no tracks returned by any configured source"))
		} else {
			utils.RecordSourceSuccess(utils.SourceBeatport)
		}

		// Load existing songs for similarity matching
		existingSongs, err := b.Queries.GetAllSongsForMatching(context.Background())
		if err != nil {
			slog.Error("Failed to load existing songs for matching", slog.Any("err", err))
			continue
		}

		// Built once for the whole cycle. The previous matcher rebuilt nothing and
		// instead ran a Levenshtein comparison against all ~1400 rows for every one
		// of the ~190 tracks fetched, every 15 minutes.
		index := utils.NewSongIndex(existingSongs)

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

				rows, err := b.Queries.UpdateSongWithBeatportData(context.Background(), db.UpdateSongWithBeatportDataParams{
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

				switch {
				case err != nil:
					slog.Error("Failed to update song with beatport data",
						slog.String("name", track.Name), slog.Any("err", err))
				case rows > 0:
					updatedCount++
				default:
					// The row already held exactly this data. Counting it as an
					// update is what made every cycle report updated=~73 forever.
					skippedCount++
				}
				continue
			}

			// Not found by beatport_id: resolve against everything else that could
			// identify this recording. Artists are compared as a set here, so
			// beatport's "Ed Sheeran, Martin Garrix" now finds the STMPD row filed
			// under "Martin Garrix & Ed Sheeran" -- the pairing the old whole-string
			// Levenshtein match could never make.
			matchedSong, matchTier := index.Lookup(utils.SongQuery{
				Title:   track.Name,
				MixName: track.MixName,
				Artists: artistsStr,
				BeatportID: pgtype.Int4{
					Int32: int32(track.ID),
					Valid: true,
				},
			})

			if matchedSong != nil {
				slog.Debug("Matched beatport track to existing song",
					slog.String("name", track.Name),
					slog.String("tier", string(matchTier)),
					slog.Int64("song_id", matchedSong.ID))
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
				rows, err := b.Queries.UpdateSongWithBeatportData(context.Background(), db.UpdateSongWithBeatportDataParams{
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

				switch {
				case err != nil:
					slog.Error("Failed to update song with beatport data",
						slog.String("name", track.Name), slog.Any("err", err))
				case rows > 0:
					updatedCount++
				default:
					// The row already held exactly this data. Counting it as an
					// update is what made every cycle report updated=~73 forever.
					skippedCount++
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

			// Register it so later tracks in this same batch -- the six remixes of
			// one release arrive together -- match against it instead of inserting
			// another copy.
			index.Append(db.GetAllSongsForMatchingRow{
				ID:      song.ID,
				Name:    song.Name,
				Artists: song.Artists,
				MixName: song.MixName,
				BeatportID: pgtype.Int4{
					Int32: int32(track.ID),
					Valid: true,
				},
				Source: song.Source,
			})

			// Announce only rows that have never been announced and are actually
			// recent. Beatport lists extended mixes and every individual remix as
			// separate tracks, so without the recency lock a catalogue re-read
			// floods the channel.
			if !song.AnnouncedAt.Valid && isRecentRelease(track.ReleaseDate) {
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

				// Lead with the beatport track link. This is built from the track id
				// rather than read from songs.beatport_url, which holds a RELEASE
				// URL from the STMPD dataset -- the track link is the more precise
				// destination for a beatport announcement.
				beatportURL := utils.BeatportTrackURL(int32(track.ID))
				buttons := append(
					[]discord.InteractiveComponent{discord.NewLinkButton("Beatport", beatportURL)},
					utils.GetSongButtons(song)...,
				)
				components := utils.ChunkButtonRows(buttons)

				notifier.AddItem(utils.NotificationItem{
					Embed:      &announcementEmbed,
					Components: components,
				})

				// Stamped when the item joins the batch rather than after the batch is
				// sent. A failed send loses one announcement; not stamping would risk
				// replaying the whole batch next cycle, and quiet is the safer failure.
				if err := b.Queries.MarkSongAnnounced(context.Background(), song.ID); err != nil {
					slog.Error("Failed to mark song announced",
						slog.Int64("song_id", song.ID), slog.Any("err", err))
				}
			}
		}

		slog.Info("Beatport sync complete",
			slog.Int("new", newCount),
			slog.Int("updated", updatedCount),
			slog.Int("skipped", skippedCount))

		if err := notifier.Send(); err != nil {
			slog.Error("Failed to send batched beatport notifications", slog.Any("err", err))
		}
	}
}
