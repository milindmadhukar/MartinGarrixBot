package handlers

// Covers the normalisation halves of the two fetchers, split out of
// fetchTourShows and fetchStmpdReleases so they can run against the captured
// pages in testdata rather than the live sites.

import (
	"strings"
	"testing"
	"time"
)

func TestParseTourShows(t *testing.T) {
	t.Parallel()

	shows, err := parseTourShows([]byte(readFixture(t, "tour_next_data.html")))
	if err != nil {
		t.Fatalf("parseTourShows returned an error: %v", err)
	}

	// Six shows in the fixture: one unannounced and one with an unparseable
	// date are both dropped.
	if len(shows) != 4 {
		t.Fatalf("got %d shows, want 4:\n%+v", len(shows), shows)
	}

	t.Run("shows are ordered by date", func(t *testing.T) {
		for i := 1; i < len(shows); i++ {
			if shows[i].ShowDate.Before(shows[i-1].ShowDate) {
				t.Errorf("show %d (%s) sorts before show %d (%s)",
					i, shows[i].ShowDate, i-1, shows[i-1].ShowDate)
			}
		}
	})

	t.Run("unannounced shows are withheld", func(t *testing.T) {
		for _, show := range shows {
			if show.ShowName == "Secret Show" {
				t.Error("an unannounced show was included; that leaks a date " +
					"before the artist announces it")
			}
		}
	})

	t.Run("shows with an unparseable date are skipped", func(t *testing.T) {
		for _, show := range shows {
			if show.ShowName == "Broken Date Show" {
				t.Error("a show with an unparseable date was included")
			}
		}
	})

	byName := make(map[string]int, len(shows))
	for i, show := range shows {
		byName[show.ShowName] = i
	}

	t.Run("a fully populated show", func(t *testing.T) {
		i, ok := byName["Tomorrowland"]
		if !ok {
			t.Fatalf("Tomorrowland is missing from %+v", shows)
		}
		show := shows[i]

		if show.City != "Boom" {
			t.Errorf("city = %q, want %q", show.City, "Boom")
		}
		if show.Country != "Belgium" {
			t.Errorf("country = %q, want %q", show.Country, "Belgium")
		}
		if show.Venue != "Main Stage" {
			t.Errorf("venue = %q, want %q", show.Venue, "Main Stage")
		}
		if want := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC); !show.ShowDate.Equal(want) {
			t.Errorf("date = %s, want %s", show.ShowDate, want)
		}
		// The analytics parameters must be gone; without that these run past
		// Discord's button URL limit and reject the whole message.
		if strings.Contains(show.TicketURL, "_gl") ||
			strings.Contains(show.TicketURL, "FPAU") ||
			strings.Contains(show.TicketURL, "_gcl_au") {
			t.Errorf("ticket URL still carries analytics parameters: %q", show.TicketURL)
		}
		if !strings.Contains(show.TicketURL, "utm_source=site") {
			t.Errorf("ticket URL lost a non-analytics parameter: %q", show.TicketURL)
		}
	})

	t.Run("a title split across rich-text blocks is joined", func(t *testing.T) {
		if _, ok := byName["Ushuaia Ibiza Residency"]; !ok {
			t.Errorf("expected the two title blocks to be joined; got %v", shows)
		}
	})

	t.Run("a location with no comma falls back to TBA", func(t *testing.T) {
		i, ok := byName["Singapore Grand Prix"]
		if !ok {
			t.Fatalf("Singapore Grand Prix is missing from %+v", shows)
		}

		if shows[i].City != "Singapore" {
			t.Errorf("city = %q, want %q", shows[i].City, "Singapore")
		}
		if shows[i].Country != "TBA" {
			t.Errorf("country = %q, want %q", shows[i].Country, "TBA")
		}
	})

	t.Run("a missing venue falls back", func(t *testing.T) {
		i := byName["Singapore Grand Prix"]
		if shows[i].Venue != "Venue TBA" {
			t.Errorf("venue = %q, want %q", shows[i].Venue, "Venue TBA")
		}
	})

	// Only the first comma splits, so a country whose name contains one stays
	// intact.
	t.Run("only the first comma splits city from country", func(t *testing.T) {
		i, ok := byName["Ultra Music Festival"]
		if !ok {
			t.Fatalf("Ultra Music Festival is missing from %+v", shows)
		}

		if shows[i].City != "Miami" {
			t.Errorf("city = %q, want %q", shows[i].City, "Miami")
		}
		if shows[i].Country != "United States of America" {
			t.Errorf("country = %q, want %q", shows[i].Country, "United States of America")
		}
	})
}

