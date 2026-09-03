package utils_test

// The client is exercised against an httptest server, never against lrclib.net: a unit
// suite that reaches the network fails when the network does, and this one runs on
// every commit. The live shape is checked by hand with scripts/backfill-lyrics.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/milindmadhukar/STMPDBot/utils"
)

// lrclibServer stands in for the API, and asserts the one thing the documentation
// actually requires of a client: that it identifies itself.
func lrclibServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("request carried no User-Agent; LRCLIB requires one")
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

const breachRecord = `{
  "id": 15275538,
  "trackName": "Breach",
  "artistName": "Martin Garrix; Blinders",
  "albumName": "BYLAW EP",
  "duration": 178.0,
  "instrumental": false,
  "plainLyrics": "These days nothing ever feels like home\nYou'll never walk alone\n"
}`

func TestLrclibGet_DecodesARecord(t *testing.T) {
	t.Parallel()

	srv := lrclibServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("track_name"); got != "Breach" {
			t.Errorf("track_name = %q, want %q", got, "Breach")
		}
		if got := r.URL.Query().Get("duration"); got != "178" {
			t.Errorf("duration = %q, want %q", got, "178")
		}
		w.Write([]byte(breachRecord))
	})

	rec, err := utils.NewLrclibClientAt(srv.URL).Get(t.Context(), "Breach", "Martin Garrix", "BYLAW EP", 178)
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if rec.ID != 15275538 || rec.TrackName != "Breach" {
		t.Errorf("decoded %+v", rec)
	}
	if rec.LengthMs() != 178000 {
		t.Errorf("LengthMs = %d, want 178000", rec.LengthMs())
	}
	if !strings.HasSuffix(rec.Plain(), "walk alone") {
		t.Errorf("Plain() should trim the trailing newline, got %q", rec.Plain())
	}
}

// A row with no stored duration must send no duration. LRCLIB matches it to within a
// couple of seconds, so a guessed value turns a hit into a 404.
func TestLrclibGet_OmitsAnUnknownDuration(t *testing.T) {
	t.Parallel()

	srv := lrclibServer(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["duration"]; ok {
			t.Error("duration was sent for a row that does not know its own length")
		}
		w.Write([]byte(breachRecord))
	})

	if _, err := utils.NewLrclibClientAt(srv.URL).Get(t.Context(), "Breach", "Martin Garrix", "", 0); err != nil {
		t.Fatalf("Get returned %v", err)
	}
}

// 404 is an answer, not a failure: LRCLIB has nothing for this track. Only this
// outcome may count against a row's retry budget.
func TestLrclibGet_NotFound(t *testing.T) {
	t.Parallel()

	srv := lrclibServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"code":404,"name":"TrackNotFound","message":"Failed to find specified track"}`))
	})

	_, err := utils.NewLrclibClientAt(srv.URL).Get(t.Context(), "Nothing", "Nobody", "", 0)
	if !errors.Is(err, utils.ErrLrclibNotFound) {
		t.Errorf("Get returned %v, want ErrLrclibNotFound", err)
	}
}

// The documentation is explicit that ignoring Retry-After may earn a ban, so the value
// has to reach the caller intact -- it is what tells a batch to stop.
func TestLrclibGet_RateLimitedCarriesRetryAfter(t *testing.T) {
	t.Parallel()

	srv := lrclibServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"code":429,"name":"TooManyRequests","message":"Rate limit exceeded"}`))
	})

	_, err := utils.NewLrclibClientAt(srv.URL).Get(t.Context(), "Breach", "Martin Garrix", "", 0)

	var limited utils.LrclibRateLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("Get returned %v, want LrclibRateLimitedError", err)
	}
	if limited.RetryAfter != 12*time.Second {
		t.Errorf("RetryAfter = %s, want 12s", limited.RetryAfter)
	}
}

