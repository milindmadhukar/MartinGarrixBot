package utils

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
)

type TrackInfo struct {
	SongID   int64  // Database song ID
	Artist   string // For fallback/display
	SongName string // For fallback/display
}

type SkipVote struct {
	Voters       map[snowflake.ID]bool // User IDs who voted to skip
	TotalMembers int                   // Total members in voice channel
}

type RadioManager struct {
	Client               disgolink.Client
	ActiveGuilds         map[snowflake.ID]bool
	PausedGuilds         map[snowflake.ID]bool      // Guilds where radio is paused (waiting for listeners)
	CurrentTracks        map[snowflake.ID]TrackInfo // Store current track info per guild
	SkipVotes            map[snowflake.ID]*SkipVote // Store skip votes per guild
	IsConnected          bool
	ReconnectAttempts    int       // Track reconnection attempts
	LastConnectAttempt   time.Time // Track last connection attempt
	mu                   sync.RWMutex
	OnTrackChange        func(guildID snowflake.ID, trackName, artist, thumbnailURL string)
	OnLavalinkDisconnect func() // Callback when Lavalink disconnects permanently
}

func NewRadioManager(userID snowflake.ID) *RadioManager {
	return &RadioManager{
		Client:        disgolink.New(userID),
		ActiveGuilds:  make(map[snowflake.ID]bool),
		PausedGuilds:  make(map[snowflake.ID]bool),
		CurrentTracks: make(map[snowflake.ID]TrackInfo),
		SkipVotes:     make(map[snowflake.ID]*SkipVote),
	}
}

// ResetSkipVotes clears skip votes for a guild (call when new track starts)
func (rm *RadioManager) ResetSkipVotes(guildID snowflake.ID) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.SkipVotes, guildID)
}

// AddSkipVote adds a user's vote to skip the current track
// Returns (votesNeeded, currentVotes, shouldSkip)
func (rm *RadioManager) AddSkipVote(guildID, userID snowflake.ID, totalMembers int) (int, int, bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Initialize skip votes for this guild if not exists
	if rm.SkipVotes[guildID] == nil {
		rm.SkipVotes[guildID] = &SkipVote{
			Voters:       make(map[snowflake.ID]bool),
			TotalMembers: totalMembers,
		}
	}

	// Add the vote
	rm.SkipVotes[guildID].Voters[userID] = true
	rm.SkipVotes[guildID].TotalMembers = totalMembers

	currentVotes := len(rm.SkipVotes[guildID].Voters)
	// For >50% majority, we need: currentVotes / totalMembers > 0.5
	// Which is: currentVotes * 2 > totalMembers
	votesNeeded := (totalMembers / 2) + 1 // Minimum votes needed for display
	shouldSkip := currentVotes*2 > totalMembers

	return votesNeeded, currentVotes, shouldSkip
}

// GetSkipVoteStatus returns the current skip vote status
// Returns (votesNeeded, currentVotes, hasVoted)
func (rm *RadioManager) GetSkipVoteStatus(guildID, userID snowflake.ID, totalMembers int) (int, int, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	votesNeeded := (totalMembers / 2) + 1 // > 50%

	if rm.SkipVotes[guildID] == nil {
		return votesNeeded, 0, false
	}

	currentVotes := len(rm.SkipVotes[guildID].Voters)
	hasVoted := rm.SkipVotes[guildID].Voters[userID]

	return votesNeeded, currentVotes, hasVoted
}

func (rm *RadioManager) IsActive(guildID snowflake.ID) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.ActiveGuilds[guildID]
}

func (rm *RadioManager) SetActive(guildID snowflake.ID, active bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.ActiveGuilds[guildID] = active
}

func (rm *RadioManager) IsPaused(guildID snowflake.ID) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.PausedGuilds[guildID]
}

func (rm *RadioManager) SetPaused(guildID snowflake.ID, paused bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.PausedGuilds[guildID] = paused
}

func (rm *RadioManager) IsLavalinkConnected() bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.IsConnected
}

func (rm *RadioManager) SetLavalinkConnected(connected bool) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.IsConnected = connected
}

func (rm *RadioManager) SetCurrentTrack(guildID snowflake.ID, songID int64, artist, songName string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.CurrentTracks[guildID] = TrackInfo{
		SongID:   songID,
		Artist:   artist,
		SongName: songName,
	}
}

