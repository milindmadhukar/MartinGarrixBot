package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	beatportBaseURL      = "https://api.beatport.com/v4"
	beatportLoginURL     = "https://api.beatport.com/v4/auth/login/"
	beatportAuthorizeURL = "https://api.beatport.com/v4/auth/o/authorize/"
	beatportTokenURL     = "https://api.beatport.com/v4/auth/o/token/"
	beatportRedirectURI  = "https://api.beatport.com/v4/auth/o/post-message/"
)

// BeatportConfig holds beatport API configuration
type BeatportConfig struct {
	Username  string
	Password  string
	LabelID   string
	ArtistIDs []string
	MaxTracks int
}

// BeatportTokenResponse represents the OAuth token response
type BeatportTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// BeatportTrack represents a processed track from Beatport
type BeatportTrack struct {
	ID           int              `json:"id"`
	Name         string           `json:"name"`
	MixName      string           `json:"mix_name"`
	ReleaseDate  string           `json:"release_date"`
	Artists      []BeatportArtist `json:"artists"`
	Remixers     []BeatportArtist `json:"remixers"`
	Release      BeatportRelease  `json:"release"`
	Key          BeatportKey      `json:"key,omitempty"`
	BPM          int              `json:"bpm,omitempty"`
	Genre        BeatportGenre    `json:"genre"`
	SubGenre     BeatportGenre    `json:"sub_genre,omitempty"`
	LengthMs     int              `json:"length_ms"`
	ThumbnailURL string           `json:"thumbnail_url"`
}

// BeatportAPITrack represents the raw track from Beatport API
type BeatportAPITrack struct {
	ID          int              `json:"id"`
	Name        string           `json:"name"`
	MixName     string           `json:"mix_name"`
	PublishDate string           `json:"publish_date"`
	Artists     []BeatportArtist `json:"artists"`
	Remixers    []BeatportArtist `json:"remixers"`
	Release     BeatportRelease  `json:"release"`
	Key         BeatportKey      `json:"key,omitempty"`
	BPM         int              `json:"bpm,omitempty"`
	Genre       BeatportGenre    `json:"genre"`
	SubGenre    BeatportGenre    `json:"sub_genre,omitempty"`
	LengthMs    int              `json:"length_ms"`
}

// BeatportArtist represents an artist from Beatport
type BeatportArtist struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// BeatportRelease represents a release from Beatport
type BeatportRelease struct {
	ID    int           `json:"id"`
	Name  string        `json:"name"`
	Image BeatportImage `json:"image"`
}

// BeatportImage represents an image from Beatport
type BeatportImage struct {
	URI        string `json:"uri"`
	DynamicURI string `json:"dynamic_uri"`
}

// BeatportKey represents a musical key from Beatport
type BeatportKey struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

// BeatportGenre represents a genre from Beatport
type BeatportGenre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// BeatportTracksResponse represents a paginated response of tracks
type BeatportTracksResponse struct {
	Results  []BeatportAPITrack `json:"results"`
	Count    int                `json:"count"`
	Next     string             `json:"next"`
	Previous string             `json:"previous"`
}

// BeatportClient handles communication with the Beatport API
type BeatportClient struct {
	httpClient  *http.Client // For auth (no redirects)
	apiClient   *http.Client // For API calls (follows redirects)
	accessToken string
	tokenExpiry time.Time
	config      *BeatportConfig
	clientID    string

	// Auth failures are held on the client, not as a loop variable, because the
	// caller re-enters through EnsureAuthenticated once per configured source --
	// one label plus four artist IDs -- on every 15 minute cycle. A local counter
	// would restart at zero each time, which is how a rejected password produced
	// roughly 480 login attempts a day against Beatport for six days straight.
	authFailures    int
	nextAuthAttempt time.Time
	lastAuthErr     error
}

// ErrBeatportCredentialsRejected marks an authentication failure that retrying
// cannot fix. Beatport answers a bad username or password with 403, the same status
// a bot-protection layer would use, so the two are indistinguishable without reading
// the body -- and treating a permanent rejection as a transient block is what kept
// the retry loop running for days.
var ErrBeatportCredentialsRejected = errors.New("beatport rejected the configured credentials")

