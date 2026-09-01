package handlers

import (
	"context"
	"log/slog"
	"time"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

// enrichBatchSize bounds how much work one cycle does.
//
// The gaps are finite and shrinking, so there is no need to hurry: a small batch on a
// slow ticker drains the backlog over a few days and then costs almost nothing,
// without ever putting a burst of requests through Apple's rate limit.
const enrichBatchSize = 20

// GetSongEnrichment fills in what neither STMPD nor beatport supplied.
//
// The two fetchers only ever write what their own source knows. When STMPD publishes a
// release with no artwork -- which is most of the catalogue before 2025 -- or beatport
// has no record of a song at all, the gap used to stay open until somebody ran a
// maintenance script by hand. That is the wrong shape for a bot that runs
// continuously: the scripts should be one-off repairs, not a chore.
//
// Apple is the source here because it is the one that can reach these rows. It needs
// no key and no account, and every candidate is verified against the row it is meant
// to describe before anything is written -- an unverified search result hangs the
// wrong cover, or the wrong date, on a real song.
func GetSongEnrichment(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	client := utils.NewItunesClient()

	for ; ; <-ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		art := enrichArtwork(ctx, b, client)
		dates := enrichDates(ctx, b, client)
		cancel()

		if art > 0 || dates > 0 {
			slog.Info("Song enrichment complete",
				slog.Int("artwork_filled", art),
				slog.Int("dates_filled", dates))
		}
	}
}

func enrichArtwork(ctx context.Context, b *mgbot.MartinGarrixBot, client *utils.ItunesClient) int {
	rows, err := b.Queries.GetSongsMissingArtwork(ctx)
	if err != nil {
		slog.Error("Failed to load songs missing artwork", slog.Any("err", err))
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	if len(rows) > enrichBatchSize {
		rows = rows[:enrichBatchSize]
	}

	filled := 0
	for _, row := range rows {
		result := lookupOrSearch(ctx, client, row.AppleMusicUrl.String, row.Name, row.Artists)
		if result == nil || result.Artwork() == "" {
			continue
		}
		n, err := b.Queries.SetSongArtwork(ctx, db.SetSongArtworkParams{
			ID: row.ID, ThumbnailUrl: utils.Text(result.Artwork()),
		})
		if err != nil {
			slog.Error("Failed to set artwork", slog.Int64("song_id", row.ID), slog.Any("err", err))
			continue
		}
		if n > 0 {
			filled++
		}
	}
	return filled
}

func enrichDates(ctx context.Context, b *mgbot.MartinGarrixBot, client *utils.ItunesClient) int {
	rows, err := b.Queries.GetSongsWithPlaceholderDate(ctx)
	if err != nil {
		slog.Error("Failed to load songs missing a release date", slog.Any("err", err))
		return 0
	}
	if len(rows) == 0 {
		return 0
	}
	if len(rows) > enrichBatchSize {
		rows = rows[:enrichBatchSize]
	}

	filled := 0
	for _, row := range rows {
		result := lookupOrSearch(ctx, client, row.AppleMusicUrl.String, row.Name, row.Artists)
		if result == nil || result.Date() == "" {
			continue
		}

		n, err := b.Queries.SetSongReleaseDate(ctx, db.SetSongReleaseDateParams{
			ID: row.ID, ReleaseDate: utils.Text(result.Date()),
		})
		if err != nil {
			// A corrected date can collide with a twin row. Merging is a judgement
			// the maintenance script makes deliberately; the bot leaves it alone.
			if db.ErrorCode(err) == db.UniqueViolation {
				slog.Debug("Date would collide with an existing row, leaving it",
					slog.Int64("song_id", row.ID), slog.String("name", row.Name))
				continue
			}
			slog.Error("Failed to set release date", slog.Int64("song_id", row.ID), slog.Any("err", err))
			continue
		}
		if n > 0 {
			filled++
		}
	}
	return filled
}

// lookupOrSearch resolves a row against Apple, preferring the id already stored on it
// and verifying anything found by search.
func lookupOrSearch(ctx context.Context, client *utils.ItunesClient, appleURL, name, artists string) *utils.ItunesResult {
	if id := utils.AppleIDFromURL(appleURL); id != "" && !utils.IsApplePlaylistURL(appleURL) {
		if r, err := client.Lookup(ctx, id); err == nil && r != nil {
			return r
		}
	}

	results, err := client.Search(ctx, artists+" "+name, 12)
	if err != nil {
		return nil
	}
	for i := range results {
		if utils.SameRecording(name, artists, results[i].Title(), results[i].ArtistName) {
			return &results[i]
		}
	}
	return nil
}
