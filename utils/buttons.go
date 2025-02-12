package utils

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

type buttonConfig struct {
	label    string
	emoji    string
	urlField pgtype.Text
}

func createButton(config buttonConfig) discord.ButtonComponent {
	name, id, animated := ExtractEmojiParts(config.emoji)

	return discord.NewLinkButton(
		config.label,
		config.urlField.String,
	).WithEmoji(discord.ComponentEmoji{
		Name:     name,
		ID:       id,
		Animated: animated,
	})
}

func GetSongButtons(song db.Song) []discord.InteractiveComponent {
	buttonConfigs := []buttonConfig{
		{
			label:    "Spotify",
			emoji:    SpotifyEmoji,
			urlField: song.SpotifyUrl,
		},
		{
			label:    "Youtube",
			emoji:    YoutubeEmoji,
			urlField: song.YoutubeUrl,
		},
		{
			label:    "Apple Music",
			emoji:    AppleMusicEmoji,
			urlField: song.AppleMusicUrl,
		},
	}

	var buttons []discord.InteractiveComponent

	for _, config := range buttonConfigs {
		if config.urlField.Valid {
			button := createButton(config)
			buttons = append(buttons, button)
		}
	}

	return buttons
}