const (
	// beatportAuthBaseBackoff is the delay after a single transient auth failure.
	beatportAuthBaseBackoff = time.Minute
	// beatportAuthMaxBackoff caps the transient backoff.
	beatportAuthMaxBackoff = time.Hour
	// beatportCredentialsCooldown is how long to wait after a rejection that
	// retrying cannot fix. Long enough to stop hammering, short enough that a
	// corrected password takes effect without a restart.
	beatportCredentialsCooldown = 6 * time.Hour
)

// beatportAuthBackoff maps consecutive transient auth failures onto a delay before
// the next attempt, mirroring radioBackoff's shape.
//
// Written out rather than using the min builtin: this package declares its own
// int-only min in levenshtein.go, which shadows it and does not accept Durations.
func beatportAuthBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}

	shift := failures - 1
	if shift > 8 {
		shift = 8
	}

	backoff := beatportAuthBaseBackoff << shift
	if backoff > beatportAuthMaxBackoff {
		return beatportAuthMaxBackoff
	}
	return backoff
}

// NewBeatportClient creates a new Beatport API client
func NewBeatportClient(config *BeatportConfig) (*BeatportClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	clientID, err := getBeatportClientID()
	if err != nil {
		return nil, fmt.Errorf("failed to get client ID: %w", err)
	}

	slog.Info("Beatport client ID obtained", slog.String("client_id", clientID))

	// Auth client doesn't follow redirects
	authClient := &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// API client follows redirects and shares the same cookie jar
	apiClient := &http.Client{
		Timeout: 30 * time.Second,
		Jar:     jar,
	}

	return &BeatportClient{
		httpClient: authClient,
		apiClient:  apiClient,
		config:     config,
		clientID:   clientID,
	}, nil
}

// truncateBody bounds a response body before it reaches the log.
func truncateBody(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// resetCookies gives both clients a fresh, shared cookie jar.
//
// They must keep sharing one: the OAuth authorize step relies on the sessionid the
// login step set, so handing them separate jars would break the flow.
func (bc *BeatportClient) resetCookies() error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	bc.httpClient.Jar = jar
	bc.apiClient.Jar = jar
	return nil
}

