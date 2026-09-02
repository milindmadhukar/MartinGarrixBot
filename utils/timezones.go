package utils

import (
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
)

// discordMaxAutocompleteChoices is Discord's hard cap on autocomplete choices.
// Returning more does not truncate the list -- it rejects the whole response.
const discordMaxAutocompleteChoices = 25

// commonTimezones backs the timezone autocomplete.
//
// The Go standard library exposes no way to enumerate zone names: time.LoadLocation
// can resolve one, but there is no List(). The runtime image is bare alpine with no
// /usr/share/zoneinfo either, so there is nothing to walk on disk -- the bot resolves
// zones purely from the embedded database that main.go pulls in with _ "time/tzdata".
//
// So this list is hand-picked, and it is only a suggestion source. The command
// handler validates whatever the user actually submits with time.LoadLocation, so a
// zone missing from this list still works if someone types it in full.
var commonTimezones = []string{
	"UTC",
	// Europe
	"Europe/Amsterdam", "Europe/London", "Europe/Dublin", "Europe/Lisbon",
	"Europe/Madrid", "Europe/Paris", "Europe/Brussels", "Europe/Berlin",
	"Europe/Zurich", "Europe/Rome", "Europe/Vienna", "Europe/Prague",
	"Europe/Warsaw", "Europe/Stockholm", "Europe/Oslo", "Europe/Copenhagen",
	"Europe/Helsinki", "Europe/Athens", "Europe/Bucharest", "Europe/Kyiv",
	"Europe/Istanbul", "Europe/Moscow", "Europe/Budapest", "Europe/Belgrade",
	// Americas
	"America/New_York", "America/Toronto", "America/Chicago", "America/Denver",
	"America/Phoenix", "America/Los_Angeles", "America/Vancouver",
	"America/Anchorage", "America/Mexico_City", "America/Bogota", "America/Lima",
	"America/Santiago", "America/Sao_Paulo", "America/Argentina/Buenos_Aires",
	"America/Halifax", "America/Panama",
	// Asia
	"Asia/Jerusalem", "Asia/Dubai", "Asia/Karachi", "Asia/Kolkata",
	"Asia/Kathmandu", "Asia/Dhaka", "Asia/Bangkok", "Asia/Jakarta",
	"Asia/Singapore", "Asia/Kuala_Lumpur", "Asia/Manila", "Asia/Hong_Kong",
	"Asia/Shanghai", "Asia/Taipei", "Asia/Seoul", "Asia/Tokyo", "Asia/Tehran",
	"Asia/Riyadh", "Asia/Baghdad",
	// Africa
	"Africa/Casablanca", "Africa/Lagos", "Africa/Cairo", "Africa/Nairobi",
	"Africa/Johannesburg", "Africa/Accra", "Africa/Tunis",
	// Oceania
	"Australia/Perth", "Australia/Adelaide", "Australia/Brisbane",
	"Australia/Sydney", "Australia/Melbourne", "Pacific/Auckland",
	"Pacific/Fiji", "Pacific/Honolulu",
}

// FilterTimezones returns autocomplete choices matching a partial timezone name.
//
// An empty input returns the head of the list, which is ordered so the first screen
// is UTC plus the European zones the community skews towards.
func FilterTimezones(input string) []discord.AutocompleteChoice {
	needle := strings.ToLower(strings.TrimSpace(input))

	choices := make([]discord.AutocompleteChoice, 0, discordMaxAutocompleteChoices)
	for _, tz := range commonTimezones {
		if len(choices) >= discordMaxAutocompleteChoices {
			break
		}
		if needle != "" && !strings.Contains(strings.ToLower(tz), needle) {
			continue
		}
		choices = append(choices, discord.AutocompleteChoiceString{Name: tz, Value: tz})
	}

	return choices
}

// ValidateTimezone resolves an IANA name, rejecting the two that would silently
// misconfigure a guild.
//
// time.LoadLocation("Local") succeeds and returns whatever zone the process is
// running in -- and stmpdbot.SetupLogger reassigns time.Local to the *log* config's
// timezone at startup. A guild that stored "Local" would therefore post at an hour
// determined by an unrelated setting, and would move if that setting ever changed.
// The empty string is the same trap: LoadLocation("") returns UTC without complaint,
// so an accidental blank would look like a deliberate choice of UTC.
func ValidateTimezone(name string) (*time.Location, bool) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" || strings.EqualFold(trimmed, "Local") {
		return nil, false
	}

	loc, err := time.LoadLocation(trimmed)
	if err != nil {
		return nil, false
	}

	return loc, true
}
