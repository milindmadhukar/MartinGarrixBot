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
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/scripts/internal/script"
	"github.com/milindmadhukar/STMPDBot/stmpdbot"
	"github.com/milindmadhukar/STMPDBot/utils"
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

	var inserted, matched, failed, slugged, unreachable int
	prog := script.NewProgress("import beatport tracks", len(tracks))
	for _, track := range tracks {
		prog.Step()
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

			// A row that already exists still needs the slug. Beatport track pages
			// are /track/<slug>/<id> and the catalogue only ever stored the id, so
			// every Beatport button led to a 404. The slug cannot be derived from
			// the stored title -- Beatport slugifies the full name including the
			// feature credit this catalogue strips out -- so this walk, which has
			// the authoritative value in hand, is where it gets filled in.
			if track.Slug != "" {
				n, err := env.Queries.SetBeatportSlug(ctx, db.SetBeatportSlugParams{
					ID:           existing.ID,
					BeatportSlug: utils.Text(track.Slug),
				})
				if err != nil {
					script.Fatal("failed to store a beatport slug", err)
				}
				if n > 0 {
					slugged++
				}
			}
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
			ID:         song.ID,
			MatchKey:   utils.Text(utils.MatchKey(song.Name, "", song.MixName.String, song.Artists)),
			BaseKey:    utils.Text(utils.BaseKey(song.Name, song.Artists)),
			SearchText: utils.Text(utils.SearchText(song.Artists, song.Name, song.MixName.String, song.ReleaseName.String)),
		}); err != nil {
			script.Fatal("failed to key inserted row", err)
		}

		inserted++
		index.Append(db.GetAllSongsForMatchingRow{
			ID: song.ID, Name: song.Name, Artists: song.Artists, Source: song.Source,
			BeatportID: song.BeatportID, MixName: song.MixName,
		})
	}

	prog.Done()

	// The listings cover the label and the configured artists. Rows that came from
	// elsewhere hold a track id those pages never mention, so their slug -- and with
	// it their only working Beatport link -- has to be fetched one at a time.
	stragglers, err := env.Queries.GetSongsMissingBeatportSlug(ctx)
	if err != nil {
		script.Fatal("failed to list rows missing a beatport slug", err)
	}
	if len(stragglers) > 0 {
		slog.Info("Fetching slugs for tracks outside the configured sources",
			slog.Int("rows", len(stragglers)))
		sp := script.NewProgress("fetch individual track slugs", len(stragglers))
		for _, row := range stragglers {
			sp.Step()
			track, err := client.GetTrack(row.BeatportID.Int32)
			if err != nil {
				// Territory-restricted and delisted tracks are expected here. The
				// row simply shows one fewer button rather than a broken one.
				unreachable++
				slog.Debug("no slug available for track",
					slog.Int("track_id", int(row.BeatportID.Int32)), slog.Any("err", err))
				continue
			}
			if track.Slug == "" {
				unreachable++
				continue
			}
			n, err := env.Queries.SetBeatportSlug(ctx, db.SetBeatportSlugParams{
				ID: row.ID, BeatportSlug: utils.Text(track.Slug),
			})
			if err != nil {
				script.Fatal("failed to store a beatport slug", err)
			}
			if n > 0 {
				slugged++
			}
		}
		sp.Done()
	}

	slog.Info("Beatport import complete",
		slog.Int("tracks_seen", len(tracks)),
		slog.Int("inserted", inserted),
		slog.Int("already_represented", matched),
		slog.Int("beatport_slugs_filled", slugged),
		slog.Int("tracks_with_no_reachable_slug", unreachable),
		slog.Int("failed", failed))
	slog.Info("Run link-remix-parents next so the new remix rows are grouped")
}

func fetchAll(client *utils.BeatportClient, cfg *stmpdbot.Config) map[int]utils.BeatportTrack {
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
		Name: track.Name, Artists: artists, ReleaseDate: utils.Text(track.ReleaseDate),
		ThumbnailUrl: utils.Text(track.ThumbnailURL),
		BeatportID:   pgtype.Int4{Int32: int32(track.ID), Valid: true},
		BeatportSlug: utils.Text(track.Slug),
		MixName:      utils.Text(track.MixName),
		ReleaseName:  utils.Text(track.Release.Name),
		Genre:        utils.Text(track.Genre.Name),
		SubGenre:     utils.Text(track.SubGenre.Name),
		Bpm:          pgtype.Int4{Int32: int32(track.BPM), Valid: track.BPM > 0},
		MusicalKey:   utils.Text(track.Key.Name),
		LengthMs:     pgtype.Int4{Int32: int32(track.LengthMs), Valid: track.LengthMs > 0},
	}
}