// getBeatportClientID scrapes the client ID from the Beatport docs page
func getBeatportClientID() (string, error) {
	slog.Debug("Fetching client ID from Beatport docs...")

	resp, err := http.Get("https://api.beatport.com/v4/docs/")
	if err != nil {
		return "", fmt.Errorf("could not fetch docs page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("could not read docs page: %w", err)
	}

	// Extract JavaScript file references
	re := regexp.MustCompile(`src="(/static/btprt/[^"]+\.js)"`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	// Try each JS file for API_CLIENT_ID
	for _, match := range matches {
		if len(match) > 1 {
			jsURL := "https://api.beatport.com" + match[1]
			jsResp, err := http.Get(jsURL)
			if err != nil {
				continue
			}

			jsBody, err := io.ReadAll(jsResp.Body)
			jsResp.Body.Close()
			if err != nil {
				continue
			}

			clientRe := regexp.MustCompile(`API_CLIENT_ID:\s*['"]([A-Za-z0-9]+)['"]`)
			clientMatches := clientRe.FindStringSubmatch(string(jsBody))

			if len(clientMatches) > 1 {
				return clientMatches[1], nil
			}
		}
	}

	return "", fmt.Errorf("could not find API_CLIENT_ID in any JavaScript file")
}

// Authenticate performs the full OAuth flow.
//
// The cookie jar is replaced first, and that is not housekeeping -- it is the fix
// for the outage that ran from 2026-08-25 to 2026-08-31.
//
// Beatport's API is Django/DRF. A successful login sets csrftoken and sessionid
// cookies, and once the client is holding a csrftoken, DRF starts enforcing CSRF on
// every later POST to the same endpoint. A server-to-server client has no Referer or
// trusted Origin to offer, so every re-authentication after the first was rejected
// with 403 "CSRF Failed: Referer checking failed - no Referer.". The first login
// after a process start always worked, which is exactly why the failure looked
// intermittent, and then permanent once the process had been up long enough for the
// initial token to expire.
//
// Starting from a clean jar reproduces the first-login conditions on every attempt.
func (bc *BeatportClient) Authenticate() error {
	slog.Info("Starting Beatport authentication...")

	if err := bc.resetCookies(); err != nil {
		return fmt.Errorf("failed to reset the beatport cookie jar: %w", err)
	}

	// Step 1: Login
	loginData := map[string]string{
		"username": bc.config.Username,
		"password": bc.config.Password,
	}

	loginJSON, err := json.Marshal(loginData)
	if err != nil {
		return fmt.Errorf("failed to marshal login data: %w", err)
	}

	loginReq, err := http.NewRequest("POST", beatportLoginURL, strings.NewReader(string(loginJSON)))
	if err != nil {
		return fmt.Errorf("failed to create login request: %w", err)
	}

	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Accept", "application/json")

	loginResp, err := bc.httpClient.Do(loginReq)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer loginResp.Body.Close()

	loginBody, _ := io.ReadAll(loginResp.Body)
	if loginResp.StatusCode != http.StatusOK && loginResp.StatusCode != http.StatusFound {
		// Logged at WARN with the body attached, deliberately. This used to be a
		// Debug line, and production runs at info -- so for six days the only
		// evidence of the outage was "login failed with status 403", which reads
		// like a bot block and is not what was happening at all. Beatport answers
		// with 403 for a bad password, for a CSRF failure, and for a rate limit; the
		// body is the only thing that tells them apart, so it must be visible.
		slog.Warn("Beatport login failed",
			slog.Int("status", loginResp.StatusCode),
			slog.String("body", truncateBody(string(loginBody), 300)),
		)

		if loginResp.StatusCode == http.StatusForbidden || loginResp.StatusCode == http.StatusUnauthorized {
			if strings.Contains(strings.ToLower(string(loginBody)), "incorrect username or password") {
				return fmt.Errorf("%w (status %d)", ErrBeatportCredentialsRejected, loginResp.StatusCode)
			}
		}
		return fmt.Errorf("login failed with status %d: %s",
			loginResp.StatusCode, truncateBody(string(loginBody), 200))
	}

	slog.Debug("Beatport login successful, requesting authorization...")

	// Step 2: Request authorization code
	authParams := url.Values{}
	authParams.Set("response_type", "code")
	authParams.Set("client_id", bc.clientID)
	authParams.Set("redirect_uri", beatportRedirectURI)

	authReq, err := http.NewRequest("GET", beatportAuthorizeURL+"?"+authParams.Encode(), nil)
	if err != nil {
		return fmt.Errorf("failed to create authorize request: %w", err)
	}

	authResp, err := bc.httpClient.Do(authReq)
	if err != nil {
		return fmt.Errorf("authorize request failed: %w", err)
	}
	defer authResp.Body.Close()

	location := authResp.Header.Get("Location")
	if location == "" {
		authBody, _ := io.ReadAll(authResp.Body)
		return fmt.Errorf("no redirect location found (status %d): %s", authResp.StatusCode, string(authBody))
	}

	parsedLocation, err := url.Parse(location)
	if err != nil {
		return fmt.Errorf("failed to parse redirect location: %w", err)
	}

	code := parsedLocation.Query().Get("code")
	if code == "" {
		return fmt.Errorf("no authorization code in redirect: %s", location)
	}

	slog.Debug("Beatport authorization code obtained, exchanging for token...")

	// Step 3: Exchange for access token
	tokenParams := url.Values{}
	tokenParams.Set("grant_type", "authorization_code")
	tokenParams.Set("code", code)
	tokenParams.Set("client_id", bc.clientID)
	tokenParams.Set("redirect_uri", beatportRedirectURI)

	tokenReq, err := http.NewRequest("POST", beatportTokenURL+"?"+tokenParams.Encode(), nil)
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}

	tokenReq.Header.Set("Accept", "application/json")

	tokenResp, err := bc.httpClient.Do(tokenReq)
	if err != nil {
		return fmt.Errorf("token request failed: %w", err)
	}
	defer tokenResp.Body.Close()

	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return fmt.Errorf("failed to read token response: %w", err)
	}

	if tokenResp.StatusCode != http.StatusOK {
		return fmt.Errorf("token exchange failed with status %d: %s", tokenResp.StatusCode, string(tokenBody))
	}

	var tokenResponse BeatportTokenResponse
	if err := json.Unmarshal(tokenBody, &tokenResponse); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	bc.accessToken = tokenResponse.AccessToken
	bc.tokenExpiry = time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)

	slog.Info("Beatport authentication successful")
	return nil
}

