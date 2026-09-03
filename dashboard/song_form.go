package dashboard

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

// linkHosts is the host each streaming column is expected to point at.
//
// The check is not pedantry: these URLs become buttons under an announcement in a
// public server, so a typo that puts a Spotify link in the YouTube column ships a
// button that lies about where it goes. Suffix matching, so regional and short forms
// (open.spotify.com, youtu.be, music.apple.com) all pass.
var linkHosts = map[string][]string{
	"spotify_url":       {"spotify.com"},
	"apple_music_url":   {"apple.com"},
	"youtube_url":       {"youtube.com", "youtu.be"},
	"youtube_music_url": {"youtube.com", "youtu.be"},
	"deezer_url":        {"deezer.com", "dzr.page.link"},
	"tidal_url":         {"tidal.com"},
	"amazon_music_url":  {"amazon.com", "amazon.co.uk", "music.amazon.com", "amzn.to", "amzn.eu"},
	"beatport_url":      {"beatport.com"},
}

// buildSongUpdate turns a submitted form into DashUpdateSongParams, validating it and
// computing the new lock set.
//
// A field absent from the form keeps its current value; a field present but empty is
// cleared. That is the same contract buildUpdate uses for guild settings, and it is what
// lets the page save one group at a time while still being able to unset a value.
//
// Every field whose value actually changes is added to locked_fields, because a hand
// correction is the most authoritative value the row will ever hold and the four
// automated writers would otherwise overwrite it on their next cycle. Setting a field to
// what it already says is not a correction and does not lock it.
func buildSongUpdate(song db.Song, form url.Values) (db.DashUpdateSongParams, []string, []string) {
	var problems []string
	locked := map[string]bool{}
	for _, f := range song.LockedFields {
		locked[f] = true
	}
	var changed []string

	// note records a field the submission actually altered.
	note := func(field string) {
		changed = append(changed, field)
		locked[field] = true
	}

	// text resolves a nullable text column.
	text := func(key string, current pgtype.Text) pgtype.Text {
		raw, present := form[key]
		if !present {
			return current
		}
		v := strings.TrimSpace(raw[0])
		next := pgtype.Text{String: v, Valid: v != ""}
		if next.String != current.String || next.Valid != current.Valid {
			note(key)
		}
		return next
	}

	// required resolves a NOT NULL text column.
	required := func(key, label, current string) string {
		raw, present := form[key]
		if !present {
			return current
		}
		v := strings.TrimSpace(raw[0])
		if v == "" {
			problems = append(problems, label+" cannot be empty.")
			return current
		}
		if v != current {
			note(key)
		}
		return v
	}

	// link resolves a streaming URL, checking that it parses and names the right host.
	link := func(key, label string, current pgtype.Text) pgtype.Text {
		raw, present := form[key]
		if !present {
			return current
		}
		v := strings.TrimSpace(raw[0])
		if v != "" {
			u, err := url.Parse(v)
			switch {
			case err != nil || u.Host == "":
				problems = append(problems, label+" is not a valid URL.")
				return current
			case u.Scheme != "https":
				problems = append(problems, label+" must be an https:// URL.")
				return current
			}
			if hosts := linkHosts[key]; len(hosts) > 0 && !hostMatches(u.Host, hosts) {
				problems = append(problems, fmt.Sprintf(
					"%s points at %s, which is not a %s address.", label, u.Host, label))
				return current
			}
		}
		next := pgtype.Text{String: v, Valid: v != ""}
		if next.String != current.String || next.Valid != current.Valid {
			note(key)
		}
		return next
	}

	// flag resolves a checkbox. A checkbox that is off is simply absent from the form,
	// so its group has to submit a hidden marker saying it was on the page at all --
	// otherwise "unticked" and "not shown" are the same submission and a flag could
	// never be cleared.
	flag := func(key string, current bool) bool {
		if form.Get("present:"+key) == "" {
			return current
		}
		next := form.Get(key) != ""
		if next != current {
			note(key)
		}
		return next
	}

	params := db.DashUpdateSongParams{
		ID:              song.ID,
		Name:            required("name", "Name", song.Name),
		Artists:         required("artists", "Artists", song.Artists),
		MixName:         text("mix_name", song.MixName),
		ThumbnailUrl:    link("thumbnail_url", "Artwork URL", song.ThumbnailUrl),
		SpotifyUrl:      link("spotify_url", "Spotify", song.SpotifyUrl),
		AppleMusicUrl:   link("apple_music_url", "Apple Music", song.AppleMusicUrl),
		YoutubeUrl:      link("youtube_url", "YouTube", song.YoutubeUrl),
		YoutubeMusicUrl: link("youtube_music_url", "YouTube Music", song.YoutubeMusicUrl),
		DeezerUrl:       link("deezer_url", "Deezer", song.DeezerUrl),
		TidalUrl:        link("tidal_url", "Tidal", song.TidalUrl),
		AmazonMusicUrl:  link("amazon_music_url", "Amazon Music", song.AmazonMusicUrl),
		BeatportUrl:     link("beatport_url", "Beatport", song.BeatportUrl),
		ReleaseName:     text("release_name", song.ReleaseName),
		Lyrics:          text("lyrics", song.Lyrics),
		Genre:           text("genre", song.Genre),
		SubGenre:        text("sub_genre", song.SubGenre),
		MusicalKey:      text("musical_key", song.MusicalKey),
		IsCollection:    flag("is_collection", song.IsCollection),
		IsInstrumental:  flag("is_instrumental", song.IsInstrumental),
		IsUnreleased:    flag("is_unreleased", song.IsUnreleased),
	}

	// The artwork URL is the one link with no fixed host: covers come from four CDNs
	// today and a corrected one may come from anywhere. Only the scheme is checked, and
	// linkHosts has no entry for it, so the host test above is skipped.

	params.ReleaseDate = song.ReleaseDate
	if raw, present := form["release_date"]; present {
		v := strings.TrimSpace(raw[0])
		switch v {
		case "":
			if song.ReleaseDate.Valid {
				note("release_date")
			}
			params.ReleaseDate = pgtype.Text{}
		default:
			if _, err := time.Parse("2006-01-02", v); err != nil {
				problems = append(problems, "Release date must be written YYYY-MM-DD.")
			} else {
				next := pgtype.Text{String: v, Valid: true}
				if next.String != song.ReleaseDate.String || !song.ReleaseDate.Valid {
					note("release_date")
				}
				params.ReleaseDate = next
			}
		}
	}

	params.Bpm = song.Bpm
	if raw, present := form["bpm"]; present {
		v := strings.TrimSpace(raw[0])
		switch v {
		case "":
			if song.Bpm.Valid {
				note("bpm")
			}
			params.Bpm = pgtype.Int4{}
		default:
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 400 {
				problems = append(problems, "BPM must be a whole number between 1 and 400.")
			} else {
				next := pgtype.Int4{Int32: int32(n), Valid: true}
				if next.Int32 != song.Bpm.Int32 || !song.Bpm.Valid {
					note("bpm")
				}
				params.Bpm = next
			}
		}
	}

	// The database has this as a CHECK constraint (unreleased_has_no_date, 000013), so
	// without catching it here the save would come back as a 500 rather than as a
	// sentence explaining what is wrong.
	if params.IsUnreleased && params.ReleaseDate.Valid {
		problems = append(problems,
			"A song marked unreleased cannot carry a release date. Clear one or the other.")
	}

	params.LockedFields = sortedKeys(locked)
	return params, changed, problems
}

// hostMatches reports whether host is one of the allowed hosts or a subdomain of one.
func hostMatches(host string, allowed []string) bool {
	host = strings.ToLower(host)
	for _, a := range allowed {
		if host == a || strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		// Only fields that are actually lockable are ever stored, so a column renamed
		// out of existence does not linger in the set forever.
		if v && catalogue.IsLockable(k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
