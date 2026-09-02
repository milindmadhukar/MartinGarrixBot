package utils

import (
	"context"
	"errors"
	"log/slog"
)

// Filling in lyrics has one hard part, and it is not fetching them. It is deciding
// whether the words that came back belong to the song they are about to be written
// onto. A cover version, a live cut or an unrelated song with the same title shares
// everything a search can see, and hanging the wrong words on a song is the one failure
// of this whole pass that nobody would ever notice.
//
// The rules live here rather than in either caller, so the one-off sweep and the daily
// watcher cannot drift apart on the question that matters.

// LyricsQuery is what a row knows about itself when asking LRCLIB.
type LyricsQuery struct {
	// Title is the answerable form of the name -- songs.normalized_name, or what
	// utils.NormalizedTitle derives from the stored name. LRCLIB indexes song titles,
	// not credit strings: "Sun Is Never Going Down" resolves and "Sun Is Never Going
	// Down (feat. Dawn Golden)" does not.
	Title string
	// Name is the stored name, used only for verification.
	Name    string
	Artists string
	Album   string
	// LengthMs is 0 when the row does not know its own duration.
	LengthMs int32
}

// LyricsOutcome says what to do with a row after asking LRCLIB about it.
type LyricsOutcome int

const (
	// LyricsFound: a record was returned and verified. Write its words.
	LyricsFound LyricsOutcome = iota
	// LyricsInstrumental: LRCLIB says this recording has no words, from an exact
	// lookup that verified. Set is_instrumental and stop asking.
	LyricsInstrumental
	// LyricsMissing: LRCLIB answered and had nothing usable. Costs the row one of its
	// retries.
	LyricsMissing
	// LyricsRejected: something came back but did not describe this row. Stamps the
	// attempt without spending a retry -- the row is fine, the candidate was not.
	LyricsRejected
)

// LyricsResult is the decision plus the record it was made from.
type LyricsResult struct {
	Outcome LyricsOutcome
	Record  *LrclibRecord
}

// durationToleranceMs is how far a candidate's running time may differ from the row's.
//
// This is the guard SameRecording cannot provide. A cover, a live version and a
// sped-up edit all share the title and the artist exactly and differ only in length.
// Five seconds absorbs the disagreement between a store's rounded duration and
// Beatport's exact one without admitting a different recording.
const durationToleranceMs = 5000

// FetchLyrics asks LRCLIB about one row and decides whether to believe the answer.
//
// Exact lookup first: it is one request, and the only result precise enough to act on
// an instrumental flag. Search is the fallback, used both when the lookup finds nothing
// and when what it found does not describe this row -- search answers with its nearest
// guess rather than with nothing, so it is never trusted on its own.
func FetchLyrics(ctx context.Context, client *LrclibClient, q LyricsQuery) (LyricsResult, error) {
	// rejected remembers a record that came back but did not describe this row, so
	// that a run which finds nothing else can say "we looked and it was not ours"
	// rather than "LRCLIB has nothing" -- only the second costs the row a retry.
	var rejected *LrclibRecord

	rec, err := client.Get(ctx, q.Title, q.Artists, q.Album, int(q.LengthMs/1000))
	switch {
	case err == nil && rec != nil:
		switch {
		case !trustLyrics(q, *rec):
			// Not ours. Search anyway: an exact lookup answers with one record, and
			// the reason it failed is often that the row's duration disagrees with
			// the pressing LRCLIB happens to hold rather than that it has nothing.
			rejected = rec
		case rec.Instrumental:
			// Only an exact lookup may retire a song as an instrumental. LRCLIB's
			// flag is community-supplied and sometimes only means nobody transcribed
			// it yet, and a wrongly flagged row leaves the quiz permanently and
			// silently.
			return LyricsResult{Outcome: LyricsInstrumental, Record: rec}, nil
		case rec.Plain() == "":
			return LyricsResult{Outcome: LyricsMissing, Record: rec}, nil
		default:
			return LyricsResult{Outcome: LyricsFound, Record: rec}, nil
		}

	case errors.Is(err, ErrLrclibNotFound):
		// Fall through to search.

	default:
		// A rate limit or a transport failure says nothing about this row, so it is
		// returned rather than turned into a miss.
		return LyricsResult{}, err
	}

	results, err := client.Search(ctx, q.Title, q.Artists)
	if err != nil && !errors.Is(err, ErrLrclibNotFound) {
		return LyricsResult{}, err
	}

	for i := range results {
		candidate := results[i]
		if !trustLyrics(q, candidate) {
			continue
		}
		// An instrumental found by search is not acted on -- see the note above --
		// but it is not a miss either, so it does not cost the row a retry.
		if candidate.Instrumental || candidate.Plain() == "" {
			rejected = &candidate
			continue
		}
		return LyricsResult{Outcome: LyricsFound, Record: &candidate}, nil
	}

	if rejected != nil {
		return LyricsResult{Outcome: LyricsRejected, Record: rejected}, nil
	}

	// LRCLIB answered and had nothing for this row.
	return LyricsResult{Outcome: LyricsMissing}, nil
}

// trustLyrics reports whether a record describes the row it was fetched for.
//
// SameRecording does the heavy lifting and is the same check the Apple enrichment
// uses: base titles must be equal after normalization, artists need only overlap --
// which is what lets a row credited to three people match a record LRCLIB files under
// one. Duration is the part it cannot judge.
func trustLyrics(q LyricsQuery, rec LrclibRecord) bool {
	if !SameRecording(q.Title, q.Artists, rec.TrackName, rec.ArtistName) &&
		!SameRecording(q.Name, q.Artists, rec.TrackName, rec.ArtistName) {
		return false
	}

	if q.LengthMs > 0 && rec.LengthMs() > 0 {
		diff := rec.LengthMs() - q.LengthMs
		if diff < 0 {
			diff = -diff
		}
		if diff > durationToleranceMs {
			slog.Debug("Rejected an LRCLIB candidate on duration",
				slog.String("name", q.Name), slog.Int("stored_ms", int(q.LengthMs)),
				slog.Int("candidate_ms", int(rec.LengthMs())))
			return false
		}
	}

	return true
}
