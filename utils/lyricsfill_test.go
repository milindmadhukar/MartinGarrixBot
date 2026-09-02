package utils_test

// The decision this file tests is the one that matters in the whole lyrics feature:
// whether the words that came back belong to the song they are about to be written
// onto. Fetching is easy and reversible; hanging a cover version's lyrics on a real
// song is neither, because nobody would ever notice.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/milindmadhukar/STMPDBot/utils"
)

// lyricsAPI serves /api/get and /api/search from the given records, so a test can say
// what LRCLIB knows without caring how the client asks.
func lyricsAPI(t *testing.T, get *utils.LrclibRecord, search []utils.LrclibRecord) *utils.LrclibClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/get":
			if get == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(get)
		case "/api/search":
			json.NewEncoder(w).Encode(search)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	return utils.NewLrclibClientAt(srv.URL)
}

func lyrics(s string) *string { return &s }

func breachQuery() utils.LyricsQuery {
	return utils.LyricsQuery{
		Title:    "Breach",
		Name:     "Breach (Walk Alone)",
		Artists:  "Martin Garrix & Blinders",
		Album:    "BYLAW EP",
		LengthMs: 178000,
	}
}

func TestFetchLyrics_AcceptsAnExactMatch(t *testing.T) {
	t.Parallel()

	client := lyricsAPI(t, &utils.LrclibRecord{
		ID: 15275538, TrackName: "Breach", ArtistName: "Martin Garrix; Blinders",
		Duration: 178, PlainLyrics: lyrics("You'll never walk alone"),
	}, nil)

	res, err := utils.FetchLyrics(t.Context(), client, breachQuery())
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome != utils.LyricsFound {
		t.Fatalf("outcome = %v, want LyricsFound", res.Outcome)
	}
	if res.Record.ID != 15275538 {
		t.Errorf("took record %d", res.Record.ID)
	}
}

// LRCLIB writes collaborations with semicolons where this catalogue uses "&". The
// artist set has to see through that, or every collaboration is rejected.
func TestFetchLyrics_AcceptsADifferentlyPunctuatedCredit(t *testing.T) {
	t.Parallel()

	client := lyricsAPI(t, nil, []utils.LrclibRecord{{
		ID: 1, TrackName: "Breach", ArtistName: "Blinders; Martin Garrix",
		Duration: 178, PlainLyrics: lyrics("You'll never walk alone"),
	}})

	res, err := utils.FetchLyrics(t.Context(), client, breachQuery())
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome != utils.LyricsFound {
		t.Errorf("outcome = %v, want LyricsFound", res.Outcome)
	}
}

// The duration guard is the only thing separating a song from its own cover, live cut
// or sped-up edit: those share the title and the artist exactly.
func TestFetchLyrics_RejectsADifferentRecordingOfTheSameSong(t *testing.T) {
	t.Parallel()

	cover := utils.LrclibRecord{
		ID: 2, TrackName: "Breach", ArtistName: "Martin Garrix; Blinders",
		Duration:    320, // nearly a minute longer: a live cut, not the single
		PlainLyrics: lyrics("something else entirely"),
	}
	client := lyricsAPI(t, &cover, []utils.LrclibRecord{cover})

	res, err := utils.FetchLyrics(t.Context(), client, breachQuery())
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome != utils.LyricsRejected {
		t.Errorf("outcome = %v, want LyricsRejected", res.Outcome)
	}
}

// A row that does not know its own length cannot use the duration guard, and must not
// invent one -- but the title and artist check still has to hold.
func TestFetchLyrics_RejectsADifferentSong(t *testing.T) {
	t.Parallel()

	q := breachQuery()
	q.LengthMs = 0

	client := lyricsAPI(t, nil, []utils.LrclibRecord{{
		ID: 3, TrackName: "Animals", ArtistName: "Martin Garrix",
		Duration: 178, PlainLyrics: lyrics("wrong song"),
	}})

	res, err := utils.FetchLyrics(t.Context(), client, q)
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome != utils.LyricsMissing {
		t.Errorf("outcome = %v, want LyricsMissing", res.Outcome)
	}
}

