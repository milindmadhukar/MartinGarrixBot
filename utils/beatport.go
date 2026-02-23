package utils

import (
	"encoding/json"
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
	Username     string
	Password     string
	LabelID      string
	ArtistIDs    []string
	MaxTracks    int
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

// Authenticate performs the full OAuth flow
func (bc *BeatportClient) Authenticate() error {
	slog.Info("Starting Beatport authentication...")

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
		return fmt.Errorf("login failed with status %d: %s", loginResp.StatusCode, string(loginBody))
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

// EnsureAuthenticated checks and refreshes auth if needed
func (bc *BeatportClient) EnsureAuthenticated() error {
	if bc.accessToken == "" || time.Now().After(bc.tokenExpiry.Add(-5*time.Minute)) {
		return bc.Authenticate()
	}
	return nil
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
