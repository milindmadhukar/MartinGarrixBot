package utils_test

import (
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
	"github.com/milindmadhukar/MartinGarrixBot/utils"
)

func TestSuccessEmbed(t *testing.T) {
	t.Parallel()

	t.Run("title is prefixed and coloured", func(t *testing.T) {
		t.Parallel()

		embed := utils.SuccessEmbed("Done", "")
		if !strings.Contains(embed.Title, "Done") {
			t.Errorf("title = %q, want it to contain the message", embed.Title)
		}
		if !strings.HasPrefix(embed.Title, utils.TickEmoji) {
			t.Errorf("title = %q, want it to start with the tick emoji", embed.Title)
		}
		if embed.Color != utils.ColorSuccess {
			t.Errorf("color = %d, want ColorSuccess (%d)", embed.Color, utils.ColorSuccess)
		}
	})

	t.Run("an empty description is left unset", func(t *testing.T) {
		t.Parallel()

		if embed := utils.SuccessEmbed("Done", ""); embed.Description != "" {
			t.Errorf("description = %q, want it unset", embed.Description)
		}
	})

	t.Run("a description is carried through", func(t *testing.T) {
		t.Parallel()

		if embed := utils.SuccessEmbed("Done", "details here"); embed.Description != "details here" {
			t.Errorf("description = %q, want %q", embed.Description, "details here")
		}
	})

	// Discord rejects the whole message if a title exceeds 256 characters or a
	// description exceeds 4096, so both are cut before they get there.
	t.Run("over-long fields are truncated to Discord's limits", func(t *testing.T) {
		t.Parallel()

		embed := utils.SuccessEmbed(strings.Repeat("a", 500), strings.Repeat("b", 5000))
		if len([]rune(embed.Title)) != 256 {
			t.Errorf("title length = %d runes, want 256", len([]rune(embed.Title)))
		}
		if len([]rune(embed.Description)) != 2048 {
			t.Errorf("description length = %d runes, want 2048", len([]rune(embed.Description)))
		}
	})
}

func TestFailureEmbed(t *testing.T) {
	t.Parallel()

	embed := utils.FailureEmbed("Nope", "because")

	if !strings.HasPrefix(embed.Title, utils.CrossEmoji) {
		t.Errorf("title = %q, want it to start with the cross emoji", embed.Title)
	}
	if !strings.Contains(embed.Title, "Nope") {
		t.Errorf("title = %q, want it to contain the message", embed.Title)
	}
	if embed.Description != "because" {
		t.Errorf("description = %q, want %q", embed.Description, "because")
	}
	if embed.Color != utils.ColorError {
		t.Errorf("color = %d, want ColorError (%d)", embed.Color, utils.ColorError)
	}
}

func TestGetSongButtons(t *testing.T) {
	t.Parallel()

	t.Run("a song with no links gets no buttons", func(t *testing.T) {
		t.Parallel()

		// nil rather than an empty slice; callers must use len(), not a
		// comparison against []discord.InteractiveComponent{}.
		if got := utils.GetSongButtons(db.Song{}); len(got) != 0 {
			t.Errorf("got %d buttons, want none", len(got))
		}
	})

	t.Run("only valid links produce buttons", func(t *testing.T) {
		t.Parallel()

		song := db.Song{
			SpotifyUrl: text("https://open.spotify.com/track/abc"),
			YoutubeUrl: text("https://youtu.be/abc"),
			// AppleMusicUrl deliberately left invalid.
		}

		buttons := utils.GetSongButtons(song)
		if len(buttons) != 2 {
			t.Fatalf("got %d buttons, want 2", len(buttons))
		}
	})

	t.Run("buttons keep a stable order and carry their URLs", func(t *testing.T) {
		t.Parallel()

		song := db.Song{
			SpotifyUrl:    text("https://open.spotify.com/track/abc"),
			YoutubeUrl:    text("https://youtu.be/abc"),
			AppleMusicUrl: text("https://music.apple.com/abc"),
		}

		buttons := utils.GetSongButtons(song)
		if len(buttons) != 3 {
			t.Fatalf("got %d buttons, want 3", len(buttons))
		}

		want := []struct{ label, url string }{
			{"Spotify", "https://open.spotify.com/track/abc"},
			{"Youtube", "https://youtu.be/abc"},
			{"Apple Music", "https://music.apple.com/abc"},
		}

		for i, w := range want {
			button, ok := buttons[i].(discord.ButtonComponent)
			if !ok {
				t.Fatalf("button %d is %T, want a button component", i, buttons[i])
			}
			if button.Label != w.label {
				t.Errorf("button %d label = %q, want %q", i, button.Label, w.label)
			}
			if button.URL != w.url {
				t.Errorf("button %d url = %q, want %q", i, button.URL, w.url)
			}
		}
	})
}
