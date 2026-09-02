package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LRCLIB (https://lrclib.net) is a community lyrics database with no API key, no
// account and no quota to register for -- the same shape as the iTunes lookup this
// client is modelled on, and the reason it is reachable from a bot at all.
//
// It exists here because songs.lyrics has only ever been filled in by hand: 79 rows
// out of 1348 have words, and every one of them was pasted into psql. That is the pool
// the quiz has been drawing from since it was written.
const lrclibBaseURL = "https://lrclib.net"

// lrclibRateLimit paces requests.
//
// LRCLIB publishes no hard number. Its documentation asks that clients send requests
// sequentially and leave 200-500ms between them, "especially for batch operations like
// scanning a full music library" -- which is exactly what the first sweep is. The
// slower end of that range costs a few extra minutes on a pass that runs once.
const lrclibRateLimit = 500 * time.Millisecond

// ErrLrclibNotFound means LRCLIB answered and has nothing for this track.
//
// Kept distinct from a transport failure on purpose. Only this one is a fact about the
// row, and only this one should count against the row's retry budget: a timeout says
// something about the network and retiring a song over it would be wrong.
var ErrLrclibNotFound = errors.New("lrclib: track not found")

// LrclibRateLimitedError carries the server's own Retry-After.
//
// The documentation is explicit that a client must honour it and that ignoring it may
// earn a ban, so this is surfaced as its own type rather than folded into a generic
// error: the caller has to be able to stop the batch rather than spend the rest of it
// collecting 429s.
type LrclibRateLimitedError struct {
	RetryAfter time.Duration
}

func (e LrclibRateLimitedError) Error() string {
	return fmt.Sprintf("lrclib: rate limited, retry after %s", e.RetryAfter)
}

// LrclibRecord is one lyrics record.
//
// syncedLyrics is deliberately absent. It is the nicer format, but songs.lyrics is what
// /lyrics renders and what the quiz splits on newlines, and a second column holding the
// same words with timestamps would be a second source of truth with nothing reading it.
type LrclibRecord struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"` // seconds
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  *string `json:"plainLyrics"` // null on an instrumental
}

// Plain returns the lyrics with surrounding blank lines removed, or "" when the record
// carries none.
func (r LrclibRecord) Plain() string {
	if r.PlainLyrics == nil {
		return ""
	}
	return strings.TrimSpace(*r.PlainLyrics)
}

// LengthMs is the record's duration in the units songs.length_ms uses.
func (r LrclibRecord) LengthMs() int32 {
	return int32(r.Duration * 1000)
}

// LrclibClient reads lyrics from LRCLIB, one request at a time.
//
// Not goroutine-safe, for the same reason ItunesClient is not: one watcher or one
// script owns one client, and the pacing is a property of that single caller.
type LrclibClient struct {
	httpClient *http.Client
	baseURL    string
	last       time.Time
	// until is set from a Retry-After. No request goes out before it, so a limit
	// tripped by one call is honoured by every later one rather than only the retry.
	until time.Time
}

func NewLrclibClient() *LrclibClient {
	return NewLrclibClientAt(lrclibBaseURL)
}

// NewLrclibClientAt points the client at a different host. It exists so the tests can
// run against an httptest server: a unit suite that reaches lrclib.net fails whenever
// the network does, and would put this project's traffic through a free service on
// every commit.
func NewLrclibClientAt(baseURL string) *LrclibClient {
	return &LrclibClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
	}
}

