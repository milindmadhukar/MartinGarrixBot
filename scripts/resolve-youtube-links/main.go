// Command resolve-youtube-links replaces playlist URLs in songs.youtube_url with the
// actual video, and fills the column in where it is empty.
//
// 87 rows point their YouTube button at a playlist rather than a song, which is worse
// than showing no button: it sends people somewhere they did not ask to go. The radio
// is affected too -- radioQuery only trusts a URL that names a single video, so a
// playlist link silently degrades to a text search.
//
// Videos come from the channels' own upload playlists rather than from search. That
// is both cheaper and stricter: search costs 100 quota units per call and would spend
// most of a day's allowance on this, while paginating three playlists costs one unit
// per page -- about 44 for all 2,155 videos -- and can only return uploads by STMPD
// RCRDS or Martin Garrix, so a match is a match against their own catalogue.
//
// Every match is verified with utils.SameRecording before it is written.
package main

import (
	"context"
	"flag"
	"log/slog"

	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/scripts/internal/script"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// uploadPlaylists are the channels whose uploads count as the catalogue. The two UU…
// ids are the auto-generated "all uploads" playlists for Martin Garrix and STMPD
// RCRDS; the third is the curated playlist the bot already watches.
var uploadPlaylists = []struct{ name, id string }{
	{"Martin Garrix uploads", "UU5H_KXkPbEsGs0tFt8R35mA"},
	{"STMPD RCRDS uploads", "UUB-7IEpKGIdXkgGUObE5D5A"},
	{"Martin Garrix playlist", "PLwPIORXMGwchuy4DTiIAasWRezahNrbUJ"},
}

type video struct {
	ID      string
	Title   string
	Artists string
	Track   string
}

var clearSpotify = flag.Bool("clear-spotify-playlists", false,
	"also clear spotify_url where it points at a playlist (they cannot be resolved without the Spotify API)")

func main() {
	env, ctx, cleanup := script.Setup("resolve-youtube-links")
	defer cleanup()

	if env.Config.Bot.YoutubeAPIKey == "" {
		script.Fatal("yt_api_key is not set in the config", nil)
	}

	svc, err := youtube.NewService(ctx, option.WithAPIKey(env.Config.Bot.YoutubeAPIKey))
	if err != nil {
		script.Fatal("failed to create the youtube client", err)
	}

	videos := fetchUploads(ctx, svc)
	slog.Info("Indexed channel uploads", slog.Int("videos", len(videos)))

	rows, err := env.Queries.GetSongsNeedingYoutube(ctx)
	if err != nil {
		script.Fatal("failed to load songs", err)
	}
	slog.Info("Songs whose YouTube link is missing or a playlist", slog.Int("count", len(rows)))

	var resolved, unmatched, unchanged int
	prog := script.NewProgress("match songs to uploads", len(rows))

	for _, row := range rows {
		prog.Step()

		match := findVideo(videos, row)
		if match == nil {
			unmatched++
			slog.Debug("no upload matches this song",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name))
			continue
		}

		url := "https://www.youtube.com/watch?v=" + match.ID
		if env.DryRun {
			slog.Info("would set youtube link",
				slog.Int64("song_id", row.ID), slog.String("name", row.Name),
				slog.String("artists", row.Artists),
				slog.String("video", match.Title), slog.String("url", url))
		}

		n, err := env.Queries.SetSongYoutubeURL(ctx, db.SetSongYoutubeURLParams{
			ID: row.ID, YoutubeUrl: utils.Text(url),
		})
		if err != nil {
			slog.Error("failed to set youtube link",
				slog.Int64("song_id", row.ID), slog.Any("err", err))
			continue
		}
		if n > 0 {
			resolved++
		} else {
			unchanged++
		}
	}
	prog.Done()

	var spotifyCleared int64
	if *clearSpotify {
		spotifyCleared, err = env.Queries.ClearPlaylistSpotifyLinks(ctx)
		if err != nil {
			script.Fatal("failed to clear spotify playlist links", err)
		}
	}

	slog.Info("YouTube link resolution complete",
		slog.Int("songs_considered", len(rows)),
		slog.Int("resolved", resolved),
		slog.Int("already_correct", unchanged),
		slog.Int("no_matching_upload", unmatched),
		slog.Int64("spotify_playlist_links_cleared", spotifyCleared))
}

// fetchUploads pages through each playlist, keeping only items whose title parses as
// a track. Channels post recaps, announcements and vlogs to the same playlist.
func fetchUploads(ctx context.Context, svc *youtube.Service) []video {
	var out []video

	for _, pl := range uploadPlaylists {
		var page string
		count, skipped := 0, 0

		for {
			call := svc.PlaylistItems.List([]string{"snippet"}).
				PlaylistId(pl.id).MaxResults(50)
			if page != "" {
				call = call.PageToken(page)
			}

			resp, err := call.Context(ctx).Do()
			if err != nil {
				slog.Error("failed to page a playlist",
					slog.String("playlist", pl.name), slog.Any("err", err))
				break
			}

			for _, item := range resp.Items {
				if item.Snippet == nil || item.Snippet.ResourceId == nil {
					continue
				}
				artists, track, ok := utils.ParseYoutubeTitle(item.Snippet.Title)
				if !ok {
					skipped++
					continue
				}
				out = append(out, video{
					ID: item.Snippet.ResourceId.VideoId, Title: item.Snippet.Title,
					Artists: artists, Track: track,
				})
				count++
			}

			page = resp.NextPageToken
			if page == "" {
				break
			}
		}

		slog.Info("Read playlist",
			slog.String("playlist", pl.name),
			slog.Int("tracks", count),
			slog.Int("not_track_uploads", skipped))
	}

	return out
}

// findVideo returns the upload that is the same recording as the row, preferring an
// exact rendition match over one that merely names the same song.
func findVideo(videos []video, row db.GetSongsNeedingYoutubeRow) *video {
	var fallback *video

	for i := range videos {
		v := videos[i]
		if !utils.SameRecording(row.Name, row.Artists, v.Track, v.Artists) {
			continue
		}

		_, rowVariant := utils.SplitVariant(row.Name, "", row.MixName.String)
		_, vidVariant := utils.SplitVariant(v.Track, "", "")
		if utils.RenditionsAgree(rowVariant, vidVariant) {
			return &videos[i]
		}
		if fallback == nil {
			fallback = &videos[i]
		}
	}

	// A rendition mismatch is not nothing -- the song is right, the version may not
	// be -- but it is not good enough to write. Report it and move on.
	if fallback != nil {
		slog.Debug("found the song but not the right version",
			slog.String("row", row.Name), slog.String("video", fallback.Title))
	}
	return nil
}