// EnsureAuthenticated refreshes the access token when it is missing or close to
// expiry, and holds a cooldown after a failure so that a broken credential cannot
// turn every fetch cycle into another five login attempts.
func (bc *BeatportClient) EnsureAuthenticated() error {
	if bc.accessToken != "" && time.Now().Before(bc.tokenExpiry.Add(-5*time.Minute)) {
		return nil
	}

	// Still inside the cooldown from the last failure: replay it rather than
	// re-attempting. Callers see the same error and log the same line they would
	// have, but Beatport sees nothing.
	if time.Now().Before(bc.nextAuthAttempt) {
		return bc.lastAuthErr
	}

	err := bc.Authenticate()
	if err == nil {
		bc.authFailures = 0
		bc.lastAuthErr = nil
		bc.nextAuthAttempt = time.Time{}
		return nil
	}

	bc.authFailures++
	bc.lastAuthErr = err

	if errors.Is(err, ErrBeatportCredentialsRejected) {
		bc.nextAuthAttempt = time.Now().Add(beatportCredentialsCooldown)
		// Logged here rather than at every call site, so a permanent rejection
		// produces one actionable line per cooldown instead of one per source.
		slog.Error("Beatport rejected the configured credentials - update beatport_username/beatport_password in the bot config",
			slog.String("username", bc.config.Username),
			slog.Duration("retrying_in", beatportCredentialsCooldown))
		return err
	}

	backoff := beatportAuthBackoff(bc.authFailures)
	bc.nextAuthAttempt = time.Now().Add(backoff)
	slog.Warn("Beatport authentication failed, backing off",
		slog.Int("consecutive_failures", bc.authFailures),
		slog.Duration("retrying_in", backoff),
		slog.Any("err", err))
	return err
}

// GetLabelTracks fetches tracks for a label from the API
func (bc *BeatportClient) GetLabelTracks(labelID string, page int, perPage int) (*BeatportTracksResponse, error) {
	if err := bc.EnsureAuthenticated(); err != nil {
		return nil, err
	}

	apiURL := fmt.Sprintf("%s/catalog/tracks?label_id=%s&page=%d&per_page=%d&sort_by=publish_date&order=desc",
		beatportBaseURL, labelID, page, perPage)

	return bc.fetchTracks(apiURL)
}

// GetArtistTracks fetches tracks for an artist from the API
func (bc *BeatportClient) GetArtistTracks(artistID string, page int, perPage int) (*BeatportTracksResponse, error) {
	if err := bc.EnsureAuthenticated(); err != nil {
		return nil, err
	}

	apiURL := fmt.Sprintf("%s/catalog/tracks?artist_id=%s&page=%d&per_page=%d&sort_by=publish_date&order=desc",
		beatportBaseURL, artistID, page, perPage)

	return bc.fetchTracks(apiURL)
}

