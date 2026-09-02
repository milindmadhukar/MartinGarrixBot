//go:build livefetch

package handlers

// Excluded from `go test ./...` and from CI: this reaches the real
// martingarrix.com and stmpdrcrds.com. Run it deliberately:
//
//	go test -tags livefetch -count=1 -v ./stmpdbot/handlers/
//
// The fixture tests in this package prove the parsers still handle the page
// shape captured in testdata. This proves that shape is still what the sites
// serve — which is the failure the fixtures cannot catch, and the one that has
// actually happened: both fetchers have silently stopped returning anything
// after an upstream rebuild (1f21cc7, 445be35).
//
// A failure here means the parsers need updating and the fixtures re-capturing,
// not that the code regressed.

import (
	"testing"
)

func TestLiveTourPage(t *testing.T) {
	shows, err := fetchTourShows()
	if err != nil {
		t.Fatalf("fetchTourShows failed against the live page: %v", err)
	}

	if len(shows) == 0 {
		t.Fatal("the live tour page yielded no shows; either the tour is over " +
			"or the payload shape changed and parseTourShows needs updating")
	}

	t.Logf("parsed %d shows from the live page", len(shows))

	for _, show := range shows {
		if show.ShowName == "" {
			t.Errorf("a show came back with no name: %+v", show)
		}
		if show.ShowDate.IsZero() {
			t.Errorf("show %q came back with no date", show.ShowName)
		}
		if len(show.TicketURL) > discordMaxButtonURL {
			t.Errorf("show %q has a %d character ticket URL, over Discord's %d "+
				"limit; sanitizeTicketURL needs a new parameter added to it",
				show.ShowName, len(show.TicketURL), discordMaxButtonURL)
		}
	}
}

func TestLiveStmpdArchive(t *testing.T) {
	releases, err := fetchStmpdReleases()
	if err != nil {
		t.Fatalf("fetchStmpdReleases failed against the live archive: %v", err)
	}

	if len(releases) == 0 {
		t.Fatal("the live archive yielded no releases; the payload shape likely " +
			"changed and parseStmpdReleases needs updating")
	}

	t.Logf("parsed %d releases from the live archive", len(releases))

	for _, release := range releases {
		if release.Name == "" {
			t.Errorf("a release came back with no name: %+v", release)
		}
	}
}
