package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// Tools returns the schema handed to the model on every request. Keep this in
// sync with the case statements in Dispatch.
func Tools() []Tool {
	return []Tool{
		{Type: "function", Function: ToolFunction{
			Name:        "search_songs",
			Description: "Search the Martin Garrix / STMPD RCRDS song catalogue by title and/or artist. query must be ONLY artist and/or title words -- descriptive words like \"unreleased\", \"best\" or \"favourite\" will never match and return nothing. Each result already carries is_unreleased and release_date, so filter/pick from the results yourself rather than searching for release status. Call get_song_details on a song_id for lyrics or links. If nothing you're looking for is in the results, say so -- never invent a track name that isn't in the catalogue.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {"type": "string", "description": "artist and/or title words only, e.g. \"AREA21\" or \"garrix animals\""}
				},
				"required": ["query"]
			}`),
		}},
		{Type: "function", Function: ToolFunction{
			Name:        "get_song_details",
			Description: "Get full details for one song by its song_id (from search_songs or random_song): lyrics, release date, genre, BPM and streaming links.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"song_id": {"type": "integer", "description": "the song_id returned by search_songs or random_song"}
				},
				"required": ["song_id"]
			}`),
		}},
		{Type: "function", Function: ToolFunction{
			Name:        "random_song",
			Description: "Get a few random songs from the catalogue that have lyrics, for inspiration when nothing specific was asked for.",
			Parameters:  json.RawMessage(`{"type": "object", "properties": {}}`),
		}},
		{Type: "function", Function: ToolFunction{
			Name:        "sample_messages",
			Description: "Sample real snippets of what this Discord community has actually said about a topic, for flavor and in-jokes. Returns raw text only, with nobody's name attached -- never present a snippet as belonging to a specific person, only as \"something people here have said\".",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"topic": {"type": "string", "description": "a word or short phrase to search past messages for"},
					"limit": {"type": "integer", "description": "how many snippets to return, at most 15"}
				},
				"required": ["topic"]
			}`),
		}},
	}
}

// Dispatch executes one tool call against the given guild's data and returns
// a JSON string suitable to send back as a role "tool" message.
func Dispatch(ctx context.Context, queries *db.Queries, guildID int64, name, argsJSON string) (string, error) {
	switch name {
	case "search_songs":
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("search_songs: bad arguments: %w", err)
		}
		rows, err := queries.SearchSongsForAgent(ctx, utils.SearchTerms(args.Query))
		if err != nil {
			return "", err
		}
		return marshal(rows)

	case "get_song_details":
		var args struct {
			SongID int64 `json:"song_id"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("get_song_details: bad arguments: %w", err)
		}
		song, err := queries.GetSongByID(ctx, args.SongID)
		if err != nil {
			return "", err
		}
		return marshal(songDetails{
			Name:          song.Name,
			Artists:       song.Artists,
			MixName:       text(song.MixName),
			ReleaseDate:   text(song.ReleaseDate),
			Genre:         text(song.Genre),
			SubGenre:      text(song.SubGenre),
			Lyrics:        text(song.Lyrics),
			IsUnreleased:  song.IsUnreleased,
			SpotifyURL:    text(song.SpotifyUrl),
			AppleMusicURL: text(song.AppleMusicUrl),
			YoutubeURL:    text(song.YoutubeUrl),
			BeatportURL:   text(song.BeatportUrl),
		})

	case "random_song":
		rows, err := queries.GetRandomSongNamesWithLyrics(ctx)
		if err != nil {
			return "", err
		}
		if len(rows) > 5 {
			rows = rows[:5]
		}
		return marshal(rows)

	case "sample_messages":
		var args struct {
			Topic string `json:"topic"`
			Limit int32  `json:"limit"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return "", fmt.Errorf("sample_messages: bad arguments: %w", err)
		}
		limit := args.Limit
		if limit <= 0 || limit > 15 {
			limit = 15
		}
		rows, err := queries.SampleMessagesByContent(ctx, db.SampleMessagesByContentParams{
			GuildID:  guildID,
			Term:     args.Topic,
			RowLimit: limit,
		})
		if err != nil {
			return "", err
		}
		return marshal(rows)

	default:
		return "", fmt.Errorf("ai: unknown tool %q", name)
	}
}

// songDetails is a curated subset of db.Song: the full row also carries sync
// bookkeeping (match keys, locked fields, LRCLIB state) that has nothing to
// do with answering a Discord message.
type songDetails struct {
	Name          string `json:"name"`
	Artists       string `json:"artists"`
	MixName       string `json:"mix_name,omitempty"`
	ReleaseDate   string `json:"release_date,omitempty"`
	Genre         string `json:"genre,omitempty"`
	SubGenre      string `json:"sub_genre,omitempty"`
	Lyrics        string `json:"lyrics,omitempty"`
	IsUnreleased  bool   `json:"is_unreleased"`
	SpotifyURL    string `json:"spotify_url,omitempty"`
	AppleMusicURL string `json:"apple_music_url,omitempty"`
	YoutubeURL    string `json:"youtube_url,omitempty"`
	BeatportURL   string `json:"beatport_url,omitempty"`
}

func text(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

func marshal(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
