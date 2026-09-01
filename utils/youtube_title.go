package utils

import (
	"net/url"
	"strings"
)

// decorations are the suffixes uploaders append to a video title. They describe the
// upload, not the recording, so they must come off before the title is compared with
// a catalogue row.
var decorations = []string{
	"official music video", "official lyric video", "official video", "official audio",
	"official visualizer", "official trailer", "official teaser", "music video",
	"lyric video", "lyrics video", "visualizer", "audio only", "out now",
	"official", "lyrics", "audio", "hd", "4k",
}

// ParseYoutubeTitle splits an uploaded video title into the artists and the track.
//
// Uploads are near-universally "Artist - Title (Official Video)", so the hyphen is
// the split and the trailing bracket is noise. A title with no separator is not a
// track upload at all -- channels post announcements, recaps and vlogs to the same
// playlist -- and is reported as such rather than guessed at.
func ParseYoutubeTitle(videoTitle string) (artists, title string, ok bool) {
	cleaned := stripDecorations(videoTitle)

	for _, sep := range []string{" - ", " – ", " — ", " -- "} {
		if i := strings.Index(cleaned, sep); i > 0 {
			artists = strings.TrimSpace(cleaned[:i])
			title = strings.TrimSpace(cleaned[i+len(sep):])
			return artists, title, artists != "" && title != ""
		}
	}
	return "", "", false
}

// stripDecorations removes trailing bracketed groups that only describe the upload.
// A group naming a rendition -- "(Extended Mix)", "(Drove Remix)" -- is kept, because
// that is part of what the recording is.
func stripDecorations(s string) string {
	for {
		trimmed := strings.TrimSpace(s)
		open, close := lastGroup(trimmed)
		if open < 0 {
			return trimmed
		}

		inner := strings.ToLower(strings.TrimSpace(trimmed[open+1 : close]))
		if !isDecoration(inner) {
			return trimmed
		}
		s = trimmed[:open]
	}
}

func isDecoration(inner string) bool {
	for _, d := range decorations {
		if inner == d {
			return true
		}
	}
	return false
}

// NormalizeYoutubeURL rewrites a YouTube link to the canonical watch form and drops
// the tracking parameters share links carry.
//
// The STMPD dataset hands out "youtu.be/<id>?si=<token>" short links, which are
// perfectly good videos -- the radio already recognises them -- but every query and
// report written against "watch?v=" read them as missing. Eighty-six rows looked like
// they had no video and were queued to have one guessed at, when they already had the
// label's own link.
//
// Returns "" when the URL names something other than a single video, such as a
// playlist, so callers can tell "not a video" from "not normalised".
func NormalizeYoutubeURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	var id string
	switch {
	case strings.EqualFold(u.Host, "youtu.be"):
		id = strings.Trim(u.Path, "/")
	case strings.Contains(u.Path, "/watch"):
		id = u.Query().Get("v")
	case strings.HasPrefix(u.Path, "/embed/"):
		id = strings.TrimPrefix(u.Path, "/embed/")
	}

	// A bare playlist or channel link has no video of its own.
	if id == "" || strings.Contains(id, "/") {
		return ""
	}
	return "https://www.youtube.com/watch?v=" + id
}