// An exact lookup is the only result precise enough to retire a song from the quiz.
func TestFetchLyrics_InstrumentalFromAnExactLookup(t *testing.T) {
	t.Parallel()

	client := lyricsAPI(t, &utils.LrclibRecord{
		ID: 4, TrackName: "Breach", ArtistName: "Martin Garrix; Blinders",
		Duration: 178, Instrumental: true, PlainLyrics: nil,
	}, nil)

	res, err := utils.FetchLyrics(t.Context(), client, breachQuery())
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome != utils.LyricsInstrumental {
		t.Errorf("outcome = %v, want LyricsInstrumental", res.Outcome)
	}
}

// From search it is not. An unverifiable "instrumental" would remove a vocal track
// from the quiz permanently and silently.
func TestFetchLyrics_InstrumentalFromSearchIsNotActedOn(t *testing.T) {
	t.Parallel()

	client := lyricsAPI(t, nil, []utils.LrclibRecord{{
		ID: 5, TrackName: "Breach", ArtistName: "Martin Garrix; Blinders",
		Duration: 178, Instrumental: true, PlainLyrics: nil,
	}})

	res, err := utils.FetchLyrics(t.Context(), client, breachQuery())
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome == utils.LyricsInstrumental {
		t.Error("a search result was allowed to flag a song as instrumental")
	}
	if res.Outcome != utils.LyricsRejected {
		t.Errorf("outcome = %v, want LyricsRejected", res.Outcome)
	}
}

// An exact lookup that answers with the wrong recording must not stop the search:
// that is usually a duration disagreement, not an absence.
func TestFetchLyrics_SearchesOnAfterRejectingAnExactMatch(t *testing.T) {
	t.Parallel()

	client := lyricsAPI(t,
		&utils.LrclibRecord{
			ID: 6, TrackName: "Breach", ArtistName: "Martin Garrix; Blinders",
			Duration: 400, PlainLyrics: lyrics("the extended cut"),
		},
		[]utils.LrclibRecord{{
			ID: 7, TrackName: "Breach", ArtistName: "Martin Garrix; Blinders",
			Duration: 178, PlainLyrics: lyrics("You'll never walk alone"),
		}})

	res, err := utils.FetchLyrics(t.Context(), client, breachQuery())
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome != utils.LyricsFound {
		t.Fatalf("outcome = %v, want LyricsFound", res.Outcome)
	}
	if res.Record.ID != 7 {
		t.Errorf("took record %d, want the one whose duration matches", res.Record.ID)
	}
}

// LRCLIB genuinely having nothing is the only outcome that spends one of a row's four
// retries, so it has to be distinguishable from every other kind of empty-handedness.
func TestFetchLyrics_NothingAtAll(t *testing.T) {
	t.Parallel()

	res, err := utils.FetchLyrics(t.Context(), lyricsAPI(t, nil, nil), breachQuery())
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome != utils.LyricsMissing {
		t.Errorf("outcome = %v, want LyricsMissing", res.Outcome)
	}
}

// A record with no words is not lyrics, however well it matches.
func TestFetchLyrics_EmptyLyricsAreNotAFill(t *testing.T) {
	t.Parallel()

	client := lyricsAPI(t, &utils.LrclibRecord{
		ID: 8, TrackName: "Breach", ArtistName: "Martin Garrix; Blinders",
		Duration: 178, PlainLyrics: lyrics("   \n  "),
	}, nil)

	res, err := utils.FetchLyrics(t.Context(), client, breachQuery())
	if err != nil {
		t.Fatalf("FetchLyrics returned %v", err)
	}
	if res.Outcome == utils.LyricsFound {
		t.Error("whitespace was accepted as lyrics")
	}
}

// A transport failure says nothing about the row and must reach the caller, so that it
// is retried rather than counted against the row.
func TestFetchLyrics_PropagatesATransportError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	if _, err := utils.FetchLyrics(t.Context(), utils.NewLrclibClientAt(srv.URL), breachQuery()); err == nil {
		t.Error("a 500 was swallowed; the row would be marked as a miss")
	}
}
