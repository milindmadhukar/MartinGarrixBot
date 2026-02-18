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
	Artist   string
	SongName string
}

type RadioManager struct {
	Client        disgolink.Client
	ActiveGuilds  map[snowflake.ID]bool
	CurrentTracks map[snowflake.ID]TrackInfo // Store current track info per guild
	IsConnected   bool
	mu            sync.RWMutex
	OnTrackChange func(guildID snowflake.ID, trackName, artist, thumbnailURL string)
}

func NewRadioManager(userID snowflake.ID) *RadioManager {
	return &RadioManager{
		Client:        disgolink.New(userID),
		ActiveGuilds:  make(map[snowflake.ID]bool),
		CurrentTracks: make(map[snowflake.ID]TrackInfo),
	}
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

func (rm *RadioManager) SetCurrentTrack(guildID snowflake.ID, artist, songName string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.CurrentTracks[guildID] = TrackInfo{
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
func (rm *RadioManager) PlayTrackWithInfo(ctx context.Context, guildID snowflake.ID, query, artist, songName string) error {
	// Store the track info before playing
	rm.SetCurrentTrack(guildID, artist, songName)

	// Play the track
	if err := rm.PlayTrack(ctx, guildID, query); err != nil {
		return err
	}

	return nil
}