// Get asks for the one record that matches a track exactly.
//
// durationSec of 0 omits the parameter. LRCLIB matches duration to within a couple of
// seconds and treats a mismatch as "no such track", so a row with no length_ms must
// send nothing rather than guess -- a wrong duration turns a hit into a 404.
//
// The track name must be the answerable form of the title, not the stored name:
// "Sun Is Never Going Down" is found, "Sun Is Never Going Down (feat. Dawn Golden)" is
// not.
func (c *LrclibClient) Get(ctx context.Context, track, artist, album string, durationSec int) (*LrclibRecord, error) {
	q := url.Values{}
	q.Set("track_name", track)
	q.Set("artist_name", artist)
	if album != "" {
		q.Set("album_name", album)
	}
	if durationSec > 0 && durationSec <= 3600 {
		q.Set("duration", strconv.Itoa(durationSec))
	}

	var rec LrclibRecord
	if err := c.do(ctx, "/api/get", q, &rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// Search is the fallback when Get finds nothing.
//
// It is needed more often than it looks. Get appears to want the artist field to match,
// and this catalogue stores full credit strings: "Breach" by "Martin Garrix" 404s on
// Get while the same pair on Search returns the record, filed under "Martin Garrix;
// Blinders". SplitArtists already treats ";" as a separator, so SameRecording pairs the
// two without help.
//
// Like Apple's search this answers with its nearest guess rather than with nothing, so
// every result has to be verified against the row it is meant to describe.
func (c *LrclibClient) Search(ctx context.Context, track, artist string) ([]LrclibRecord, error) {
	q := url.Values{}
	q.Set("track_name", track)
	if artist != "" {
		q.Set("artist_name", artist)
	}

	var out []LrclibRecord
	if err := c.do(ctx, "/api/search", q, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// overloadedBackoff is how long to wait before the single retry a 503 gets.
//
// LRCLIB answers "the server is busy, please retry in a moment" to roughly one request
// in twenty, and its own message says to retry. Without this, five percent of the
// backlog is skipped on every pass -- not lost, since an un-stamped row comes back, but
// never converging either. One retry and no more: a service that is busy does not want
// to be asked five times.
const overloadedBackoff = 2 * time.Second

func (c *LrclibClient) do(ctx context.Context, path string, q url.Values, out any) error {
	err := c.attempt(ctx, path, q, out)
	if !errors.Is(err, errLrclibOverloaded) {
		return err
	}

	select {
	case <-time.After(overloadedBackoff):
	case <-ctx.Done():
		return ctx.Err()
	}

	if err := c.attempt(ctx, path, q, out); err != nil {
		if errors.Is(err, errLrclibOverloaded) {
			return fmt.Errorf("lrclib is overloaded: %w", err)
		}
		return err
	}
	return nil
}

// errLrclibOverloaded is LRCLIB asking to be tried again shortly. Internal: callers see
// either a successful result or a plain error, never a reason to retry themselves.
var errLrclibOverloaded = errors.New("lrclib: server busy")

func (c *LrclibClient) attempt(ctx context.Context, path string, q url.Values, out any) error {
	if err := c.wait(ctx); err != nil {
		return err
	}
	c.last = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	// LRCLIB asks clients to identify themselves and says so in its documentation.
	// This is the same agent the STMPD and iTunes clients send: it names the project
	// and links to it, which is the shape they ask for.
	req.Header.Set("User-Agent", stmpdUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("lrclib request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrLrclibNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		wait := parseRetryAfter(resp.Header.Get("Retry-After"))
		c.until = time.Now().Add(wait)
		return LrclibRateLimitedError{RetryAfter: wait}
	case resp.StatusCode == http.StatusServiceUnavailable:
		return errLrclibOverloaded
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("lrclib returned status %d: %s", resp.StatusCode, truncate(string(body), 120))
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("failed to decode lrclib response: %w", err)
	}
	return nil
}

// wait honours both the pacing between requests and any Retry-After still in force.
func (c *LrclibClient) wait(ctx context.Context) error {
	deadline := c.last.Add(lrclibRateLimit)
	if c.until.After(deadline) {
		deadline = c.until
	}

	if delay := time.Until(deadline); delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// defaultRetryAfter is used when the server sends a 429 without a usable Retry-After.
// Long enough to be a real back-off rather than a token one.
const defaultRetryAfter = 60 * time.Second

// maxRetryAfter caps what a header can ask for. A cycle context is minutes long, so
// obeying an hour-long back-off inside one would just burn the whole cycle asleep; the
// batch ends instead and the next tick picks up.
const maxRetryAfter = 5 * time.Minute

// parseRetryAfter reads the header in either of the two forms HTTP allows: a count of
// seconds, or an absolute date.
func parseRetryAfter(header string) time.Duration {
	header = strings.TrimSpace(header)
	if header == "" {
		return defaultRetryAfter
	}

	if secs, err := strconv.Atoi(header); err == nil {
		return clampRetryAfter(time.Duration(secs) * time.Second)
	}
	if when, err := http.ParseTime(header); err == nil {
		return clampRetryAfter(time.Until(when))
	}
	return defaultRetryAfter
}

// Written out rather than using min(): this package defines its own min over ints in
// levenshtein.go, which shadows the builtin.
func clampRetryAfter(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultRetryAfter
	}
	if d > maxRetryAfter {
		return maxRetryAfter
	}
	return d
}