func TestParseTourShows_Errors(t *testing.T) {
	t.Parallel()

	t.Run("a page with no data script", func(t *testing.T) {
		t.Parallel()

		_, err := parseTourShows([]byte(readFixture(t, "tour_next_data_missing.html")))
		if err == nil {
			t.Fatal("expected an error when the page carries no tour data")
		}
		if !strings.Contains(err.Error(), "failed to locate tour data") {
			t.Errorf("error = %q, want it to mention locating the data", err)
		}
	})

	t.Run("a payload that is not JSON", func(t *testing.T) {
		t.Parallel()

		_, err := parseTourShows([]byte(`<script id="__NEXT_DATA__">not json</script>`))
		if err == nil {
			t.Fatal("expected an error for a non-JSON payload")
		}
		if !strings.Contains(err.Error(), "failed to decode tour data") {
			t.Errorf("error = %q, want it to mention decoding", err)
		}
	})

	t.Run("a valid page with no shows", func(t *testing.T) {
		t.Parallel()

		shows, err := parseTourShows([]byte(
			`<script id="__NEXT_DATA__">{"props":{"pageProps":{"toursData":[]}}}</script>`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(shows) != 0 {
			t.Errorf("got %d shows, want none", len(shows))
		}
	})
}

func TestParseStmpdReleases(t *testing.T) {
	t.Parallel()

	releases, err := parseStmpdReleases([]byte(readFixture(t, "stmpd_flight.html")))
	if err != nil {
		t.Fatalf("parseStmpdReleases returned an error: %v", err)
	}

	// Four releases in the fixture; the one with an empty title is dropped.
	if len(releases) != 3 {
		t.Fatalf("got %d releases, want 3:\n%+v", len(releases), releases)
	}

	t.Run("a version is folded into the name", func(t *testing.T) {
		if releases[0].Name != "Kangaroo (Extended Mix)" {
			t.Errorf("name = %q, want %q", releases[0].Name, "Kangaroo (Extended Mix)")
		}
	})

	t.Run("a release with no version keeps its title", func(t *testing.T) {
		if releases[1].Name != "Us [Reimagined]" {
			t.Errorf("name = %q, want %q", releases[1].Name, "Us [Reimagined]")
		}
	})

	t.Run("releases with no title are skipped", func(t *testing.T) {
		for _, release := range releases {
			if strings.HasPrefix(release.Name, "(") || release.Name == "" {
				t.Errorf("a release with no title was included: %+v", release)
			}
		}
	})

	// Only the year is kept: existing rows were written as "<year>-01-01" and
	// DoesSongExist matches on that date, so storing the exact date would make
	// every stored song look new and re-announce it.
	t.Run("only the year is taken from the release date", func(t *testing.T) {
		if releases[0].ReleaseYear != 2026 {
			t.Errorf("release year = %d, want 2026", releases[0].ReleaseYear)
		}
	})

	t.Run("a missing release date leaves the year unset", func(t *testing.T) {
		if releases[2].ReleaseYear != 0 {
			t.Errorf("release year = %d, want 0 for a release with no date",
				releases[2].ReleaseYear)
		}
	})

	t.Run("artwork with sizing parameters is upscaled", func(t *testing.T) {
		if !strings.Contains(releases[0].Thumbnail, "w=1000") ||
			!strings.Contains(releases[0].Thumbnail, "h=1000") {
			t.Errorf("thumbnail = %q, want it upscaled to 1000x1000", releases[0].Thumbnail)
		}
	})

	t.Run("artwork with no sizing parameters is left alone", func(t *testing.T) {
		if releases[1].Thumbnail != "https://cdn.sanity.io/images/x/prod/us.jpg" {
			t.Errorf("thumbnail = %q, want it untouched", releases[1].Thumbnail)
		}
	})

	t.Run("streaming links are carried through", func(t *testing.T) {
		if releases[0].SpotifyURL != "https://open.spotify.com/track/kangaroo" {
			t.Errorf("spotify = %q, want the fixture value", releases[0].SpotifyURL)
		}
		if releases[0].YoutubeURL != "https://youtu.be/kangaroo" {
			t.Errorf("youtube = %q, want the fixture value", releases[0].YoutubeURL)
		}
		if releases[1].AppleMusicUrl != "" {
			t.Errorf("apple music = %q, want it empty", releases[1].AppleMusicUrl)
		}
	})

	t.Run("artists are carried through", func(t *testing.T) {
		if releases[0].Artists != "Julian Jordan" {
			t.Errorf("artists = %q, want %q", releases[0].Artists, "Julian Jordan")
		}
	})
}

func TestParseStmpdReleases_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		body            string
		wantErrContains string
	}{
		{
			name:            "no flight payload at all",
			body:            `<html><body>nothing</body></html>`,
			wantErrContains: "no next.js payload found",
		},
		{
			name:            "a payload with no releases key",
			body:            `<script>self.__next_f.push([1,"{\"other\":[]}"])</script>`,
			wantErrContains: "failed to locate releases",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := parseStmpdReleases([]byte(tt.body))
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", tt.wantErrContains)
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErrContains)
			}
		})
	}
}
