package utils_test

import (
	"testing"

	"github.com/milindmadhukar/STMPDBot/utils"
)

func TestIsSquareImageURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"square 500", "https://geo-media.beatport.com/image_size/500x500/abc.jpg", true},
		{"square 1400", "https://geo-media.beatport.com/image_size/1400x1400/abc.jpg", true},
		{"not square", "https://geo-media.beatport.com/image_size/500x250/abc.jpg", false},
		{"not square, taller", "https://geo-media.beatport.com/image_size/250x500/abc.jpg", false},
		// The dimensions are the only signal; without them the URL is accepted
		// rather than discarded, so a valid artwork is not dropped by accident.
		{"no dimensions is accepted", "https://geo-media.beatport.com/artwork/abc.jpg", true},
		{"empty string is accepted", "", true},
		{"malformed dimensions are accepted", "https://x/image_size/axb/abc.jpg", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.IsSquareImageURL(tt.url); got != tt.want {
				t.Errorf("IsSquareImageURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestFormatBeatportArtists(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		artists []utils.BeatportArtist
		want    string
	}{
		{"no artists", nil, ""},
		{"empty slice", []utils.BeatportArtist{}, ""},
		{"one artist", []utils.BeatportArtist{{Name: "Martin Garrix"}}, "Martin Garrix"},
		{
			name: "several artists are comma separated",
			artists: []utils.BeatportArtist{
				{Name: "Martin Garrix"}, {Name: "Bono"}, {Name: "The Edge"},
			},
			want: "Martin Garrix, Bono, The Edge",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.FormatBeatportArtists(tt.artists); got != tt.want {
				t.Errorf("FormatBeatportArtists(%v) = %q, want %q", tt.artists, got, tt.want)
			}
		})
	}
}

// This is the exact string findSimilarExistingSong compares against, so its
// separator matters as much as its contents.
func TestFormatBeatportArtists_BuildsTheDedupeKey(t *testing.T) {
	t.Parallel()

	artists := utils.FormatBeatportArtists([]utils.BeatportArtist{
		{Name: "Julian Jordan"}, {Name: "Martin Garrix"},
	})

	const want = "Julian Jordan, Martin Garrix - Kangaroo"
	if got := artists + " - " + "Kangaroo"; got != want {
		t.Errorf("dedupe key = %q, want %q", got, want)
	}
}

func TestFormatBeatportDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ms   int
		want string
	}{
		{"zero", 0, "0:00"},
		{"under a second", 999, "0:00"},
		{"one second", 1000, "0:01"},
		{"seconds are zero padded", 9000, "0:09"},
		{"one minute", 60_000, "1:00"},
		{"typical track length", 203_000, "3:23"},
		{"ten minutes", 600_000, "10:00"},
		// There is no hour rollover: an hour-long mix reads as 60 minutes.
		{"an hour stays in minutes", 3_600_000, "60:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := utils.FormatBeatportDuration(tt.ms); got != tt.want {
				t.Errorf("FormatBeatportDuration(%d) = %q, want %q", tt.ms, got, tt.want)
			}
		})
	}
}

func TestProcessBeatportTrack(t *testing.T) {
	t.Parallel()

	t.Run("maps the API shape onto the internal one", func(t *testing.T) {
		t.Parallel()

		got := utils.ProcessBeatportTrack(utils.BeatportAPITrack{
			ID:          12345,
			Name:        "Animals",
			MixName:     "Original Mix",
			PublishDate: "2013-06-17",
			Artists:     []utils.BeatportArtist{{ID: 1, Name: "Martin Garrix"}},
			BPM:         128,
			LengthMs:    303_000,
			Genre:       utils.BeatportGenre{Name: "Big Room"},
			Release: utils.BeatportRelease{
				Name:  "Animals",
				Image: utils.BeatportImage{URI: "https://x/image_size/500x500/a.jpg"},
			},
		})

		if got.ID != 12345 {
			t.Errorf("ID = %d, want 12345", got.ID)
		}
		if got.Name != "Animals" {
			t.Errorf("Name = %q, want %q", got.Name, "Animals")
		}
		if got.MixName != "Original Mix" {
			t.Errorf("MixName = %q, want %q", got.MixName, "Original Mix")
		}
		// publish_date on the API side becomes release_date internally.
		if got.ReleaseDate != "2013-06-17" {
			t.Errorf("ReleaseDate = %q, want %q", got.ReleaseDate, "2013-06-17")
		}
		if got.BPM != 128 {
			t.Errorf("BPM = %d, want 128", got.BPM)
		}
		if got.LengthMs != 303_000 {
			t.Errorf("LengthMs = %d, want 303000", got.LengthMs)
		}
		if got.ThumbnailURL != "https://x/image_size/500x500/a.jpg" {
			t.Errorf("ThumbnailURL = %q, want the square release image", got.ThumbnailURL)
		}
	})

	t.Run("a non-square thumbnail is dropped", func(t *testing.T) {
		t.Parallel()

		got := utils.ProcessBeatportTrack(utils.BeatportAPITrack{
			Release: utils.BeatportRelease{
				Image: utils.BeatportImage{URI: "https://x/image_size/500x250/a.jpg"},
			},
		})

		if got.ThumbnailURL != "" {
			t.Errorf("ThumbnailURL = %q, want it dropped for a non-square image",
				got.ThumbnailURL)
		}
	})

	t.Run("a missing image stays empty", func(t *testing.T) {
		t.Parallel()

		if got := utils.ProcessBeatportTrack(utils.BeatportAPITrack{}); got.ThumbnailURL != "" {
			t.Errorf("ThumbnailURL = %q, want empty", got.ThumbnailURL)
		}
	})
}
