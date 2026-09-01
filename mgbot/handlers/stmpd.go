package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
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

const stmpdArchiveURL = "https://stmpdrcrds.com/archive"

// stmpdClient bounds the archive request. This fetcher no longer uses the shared
// colly collector: the archive is a Next.js app whose release data arrives as a
// JSON payload, so there is no HTML worth traversing.
var stmpdClient = &http.Client{Timeout: 30 * time.Second}

// stmpdArchiveRelease mirrors the release objects STMPD's archive page embeds in
// its Next.js payload. The old CSS-selector scrape broke when the site was
// rebuilt; this payload carries the same fields (plus an exact release date) and
// does not depend on class names surviving a redesign.
type stmpdArchiveRelease struct {
	ID          string `json:"_id"`
	Title       string `json:"title"`
	Artists     string `json:"artists"`
	Version     string `json:"version"`
	ReleaseDate string `json:"releaseDate"`
	ArtworkURL  string `json:"artworkUrl"`
	Slug        struct {
		Current string `json:"current"`
	} `json:"slug"`
	StreamingLinks struct {
		Spotify    string `json:"spotify"`
		AppleMusic string `json:"appleMusic"`
		YouTube    string `json:"youtube"`
	} `json:"streamingLinks"`
}

// nextFlightPayload reassembles the Next.js RSC payload, which the page streams
// as a series of self.__next_f.push([1,"<json-string-chunk>"]) calls. Each chunk
// is a JSON string literal, so decoding and concatenating them yields the
// original text the server sent.
func nextFlightPayload(body string) string {
	const marker = `self.__next_f.push([1,`

	var sb strings.Builder
	for idx := 0; idx < len(body); {
		k := strings.Index(body[idx:], marker)
		if k < 0 {
			break
		}

		p := idx + k + len(marker)
		for p < len(body) && body[p] != '"' && body[p] != ']' {
			p++
		}
		if p >= len(body) || body[p] != '"' {
			idx = p + 1
			continue
		}

		// Walk to the closing quote, stepping over backslash escapes.
		q := p + 1
		for q < len(body) && body[q] != '"' {
			if body[q] == '\\' {
				q++
			}
			q++
		}
		if q >= len(body) {
			break
		}

		var chunk string
		if err := json.Unmarshal([]byte(body[p:q+1]), &chunk); err == nil {
			sb.WriteString(chunk)
		}
		idx = q + 1
	}

	return sb.String()
}

// extractJSONArray returns the JSON array following key, tracking string state so
// that brackets inside titles or URLs do not end the array early.
func extractJSONArray(s, key string) (string, error) {
	k := strings.Index(s, key)
	if k < 0 {
		return "", fmt.Errorf("key %q not found in payload", key)
	}

	rel := strings.Index(s[k:], "[")
	if rel < 0 {
		return "", fmt.Errorf("no array found after key %q", key)
	}
	start := k + rel

	var depth int
	var inStr, esc bool
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// brackets inside strings are data, not structure
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return s[start : i+1], nil
			}
		}
	}

	return "", fmt.Errorf("unterminated array after key %q", key)
}

// upscaleArtwork asks Sanity's CDN for a larger rendition than the 400px one the
// page uses for its grid thumbnails, so Discord embeds are not blurry.
func upscaleArtwork(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}

	q := u.Query()
	if q.Get("w") == "" && q.Get("h") == "" {
		return raw
	}
	q.Set("w", "1000")
	q.Set("h", "1000")
	u.RawQuery = q.Encode()

	return u.String()
}

// fetchStmpdReleases returns the archive's releases, newest first.
func fetchStmpdReleases() ([]utils.StmpdRelease, error) {
	req, err := http.NewRequest("GET", stmpdArchiveURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create stmpd request: %w", err)
	}
	req.Header.Set("User-Agent", "MartinGarrixBot (+https://github.com/milindmadhukar/MartinGarrixBot)")

	resp, err := stmpdClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch stmpd archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stmpd archive returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read stmpd archive body: %w", err)
	}

	payload := nextFlightPayload(string(body))
	if payload == "" {
		return nil, fmt.Errorf("no next.js payload found in stmpd archive")
	}

	rawArray, err := extractJSONArray(payload, `"initialReleases"`)
	if err != nil {
		return nil, fmt.Errorf("failed to locate releases in stmpd payload: %w", err)
	}

	var parsed []stmpdArchiveRelease
	if err := json.Unmarshal([]byte(rawArray), &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode stmpd releases: %w", err)
	}

	releases := make([]utils.StmpdRelease, 0, len(parsed))
	for _, p := range parsed {
		if p.Title == "" {
			continue
		}

		name := p.Title
		if p.Version != "" {
			name = fmt.Sprintf("%s (%s)", p.Title, p.Version)
		}

		release := utils.StmpdRelease{
			Name:          name,
			Artists:       p.Artists,
			Thumbnail:     upscaleArtwork(p.ArtworkURL),
			SpotifyURL:    p.StreamingLinks.Spotify,
			AppleMusicUrl: p.StreamingLinks.AppleMusic,
			YoutubeURL:    p.StreamingLinks.YouTube,
		}

		// releaseDate is a full ISO date, but only the year is kept: the existing
		// rows were written as "<year>-01-01" and DoesSongExist matches on the
		// date, so switching to the exact date would make every stored song look
		// new and re-announce it.
		if len(p.ReleaseDate) >= 4 {
			if year, err := strconv.Atoi(p.ReleaseDate[:4]); err == nil {
				release.ReleaseYear = year
			}
		}

		releases = append(releases, release)
	}

	return releases, nil
}

func GetAllStmpdReleases(b *mgbot.MartinGarrixBot, ticker *time.Ticker) {
	for ; ; <-ticker.C {
		slog.Info("Running STMPD RCRDS releases fetcher")

		releases, err := fetchStmpdReleases()
		if err != nil {
			slog.Error("Failed to fetch STMPD releases", slog.Any("err", err))
			continue
		}

		if len(releases) == 0 {
			slog.Warn("STMPD archive returned no releases - the page layout may have changed again")
			continue
		}

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
		notifier := utils.NewBatchNotifier(b.Queries, b.Client.Rest, utils.NotificationTypeSTMPD)

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

			announcementEmbed := discord.NewEmbed().
				WithTitle(fmt.Sprintf("%s - %s", release.Artists, release.Name)).
				WithImage(release.Thumbnail).
				WithFooter(fmt.Sprintf("Release Year: %d", release.ReleaseYear), "")

			// Prepare the components for this song
			var components []discord.LayoutComponent
			if song.SpotifyUrl.Valid || song.YoutubeUrl.Valid || song.AppleMusicUrl.Valid {
				components = []discord.LayoutComponent{
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
