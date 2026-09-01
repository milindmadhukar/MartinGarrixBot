package utils

import "strings"

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