func (bc *BeatportClient) fetchTracks(apiURL string) (*BeatportTracksResponse, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", bc.accessToken))
	req.Header.Set("Accept", "application/json")

	resp, err := bc.apiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tracksResp BeatportTracksResponse
	if err := json.Unmarshal(body, &tracksResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &tracksResp, nil
}

// GetAllLabelTracks fetches all tracks for a label with pagination
func (bc *BeatportClient) GetAllLabelTracks(labelID string, maxTracks int) ([]BeatportTrack, error) {
	var allTracks []BeatportTrack
	page := 1
	perPage := 100

	for {
		slog.Debug("Fetching label tracks page", slog.Int("page", page))
		tracksResp, err := bc.GetLabelTracks(labelID, page, perPage)
		if err != nil {
			return nil, err
		}

		for _, apiTrack := range tracksResp.Results {
			allTracks = append(allTracks, ProcessBeatportTrack(apiTrack))
		}

		if maxTracks > 0 && len(allTracks) >= maxTracks {
			allTracks = allTracks[:maxTracks]
			break
		}

		if tracksResp.Next == "" || len(tracksResp.Results) == 0 {
			break
		}

		page++
	}

	slog.Info("Fetched label tracks", slog.Int("count", len(allTracks)))
	return allTracks, nil
}

// GetAllArtistTracks fetches all tracks for an artist with pagination
func (bc *BeatportClient) GetAllArtistTracks(artistID string, maxTracks int) ([]BeatportTrack, error) {
	var allTracks []BeatportTrack
	page := 1
	perPage := 100

	for {
		slog.Debug("Fetching artist tracks page", slog.Int("page", page), slog.String("artist_id", artistID))
		tracksResp, err := bc.GetArtistTracks(artistID, page, perPage)
		if err != nil {
			return nil, err
		}

		for _, apiTrack := range tracksResp.Results {
			allTracks = append(allTracks, ProcessBeatportTrack(apiTrack))
		}

		if maxTracks > 0 && len(allTracks) >= maxTracks {
			allTracks = allTracks[:maxTracks]
			break
		}

		if tracksResp.Next == "" || len(tracksResp.Results) == 0 {
			break
		}

		page++
	}

	slog.Info("Fetched artist tracks", slog.Int("count", len(allTracks)), slog.String("artist_id", artistID))
	return allTracks, nil
}

// ProcessBeatportTrack converts an API track to our internal format
func ProcessBeatportTrack(apiTrack BeatportAPITrack) BeatportTrack {
	// Use release image URI as thumbnail (square album artwork)
	thumbnailURL := ""
	if apiTrack.Release.Image.URI != "" {
		thumbnailURL = apiTrack.Release.Image.URI
	}

	// Verify it's a square image by checking the URL pattern
	if thumbnailURL != "" && !IsSquareImageURL(thumbnailURL) {
		slog.Warn("Beatport thumbnail may not be square, skipping",
			slog.String("url", thumbnailURL),
			slog.Int("track_id", apiTrack.ID))
		thumbnailURL = ""
	}

	return BeatportTrack{
		ID:           apiTrack.ID,
		Name:         apiTrack.Name,
		MixName:      apiTrack.MixName,
		ReleaseDate:  apiTrack.PublishDate,
		Artists:      apiTrack.Artists,
		Remixers:     apiTrack.Remixers,
		Release:      apiTrack.Release,
		Key:          apiTrack.Key,
		BPM:          apiTrack.BPM,
		Genre:        apiTrack.Genre,
		SubGenre:     apiTrack.SubGenre,
		LengthMs:     apiTrack.LengthMs,
		ThumbnailURL: thumbnailURL,
	}
}

// IsSquareImageURL checks if a Beatport image URL represents a square image
// Beatport image URLs contain dimensions like /image_size/500x500/ or /image_size/1400x1400/
func IsSquareImageURL(imageURL string) bool {
	re := regexp.MustCompile(`/image_size/(\d+)x(\d+)/`)
	matches := re.FindStringSubmatch(imageURL)
	if len(matches) == 3 {
		return matches[1] == matches[2] // Width equals height
	}
	// If we can't parse dimensions, assume it's okay (don't reject)
	return true
}

// FormatBeatportArtists formats a list of beatport artists into a comma-separated string
func FormatBeatportArtists(artists []BeatportArtist) string {
	names := make([]string, len(artists))
	for i, artist := range artists {
		names[i] = artist.Name
	}
	return strings.Join(names, ", ")
}

// FormatBeatportDuration formats milliseconds into a human-readable duration string
func FormatBeatportDuration(ms int) string {
	seconds := ms / 1000
	minutes := seconds / 60
	seconds = seconds % 60
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}