// A malformed or absent Retry-After must still produce a real back-off rather than
// zero, which would be a busy loop against a service asking for quiet.
func TestLrclibGet_RateLimitedWithoutAUsableHeader(t *testing.T) {
	t.Parallel()

	for _, header := range []string{"", "soon", "-5"} {
		srv := lrclibServer(t, func(w http.ResponseWriter, _ *http.Request) {
			if header != "" {
				w.Header().Set("Retry-After", header)
			}
			w.WriteHeader(http.StatusTooManyRequests)
		})

		_, err := utils.NewLrclibClientAt(srv.URL).Get(t.Context(), "Breach", "Martin Garrix", "", 0)

		var limited utils.LrclibRateLimitedError
		if !errors.As(err, &limited) {
			t.Fatalf("Retry-After %q: got %v, want LrclibRateLimitedError", header, err)
		}
		if limited.RetryAfter <= 0 {
			t.Errorf("Retry-After %q produced a back-off of %s", header, limited.RetryAfter)
		}
	}
}

// LRCLIB answers "server busy" to roughly one request in twenty. One retry converts
// that from a permanently skipped row into a filled one.
func TestLrclibGet_RetriesOnceWhenOverloaded(t *testing.T) {
	t.Parallel()

	var calls int
	srv := lrclibServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"name":"ServerOverloaded"}`))
			return
		}
		w.Write([]byte(breachRecord))
	})

	rec, err := utils.NewLrclibClientAt(srv.URL).Get(t.Context(), "Breach", "Martin Garrix", "", 0)
	if err != nil {
		t.Fatalf("Get returned %v after a retryable 503", err)
	}
	if rec.TrackName != "Breach" {
		t.Errorf("decoded %+v", rec)
	}
	if calls != 2 {
		t.Errorf("made %d requests, want exactly 2", calls)
	}
}

// And no more than once: a service that is busy does not want to be asked five times.
func TestLrclibGet_GivesUpAfterASecondOverload(t *testing.T) {
	t.Parallel()

	var calls int
	srv := lrclibServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	if _, err := utils.NewLrclibClientAt(srv.URL).Get(t.Context(), "Breach", "Martin Garrix", "", 0); err == nil {
		t.Fatal("Get should have failed after two 503s")
	}
	if calls != 2 {
		t.Errorf("made %d requests, want exactly 2", calls)
	}
}

func TestLrclibSearch_DecodesAList(t *testing.T) {
	t.Parallel()

	srv := lrclibServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/api/search" {
			t.Errorf("path = %q, want /api/search", got)
		}
		w.Write([]byte("[" + breachRecord + "]"))
	})

	got, err := utils.NewLrclibClientAt(srv.URL).Search(t.Context(), "Breach", "Martin Garrix")
	if err != nil {
		t.Fatalf("Search returned %v", err)
	}
	if len(got) != 1 || got[0].ArtistName != "Martin Garrix; Blinders" {
		t.Errorf("decoded %+v", got)
	}
}

// An instrumental record carries a null plainLyrics, which must decode as absent
// rather than crashing on a nil dereference.
func TestLrclibRecord_NullLyrics(t *testing.T) {
	t.Parallel()

	srv := lrclibServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"id":1,"trackName":"Pizza","artistName":"Martin Garrix",
			"duration":180,"instrumental":true,"plainLyrics":null,"syncedLyrics":null}`))
	})

	rec, err := utils.NewLrclibClientAt(srv.URL).Get(t.Context(), "Pizza", "Martin Garrix", "", 0)
	if err != nil {
		t.Fatalf("Get returned %v", err)
	}
	if !rec.Instrumental {
		t.Error("instrumental flag was lost")
	}
	if rec.Plain() != "" {
		t.Errorf("Plain() = %q, want empty", rec.Plain())
	}
}

// The pacing must be interruptible. A cycle whose context expires has to return
// promptly rather than sleeping out its delay.
func TestLrclibClient_HonoursContextWhilePacing(t *testing.T) {
	t.Parallel()

	srv := lrclibServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(breachRecord))
	})

	client := utils.NewLrclibClientAt(srv.URL)
	if _, err := client.Get(t.Context(), "Breach", "Martin Garrix", "", 0); err != nil {
		t.Fatalf("first Get returned %v", err)
	}

	// The second call has to wait out the pacing delay, and this context will not
	// survive that long.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := client.Get(ctx, "Breach", "Martin Garrix", "", 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Get returned %v, want a context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Errorf("Get slept %s past its context", elapsed)
	}
}
