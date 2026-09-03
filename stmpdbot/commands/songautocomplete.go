package commands

import (
	"context"
	"log/slog"

	"github.com/disgoorg/disgo/handler"
	"github.com/milindmadhukar/STMPDBot/utils"
)

// /links and /lyrics both autocomplete a song name, and their handlers were
// byte-for-byte identical apart from which two queries they called: one for the empty
// input, one for a search term.
//
// What is NOT shared is the queries, and that is deliberate rather than an oversight.
// /links must list renditions -- a remix is its own recording with its own streaming
// links, and hiding it makes those links unreachable through the bot. /lyrics must not
// -- every rendition of a song has the same words, so listing them fills Discord's
// 20-choice limit with the same page over and over.

// songLoader fetches the choices for an autocomplete input. An empty input means the
// member has typed nothing yet and wants a sample to pick from.
type songLoader func(ctx context.Context, input string) ([]utils.SongChoice, error)

// songAutocomplete is the shared shell: read the input, load, render.
func songAutocomplete(command string, load songLoader) handler.AutocompleteHandler {
	return func(e *handler.AutocompleteEvent) error {
		choices, err := load(e.Ctx, e.Data.String("song"))
		if err != nil {
			slog.Error("Failed to load song choices for autocomplete",
				slog.String("command", command), slog.Any("err", err))
			return err
		}
		return e.AutocompleteResult(utils.BuildSongChoices(choices))
	}
}

// songChoicesFrom adapts one of sqlc's row shapes to the choice list.
//
// Generic because sqlc generates a distinct struct per query even when the selected
// columns are identical, so there is no shared type to range over.
func songChoicesFrom[T any](rows []T, choice func(T) utils.SongChoice) []utils.SongChoice {
	if len(rows) == 0 {
		return nil
	}
	choices := make([]utils.SongChoice, 0, len(rows))
	for _, row := range rows {
		choices = append(choices, choice(row))
	}
	return choices
}

// What the member typed is folded into LIKE terms by utils.SearchTerms, which both
// commands call directly. It lives in utils rather than here because the dashboard's
// catalogue search has to agree with them too -- a difference in the folding would make
// one of the three quietly worse at finding things than the others.
//
// It replaced a local "%"+input+"%": that matched the whole phrase contiguously, so
// "matisse sadko dont tell me" found nothing for a row credited to "Matisse & Sadko,
// Aspyer, Matluck", and it passed a member's % and _ straight through as wildcards.
