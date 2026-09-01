package utils

import "testing"

func TestParseYoutubeTitle(t *testing.T) {
	tests := []struct {
		video          string
		artists, title string
		ok             bool
	}{
		{"Martin Garrix & Bebe Rexha - In The Name Of Love (Official Video)",
			"Martin Garrix & Bebe Rexha", "In The Name Of Love", true},
		{"AREA21 - Spaceships (Live on Planet Earth)",
			"AREA21", "Spaceships (Live on Planet Earth)", true},
		{"Martin Garrix - Animals (Official Music Video)",
			"Martin Garrix", "Animals", true},
		{"Julian Jordan - The Bass [Official Audio]",
			"Julian Jordan", "The Bass", true},
		// Channels post things that are not tracks to the same uploads playlist.
		{"THANK YOU CREAMFIELDS 🇬🇧❤️", "", "", false},
		{"for everyone who's been part of these 10 years", "", "", false},
	}

	for _, tt := range tests {
		a, ti, ok := ParseYoutubeTitle(tt.video)
		if ok != tt.ok || a != tt.artists || ti != tt.title {
			t.Errorf("ParseYoutubeTitle(%q) = (%q, %q, %v); want (%q, %q, %v)",
				tt.video, a, ti, ok, tt.artists, tt.title, tt.ok)
		}
	}
}

func TestParseYoutubeTitleKeepsRenditions(t *testing.T) {
	// A rendition is part of what the recording is, unlike "(Official Video)".
	_, title, ok := ParseYoutubeTitle("Martin Garrix - Catharina (Extended Mix) (Official Audio)")
	if !ok || title != "Catharina (Extended Mix)" {
		t.Errorf("got %q, want %q", title, "Catharina (Extended Mix)")
	}
}

func TestNormalizeYoutubeURL(t *testing.T) {
	tests := []struct{ in, want string }{
		// The shape the STMPD dataset actually hands out.
		{"https://youtu.be/XkI-hwuRprU?si=7CBvwdkBRa0ylfuA", "https://www.youtube.com/watch?v=XkI-hwuRprU"},
		{"https://www.youtube.com/watch?v=gcCZKHi43AU", "https://www.youtube.com/watch?v=gcCZKHi43AU"},
		// Tracking parameters are noise.
		{"https://www.youtube.com/watch?v=abc123&si=xyz&t=30", "https://www.youtube.com/watch?v=abc123"},
		{"https://www.youtube.com/embed/abc123", "https://www.youtube.com/watch?v=abc123"},
		// Not a single video.
		{"https://www.youtube.com/playlist?list=PLxxx", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeYoutubeURL(tt.in); got != tt.want {
			t.Errorf("NormalizeYoutubeURL(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestAppleURLNamesThisRelease(t *testing.T) {
	// The row IS the EP: the slug reduces to its name.
	if !AppleURLNamesThisRelease("Seven", "https://music.apple.com/ca/album/seven-ep/1168670107") {
		t.Error("Seven is an EP and its Apple slug says so")
	}
	if !AppleURLNamesThisRelease("The Street", "https://music.apple.com/nl/album/the-street-ep/123") {
		t.Error("The Street is an EP")
	}
	// The row is a TRACK on an EP. The slug says "-ep" but names a different release.
	if AppleURLNamesThisRelease("Mind The Grind", "https://music.apple.com/nl/album/bombai-ep/456") {
		t.Error("a track on the Bombai EP is not itself a release")
	}
	// An ordinary single.
	if AppleURLNamesThisRelease("Animals", "https://music.apple.com/nl/album/animals-single/789") {
		t.Error("a single is not a multi-track release")
	}
}
