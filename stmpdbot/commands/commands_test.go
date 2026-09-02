package commands

// Every command Discord is told about must have a route that actually matches.
//
// This exists because the failure mode is silent. handler.Mux.Handle returns nil when
// no route matches, so an unrouted command is neither answered nor logged: Discord
// shows "The application did not respond" and the bot log says nothing at all. The
// disgo v0.18 -> v0.19 upgrade changed splitPath such that a sub-mux mounted at "/"
// stopped matching, which silently killed /ping, /avatar, /8ball, /lyrics and /quiz
// for a day before anyone tried one.
//
// Match is the same predicate the dispatcher uses, so this catches a routing gap
// without needing a gateway or a live interaction.

import (
	"testing"

	"github.com/disgoorg/disgo/discord"
)

func TestEveryRegisteredCommandIsRouted(t *testing.T) {
	t.Parallel()

	// The handlers are closures over the bot and nothing dereferences it at
	// registration time, so a nil bot is enough to build the router.
	mux := SetupHandlers(nil)

	for _, cmd := range Commands {
		slash, ok := cmd.(discord.SlashCommandCreate)
		if !ok {
			continue
		}

		t.Run(slash.Name, func(t *testing.T) {
			path := "/" + slash.Name
			if !mux.Match(path, discord.InteractionTypeApplicationCommand,
				int(discord.ApplicationCommandTypeSlash)) {
				t.Errorf("%s is registered with Discord but no route matches it; "+
					"invoking it would fail silently with no log", path)
			}
		})
	}
}

// A command with autocomplete options is useless if the autocomplete interaction is
// not routed too -- and that failure is equally silent.
func TestAutocompleteCommandsAreRouted(t *testing.T) {
	t.Parallel()

	mux := SetupHandlers(nil)

	for _, name := range []string{"/lyrics", "/links", "/config"} {
		t.Run(name, func(t *testing.T) {
			if !mux.Match(name, discord.InteractionTypeAutocomplete,
				int(discord.ApplicationCommandTypeSlash)) {
				t.Errorf("%s has no autocomplete route", name)
			}
		})
	}
}
