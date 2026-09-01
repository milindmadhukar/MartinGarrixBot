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
