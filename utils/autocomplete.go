package utils

import (
	"strconv"

	"github.com/disgoorg/disgo/discord"
)

// discordChoiceNameLimit is Discord's cap on an autocomplete choice's display name.
const discordChoiceNameLimit = 100

// SongChoice is the minimum an autocomplete row needs: what to show, and which row
// to load when it is picked.
type SongChoice struct {
	ID      int64
	Name    string
	Artists string
	Mix     string
}

// BuildSongChoices renders autocomplete choices whose value is the song id.
//
// The value used to be a JSON object of {name, artists, release_date}, which Discord
// rejects: choice values are capped at 100 characters and
// {"name":"Repeat It (Acoustic Version)","artists":"Martin Garrix & Ed Sheeran",...}
// is 104. That is the "invalid form body" the command handlers carried TODOs about,
// and long remix names made it routine. An id is never more than a few bytes, and it
// also stops a song's display name being load-bearing for looking it back up.
func BuildSongChoices(songs []SongChoice) []discord.AutocompleteChoice {
	choices := make([]discord.AutocompleteChoice, 0, len(songs))
	for _, song := range songs {
		// The rendition belongs in the label. Renditions are listed now rather than
		// hidden, and without it two of them read as the same entry twice -- which is
		// exactly the confusion that hiding them was meant to avoid.
		label := SongHeading(song.Artists, song.Name, song.Mix)
		if len(label) > discordChoiceNameLimit {
			label = label[:discordChoiceNameLimit-1] + "…"
		}
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  label,
			Value: strconv.FormatInt(song.ID, 10),
		})
	}
	return choices
}

// ParseSongChoice reads back the value produced by BuildSongChoices.
//
// Discord sends whatever the user typed when they submit without picking a
// suggestion, so a non-numeric value is an ordinary outcome, not a fault.
func ParseSongChoice(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
