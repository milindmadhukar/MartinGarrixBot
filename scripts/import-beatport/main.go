// Command import-beatport does a one-off unbounded import of the Beatport catalogue.
//
// This was the bot's --fetch-all-beatport flag. It does not belong on a long-running
// process: the flag was consumed by mutating a captured bool inside the fetcher's
// ticker goroutine, so a bulk import could begin inside a bot that had been serving
// traffic for weeks. As a script it can only run when someone deliberately runs it.
//
// It writes rows and never announces: this binary does not import the notifier.
//
// Run rekey-songs first so the matcher can resolve tracks against existing rows.
package main

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/mgbot"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func main() {
	env, ctx, cleanup := script.Setup("import-beatport")
	defer cleanup()

	cfg := env.Config
	if cfg.Bot.BeatportUsername == "" || cfg.Bot.BeatportPassword == "" {
		script.Fatal("beatport credentials are not configured", nil)
	}

	client, err := utils.NewBeatportClient(&utils.BeatportConfig{
		Username:  cfg.Bot.BeatportUsername,
		Password:  cfg.Bot.BeatportPassword,
		LabelID:   cfg.Bot.BeatportLabelID,
		ArtistIDs: cfg.Bot.BeatportArtistIDs,
		MaxTracks: 0, // unlimited
	})
	if err != nil {
		script.Fatal("failed to create the beatport client", err)
	}

	tracks := fetchAll(client, cfg)
	slog.Info("Fetched beatport tracks", slog.Int("unique", len(tracks)))

	rows, err := env.Queries.GetAllSongsForMatching(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}
	index := utils.NewSongIndex(rows)

	var inserted, matched, failed int
	for _, track := range tracks {
		artists := utils.FormatBeatportArtists(track.Artists)

		if existing, tier := index.Lookup(utils.SongQuery{
			Title:      track.Name,
			MixName:    track.MixName,
			Artists:    artists,
			BeatportID: pgtype.Int4{Int32: int32(track.ID), Valid: true},
		}); existing != nil {
			matched++
			slog.Debug("track already represented",
				slog.String("name", track.Name), slog.String("tier", string(tier)))
			continue
		}

		if env.DryRun {
			slog.Info("would insert beatport track",
				slog.String("name", track.Name), slog.String("artists", artists),
				slog.String("release_date", track.ReleaseDate))
			inserted++
			continue
		}

		song, err := env.Queries.InsertBeatportSong(ctx, insertParams(track, artists))
		if err != nil {
			if db.ErrorCode(err) == db.UniqueViolation {
				matched++
				continue
			}
			slog.Error("failed to insert beatport track",
				slog.String("name", track.Name), slog.Any("err", err))
			failed++
			continue
		}

		// Historical catalogue, not news. Stamping it here is what makes silence
		// structural rather than conditional.
		if err := env.Queries.MarkSongAnnounced(ctx, song.ID); err != nil {
			script.Fatal("failed to stamp inserted row as announced", err)
		}
		if _, err := env.Queries.SetSongKeys(ctx, db.SetSongKeysParams{
			ID:       song.ID,
			MatchKey: utils.Text(utils.MatchKey(song.Name, "", song.MixName.String, song.Artists)),
			BaseKey:  utils.Text(utils.BaseKey(song.Name, song.Artists)),
		}); err != nil {
			script.Fatal("failed to key inserted row", err)
		}

		inserted++
		index.Append(db.GetAllSongsForMatchingRow{
			ID: song.ID, Name: song.Name, Artists: song.Artists, Source: song.Source,
			BeatportID: song.BeatportID, MixName: song.MixName,
		})
	}

	slog.Info("Beatport import complete",
		slog.Int("tracks_seen", len(tracks)),
		slog.Int("inserted", inserted),
		slog.Int("already_represented", matched),
		slog.Int("failed", failed))
	slog.Info("Run link-remix-parents next so the new remix rows are grouped")
}

func fetchAll(client *utils.BeatportClient, cfg *mgbot.Config) map[int]utils.BeatportTrack {
	tracks := map[int]utils.BeatportTrack{}

	if cfg.Bot.BeatportLabelID != "" {
		slog.Info("Fetching label catalogue", slog.String("label_id", cfg.Bot.BeatportLabelID))
		labelTracks, err := client.GetAllLabelTracks(cfg.Bot.BeatportLabelID, 0)
		if err != nil {
			script.Fatal("failed to fetch label tracks", err)
		}
		for _, t := range labelTracks {
			tracks[t.ID] = t
		}
	}

	for _, artistID := range cfg.Bot.BeatportArtistIDs {
		slog.Info("Fetching artist catalogue", slog.String("artist_id", artistID))
		artistTracks, err := client.GetAllArtistTracks(artistID, 0)
		if err != nil {
			script.Fatal("failed to fetch artist tracks", err)
		}
		for _, t := range artistTracks {
			tracks[t.ID] = t
		}
	}

	return tracks
}

func insertParams(track utils.BeatportTrack, artists string) db.InsertBeatportSongParams {
	return db.InsertBeatportSongParams{
		Name: track.Name, Artists: artists, ReleaseDate: track.ReleaseDate,
		ThumbnailUrl: utils.Text(track.ThumbnailURL),
		BeatportID:   pgtype.Int4{Int32: int32(track.ID), Valid: true},
		MixName:      utils.Text(track.MixName),
		ReleaseName:  utils.Text(track.Release.Name),
		Genre:        utils.Text(track.Genre.Name),
		SubGenre:     utils.Text(track.SubGenre.Name),
		Bpm:          pgtype.Int4{Int32: int32(track.BPM), Valid: track.BPM > 0},
		MusicalKey:   utils.Text(track.Key.Name),
		LengthMs:     pgtype.Int4{Int32: int32(track.LengthMs), Valid: track.LengthMs > 0},
	}
}