func (rm *RadioManager) GetCurrentTrack(guildID snowflake.ID) (TrackInfo, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	track, exists := rm.CurrentTracks[guildID]
	return track, exists
}

func (rm *RadioManager) ConnectToLavalink(ctx context.Context, url, password string) error {
	// Check if already connected
	if rm.IsLavalinkConnected() {
		// Check if node already exists
		if rm.Client.Node("main") != nil {
			slog.Info("Already connected to Lavalink")
			return nil
		}
	}

	// Strip http:// or https:// from URL as disgolink expects just host:port
	address := strings.TrimPrefix(url, "http://")
	address = strings.TrimPrefix(address, "https://")

	// Determine if secure based on original URL
	secure := strings.HasPrefix(url, "https://")

	tries := 5
	var lastErr error

	for tries > 0 {
		slog.Info("Attempting to connect to Lavalink...",
			slog.String("address", address),
			slog.Bool("secure", secure),
			slog.Int("tries_remaining", tries))

		nodeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := rm.Client.AddNode(nodeCtx, disgolink.NodeConfig{
			Name:     "main",
			Address:  address,
			Password: password,
			Secure:   secure,
		})
		cancel()

		if err != nil {
			lastErr = err
			tries--
			slog.Warn("Failed to connect to Lavalink", slog.Any("err", err), slog.Int("tries_remaining", tries))
			if tries > 0 {
				time.Sleep(5 * time.Second)
			}
			continue
		}

		slog.Info("Successfully connected to Lavalink")
		rm.SetLavalinkConnected(true)
		return nil
	}

	rm.SetLavalinkConnected(false)
	return fmt.Errorf("failed to connect to Lavalink after 5 attempts: %w", lastErr)
}

func (rm *RadioManager) PlayTrack(ctx context.Context, guildID snowflake.ID, query string) error {
	// Check if Lavalink is connected
	if !rm.IsLavalinkConnected() {
		return fmt.Errorf("lavalink is not connected")
	}

	player := rm.Client.Player(guildID)

	// Load tracks
	result, err := rm.Client.BestNode().LoadTracks(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to load tracks: %w", err)
	}

	var selectedTrack lavalink.Track

	switch result.LoadType {
	case lavalink.LoadTypeTrack:
		track, ok := result.Data.(lavalink.Track)
		if !ok {
			return fmt.Errorf("unexpected data type for track")
		}
		selectedTrack = track
		slog.Debug("Loaded single track", slog.String("title", selectedTrack.Info.Title))
	case lavalink.LoadTypePlaylist:
		playlist, ok := result.Data.(lavalink.Playlist)
		if !ok {
			return fmt.Errorf("unexpected data type for playlist")
		}
		if len(playlist.Tracks) > 0 {
			selectedTrack = playlist.Tracks[0]
			slog.Debug("Loaded playlist, using first track", slog.String("title", selectedTrack.Info.Title))
		}
	case lavalink.LoadTypeSearch:
		tracks, ok := result.Data.(lavalink.Search)
		if !ok {
			return fmt.Errorf("unexpected data type for search")
		}
		if len(tracks) > 0 {
			selectedTrack = tracks[0]
			slog.Debug("Loaded search results, using first track", slog.String("title", selectedTrack.Info.Title))
		}
	case lavalink.LoadTypeEmpty:
		return fmt.Errorf("no tracks found for query: %s", query)
	case lavalink.LoadTypeError:
		exception, ok := result.Data.(lavalink.Exception)
		if !ok {
			return fmt.Errorf("error loading track but could not parse exception")
		}
		return fmt.Errorf("error loading track: %s - %s", exception.Message, exception.Severity)
	}

	if selectedTrack.Info.Title == "" {
		return fmt.Errorf("no valid track found")
	}

	// Play the track
	if err := player.Update(ctx, lavalink.WithTrack(selectedTrack)); err != nil {
		return fmt.Errorf("failed to play track: %w", err)
	}

	slog.Info("Now playing",
		slog.String("guild_id", guildID.String()),
		slog.String("track", selectedTrack.Info.Title),
		slog.String("author", selectedTrack.Info.Author))

	return nil
}

