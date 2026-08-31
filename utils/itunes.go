package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Apple's iTunes Search API is public: no key, no account, no quota to register
// for. It is used here only to resolve release dates, by looking up the numeric id
// that is already embedded in a song's stored apple_music_url -- so there is no
// searching and no fuzzy matching involved, and the answer cannot be a different
// song than the one the row already links to.
const itunesLookupURL = "https://itunes.apple.com/lookup"

// itunesRateLimit paces requests. Apple does not publish a limit for the lookup
// endpoint but is widely reported to throttle around 20 requests per minute, and
// this is a maintenance pass with no deadline.
const itunesRateLimit = 3 * time.Second

// appleIDPattern pulls the ids out of an Apple Music or iTunes URL. Both shapes are
// present in the database:
//
//	https://itunes.apple.com/us/album/hold-on-believe-single/id1165468984
//	https://music.apple.com/nl/album/so-far-away/1315767709?i=1315768443
//
// The "i" query parameter, when present, is the individual track; the trailing path
// segment is the album. The track is preferred: an album's release date is the date
// of the collection, which for a compilation is not the song's own release.
var appleIDPattern = regexp.MustCompile(`/(?:id)?(\d{6,})(?:\?|$|/)`)

// AppleIDFromURL returns the best id to look up for an Apple Music URL.
func AppleIDFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	if u, err := url.Parse(rawURL); err == nil {
		if track := u.Query().Get("i"); track != "" {
			return track
		}
	}

	// Strip the query before matching so a parameter cannot be mistaken for the id.
	path := rawURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimRight(path, "/") + "/"

	if m := appleIDPattern.FindStringSubmatch(path); len(m) > 1 {
		return m[1]
	}
	return ""
}

// IsApplePlaylistURL reports whether a URL points at a playlist rather than a release.
//
// 27 rows share a single playlist link in place of a real album or track URL. A
// playlist has no release date, and its id is not something the lookup endpoint
// understands, so these must be reported as bad links rather than silently counted
// among the ones with no Apple link at all.
func IsApplePlaylistURL(rawURL string) bool {
	return strings.Contains(rawURL, "/playlist/") || strings.Contains(rawURL, "pl.")
}

// ItunesResult is one entry from a lookup response.
type ItunesResult struct {
	WrapperType    string `json:"wrapperType"`
	ArtistName     string `json:"artistName"`
	TrackName      string `json:"trackName"`
	CollectionName string `json:"collectionName"`
	ReleaseDate    string `json:"releaseDate"`
}

// Title returns whichever name the entry carries.
func (r ItunesResult) Title() string {
	if r.TrackName != "" {
		return r.TrackName
	}
	return r.CollectionName
}

// Date returns the release date as a plain ISO day, or "" if absent or malformed.
func (r ItunesResult) Date() string {
	if len(r.ReleaseDate) < 10 {
		return ""
	}
	day := r.ReleaseDate[:10]
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return ""
	}
	return day
}

// ItunesClient looks up release metadata by id.
type ItunesClient struct {
	httpClient *http.Client
	last       time.Time
}

func NewItunesClient() *ItunesClient {
	return &ItunesClient{httpClient: &http.Client{Timeout: 30 * time.Second}}
}

// Lookup returns the entry for an id, or nil when Apple knows nothing about it.
func (c *ItunesClient) Lookup(ctx context.Context, id string) (*ItunesResult, error) {
	if wait := itunesRateLimit - time.Since(c.last); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c.last = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s?id=%s", itunesLookupURL, url.QueryEscape(id)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", stmpdUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itunes lookup failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("itunes returned status %d: %s", resp.StatusCode, truncate(string(body), 120))
	}

	var parsed struct {
		ResultCount int            `json:"resultCount"`
		Results     []ItunesResult `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode itunes response: %w", err)
	}
	if parsed.ResultCount == 0 || len(parsed.Results) == 0 {
		return nil, nil
	}
	return &parsed.Results[0], nil
}
