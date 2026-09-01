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