func (rm *RadioManager) StopRadio(ctx context.Context, guildID snowflake.ID) error {
	rm.SetActive(guildID, false)

	player := rm.Client.ExistingPlayer(guildID)
	if player == nil {
		return nil
	}

	if err := player.Update(ctx, lavalink.WithNullTrack()); err != nil {
		return fmt.Errorf("failed to stop track: %w", err)
	}

	slog.Info("Stopped radio", slog.String("guild_id", guildID.String()))
	return nil
}

// StopRadioAndClearStatus stops the radio and clears the voice channel status
func (rm *RadioManager) StopRadioAndClearStatus(ctx context.Context, client bot.Client, botToken string, guildID snowflake.ID) error {
	// Stop the radio
	if err := rm.StopRadio(ctx, guildID); err != nil {
		slog.Error("Error stopping radio", slog.Any("err", err))
	}

	// Clear voice channel status
	player := rm.Client.ExistingPlayer(guildID)
	if player != nil {
		if channelID := player.ChannelID(); channelID != nil {
			if err := UpdateVoiceChannelStatus(ctx, client, botToken, *channelID, ""); err != nil {
				slog.Error("Failed to clear voice channel status", slog.Any("err", err))
			}
		}
	}

	return nil
}

// PlayTrackWithInfo plays a track and stores the artist/song info for status display
func (rm *RadioManager) PlayTrackWithInfo(ctx context.Context, guildID snowflake.ID, query string, songID int64, artist, songName string) error {
	// Store the track info before playing
	rm.SetCurrentTrack(guildID, songID, artist, songName)

	// Play the track
	if err := rm.PlayTrack(ctx, guildID, query); err != nil {
		return err
	}

	return nil
}

// GetActiveGuilds returns a slice of all active guild IDs
func (rm *RadioManager) GetActiveGuilds() []snowflake.ID {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	guilds := make([]snowflake.ID, 0, len(rm.ActiveGuilds))
	for guildID, active := range rm.ActiveGuilds {
		if active {
			guilds = append(guilds, guildID)
		}
	}
	return guilds
}

// StopAllRadios stops radio in all active guilds and marks them as inactive
func (rm *RadioManager) StopAllRadios(ctx context.Context) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	for guildID, active := range rm.ActiveGuilds {
		if active {
			slog.Info("Stopping radio due to Lavalink disconnect", slog.String("guild_id", guildID.String()))

			// Stop the player
			player := rm.Client.ExistingPlayer(guildID)
			if player != nil {
				if err := player.Update(ctx, lavalink.WithNullTrack()); err != nil {
					slog.Error("Failed to stop player", slog.Any("err", err), slog.String("guild_id", guildID.String()))
				}
			}

			// Mark as inactive
			rm.ActiveGuilds[guildID] = false
		}
	}
}

// DisconnectLavalink removes the Lavalink node and marks connection as lost
func (rm *RadioManager) DisconnectLavalink() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Remove the node to stop reconnection attempts
	node := rm.Client.Node("main")
	if node != nil {
		slog.Info("Removing Lavalink node to stop reconnection attempts")
		rm.Client.RemoveNode("main")
	}

	rm.IsConnected = false
}

// MonitorLavalinkConnection monitors the Lavalink connection and handles permanent disconnection
// Call this after successfully connecting to Lavalink
func (rm *RadioManager) MonitorLavalinkConnection(maxReconnectAttempts int) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0

	for range ticker.C {
		node := rm.Client.Node("main")
		if node == nil {
			// Node was removed, stop monitoring
			return
		}

		// Check node status
		status := node.Status()

		switch status {
		case disgolink.StatusDisconnected:
			consecutiveFailures++
			slog.Warn("Lavalink node disconnected",
				slog.Int("consecutive_failures", consecutiveFailures),
				slog.Int("max_attempts", maxReconnectAttempts))

			if consecutiveFailures >= maxReconnectAttempts {
				slog.Error("Lavalink connection failed after maximum reconnection attempts - giving up")

				// Mark as disconnected
				rm.SetLavalinkConnected(false)

				// Remove the node to stop infinite reconnection attempts
				rm.DisconnectLavalink()

				// Call the disconnect callback if set
				if rm.OnLavalinkDisconnect != nil {
					go rm.OnLavalinkDisconnect()
				}

				return
			}
		case disgolink.StatusConnected:
			// Reset failure counter if connected
			if consecutiveFailures > 0 {
				slog.Info("Lavalink reconnected successfully")
				consecutiveFailures = 0
			}
		}
	}
}
