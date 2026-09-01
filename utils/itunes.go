package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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

// appleSlugPattern pulls the human-readable slug out of an Apple album URL:
// https://music.apple.com/ca/album/seven-ep/1168670107 -> "seven-ep".
var appleSlugPattern = regexp.MustCompile(`/album/([^/]+)/`)

// releaseSlugSuffixes are how Apple's own URLs label the kind of release.
var releaseSlugSuffixes = []string{"-ep", "-album", "-lp"}

// AppleURLNamesThisRelease reports whether an Apple link points at a multi-track
// release that the row is named after -- meaning the row is the release, not a track.
//
// The suffix alone is not enough. "Mind The Grind" links to /album/bombai-ep/ because
// it is a track on the Bombai EP; the row is a song. Only when the slug reduces to the
// row's own name is the row the release itself.
//
// This is deliberately offline. The same question can be answered by asking Apple for
// the release's track count, but that is one rate-limited request per row and nearly
// an hour for the catalogue, to learn something the URL already says.
func AppleURLNamesThisRelease(name, appleURL string) bool {
	if appleURL == "" {
		return false
	}

	m := appleSlugPattern.FindStringSubmatch(appleURL)
	if len(m) < 2 {
		return false
	}

	slug := strings.ToLower(m[1])
	trimmed := ""
	for _, suffix := range releaseSlugSuffixes {
		if strings.HasSuffix(slug, suffix) {
			trimmed = strings.TrimSuffix(slug, suffix)
			break
		}
	}
	if trimmed == "" {
		return false
	}

	return NormalizeToken(trimmed) == NormalizeToken(name)
}

// AppleAlbumIDFromURL returns the id of the release an Apple URL points at, ignoring
// any track within it. Asking about the release is what tells an EP from a single.
func AppleAlbumIDFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

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
	ArtworkURL100  string `json:"artworkUrl100"`
	CollectionType string `json:"collectionType"`
	TrackCount     int    `json:"trackCount"`
}

// collectionSuffixes are how Apple labels the kind of release in its title.
var collectionSuffixes = []string{" - EP", " - Album", " - Single"}

// CollectionTitle strips Apple's "- EP" / "- Single" label, leaving the release name
// as it would be written elsewhere.
func (r ItunesResult) CollectionTitle() string {
	name := r.CollectionName
	for _, suffix := range collectionSuffixes {
		name = strings.TrimSuffix(name, suffix)
	}
	return strings.TrimSpace(name)
}

// IsMultiTrackRelease reports whether Apple describes this entry as a release holding
// several tracks, rather than a single.
//
// The count alone is not enough: a track on an eight-track album would qualify. The
// caller has to check that the row is named after the release itself.
func (r ItunesResult) IsMultiTrackRelease() bool {
	return r.TrackCount >= 4 || strings.HasSuffix(r.CollectionName, " - EP") ||
		strings.HasSuffix(r.CollectionName, " - Album")
}

// artworkSize is what the thumbnail is rewritten to. Apple returns a 100px URL and
// serves any size from the same path, so the dimensions are simply substituted.
const artworkSize = "1000x1000bb"

// Artwork returns the cover at a size worth putting in a Discord embed, or "" when
// Apple has none.
func (r ItunesResult) Artwork() string {
	if r.ArtworkURL100 == "" {
		return ""
	}
	return strings.Replace(r.ArtworkURL100, "100x100bb", artworkSize, 1)
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

const itunesSearchURL = "https://itunes.apple.com/search"

// Search returns song results for a free-text query, best match first.
//
// Unlike Lookup this is a guess: Apple returns its nearest match even when it has
// nothing for the query at all. Searching "AREA21 Drinks Up" comes back with
// "AREA21 - Glad You Came". Every result must therefore be verified against the row
// it is meant to describe before its date is believed.
func (c *ItunesClient) Search(ctx context.Context, term string, limit int) ([]ItunesResult, error) {
	if wait := itunesRateLimit - time.Since(c.last); wait > 0 {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c.last = time.Now()

	q := url.Values{}
	q.Set("term", term)
	q.Set("entity", "song")
	q.Set("limit", strconv.Itoa(limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, itunesSearchURL+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", stmpdUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("itunes search failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("itunes search returned status %d: %s", resp.StatusCode, truncate(string(body), 120))
	}

	var parsed struct {
		Results []ItunesResult `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to decode itunes search response: %w", err)
	}
	return parsed.Results, nil
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
