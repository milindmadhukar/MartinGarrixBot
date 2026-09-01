package utils

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

// discordButtonsPerRow is Discord's hard limit on components in one action row.
// Exceeding it does not drop the extra buttons -- it rejects the whole message with
// 50035: Invalid Form Body, so a song carrying every streaming link would silently
// fail to announce.
const discordButtonsPerRow = 5

type buttonConfig struct {
	label    string
	emoji    string
	urlField pgtype.Text
}

func createButton(config buttonConfig) discord.ButtonComponent {
	button := discord.NewLinkButton(config.label, config.urlField.String)

	// Not every service has a custom emoji uploaded to the guild. Attaching an
	// empty ComponentEmoji is not the same as attaching none, so skip it entirely.
	if config.emoji == "" {
		return button
	}

	name, id, animated := ExtractEmojiParts(config.emoji)
	return button.WithEmoji(discord.ComponentEmoji{
		Name:     name,
		ID:       id,
		Animated: animated,
	})
}

// BeatportTrackURL is the public page for a beatport track.
//
// The route is /track/<slug>/<id>; /track/<id> is not a route Beatport serves, so the
// id-only form this used to build 404'd for every song in the catalogue. The slug is
// Beatport's own, stored from the API, because it is slugified from the track's full
// name including any feature credit -- which this catalogue strips into the artist
// column and so cannot reconstruct.
func BeatportTrackURL(slug string, id int32) string {
	return fmt.Sprintf("https://www.beatport.com/track/%s/%d", slug, id)
}

// beatportLink resolves the best beatport destination for a song.
//
// songs.beatport_url is a RELEASE page and is only ever written from the STMPD
// dataset, which carries one for about 30 of its 1015 releases. Beatport-sourced rows
// instead hold a track id -- and those are exactly the rows that have no streaming
// links at all, because releases outside the STMPD catalogue (the U2 and Madonna
// collaborations, for instance) have no dataset document to backfill from. Without
// this they would show no buttons whatsoever despite a perfectly good link existing.
func beatportLink(song db.Song) pgtype.Text {
	if song.BeatportUrl.Valid && song.BeatportUrl.String != "" {
		return song.BeatportUrl
	}
	// Without the slug there is no URL to build. Offering a button that leads to a
	// 404 is worse than offering none, so the row simply shows one fewer button
	// until the slug backfill reaches it.
	if song.BeatportID.Valid && song.BeatportSlug.Valid && song.BeatportSlug.String != "" {
		return pgtype.Text{
			String: BeatportTrackURL(song.BeatportSlug.String, song.BeatportID.Int32),
			Valid:  true,
		}
	}
	return pgtype.Text{}
}

// songButtonConfigs lists every link a song row can carry, in the order they should
// appear. The first five -- the services the community actually uses -- land in the
// first action row; the long tail follows in a second.
func songButtonConfigs(song db.Song) []buttonConfig {
	return []buttonConfig{
		{label: "Spotify", emoji: SpotifyEmoji, urlField: song.SpotifyUrl},
		{label: "Youtube", emoji: YoutubeEmoji, urlField: song.YoutubeUrl},
		{label: "Apple Music", emoji: AppleMusicEmoji, urlField: song.AppleMusicUrl},
		{label: "Beatport", emoji: BeatportEmoji, urlField: beatportLink(song)},
		{label: "Deezer", emoji: DeezerEmoji, urlField: song.DeezerUrl},
		{label: "Tidal", emoji: TidalEmoji, urlField: song.TidalUrl},
		{label: "Amazon Music", emoji: AmazonMusicEmoji, urlField: song.AmazonMusicUrl},
		{label: "YouTube Music", emoji: YoutubeMusicEmoji, urlField: song.YoutubeMusicUrl},
	}
}

// BeatportButton builds a beatport link button for a URL the caller already has,
// so the announcement path gets the same styling as the /links card.
func BeatportButton(url string) discord.ButtonComponent {
	return createButton(buttonConfig{
		label:    "Beatport",
		emoji:    BeatportEmoji,
		urlField: pgtype.Text{String: url, Valid: true},
	})
}

// GetSongButtons returns the link buttons for a song as a flat slice, for callers
// that place them in an action row of their own. Callers that may exceed
// discordButtonsPerRow should use GetSongButtonRows instead.
func GetSongButtons(song db.Song) []discord.InteractiveComponent {
	var buttons []discord.InteractiveComponent
	for _, config := range songButtonConfigs(song) {
		if config.urlField.Valid {
			buttons = append(buttons, createButton(config))
		}
	}
	return buttons
}

// GetSongButtonRows returns the link buttons already chunked into action rows.
//
// It returns nil when the song has no links at all, which is the case callers used
// to guard by hand with `if song.SpotifyUrl.Valid || ...` -- appending a nil slice
// of rows is harmless, so that guard is no longer needed.
func GetSongButtonRows(song db.Song) []discord.LayoutComponent {
	return ChunkButtonRows(GetSongButtons(song))
}

// ChunkButtonRows splits buttons into action rows within Discord's per-row cap, for
// callers that prepend their own buttons to a song's links.
func ChunkButtonRows(buttons []discord.InteractiveComponent) []discord.LayoutComponent {
	if len(buttons) == 0 {
		return nil
	}

	var rows []discord.LayoutComponent
	for start := 0; start < len(buttons); start += discordButtonsPerRow {
		end := start + discordButtonsPerRow
		if end > len(buttons) {
			end = len(buttons)
		}
		rows = append(rows, discord.NewActionRow(buttons[start:end]...))
	}
	return rows
}
