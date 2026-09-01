package handlers

// These four functions parse HTML and JSON fetched from sites the bot does not
// control, using manual byte indexing. GetAllTourShows and GetAllStmpdReleases
// are launched from main.go as bare `go` statements with no recover, so a panic
// in any of them takes the whole process down rather than failing one fetch.
// That makes crash-freedom an availability property, not a theoretical one.
//
// `go test` runs the seeds below as ordinary unit tests. To fuzz properly:
//
//	go test -run=^$ -fuzz=FuzzSanitizeTicketURL -fuzztime=60s ./mgbot/handlers/

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

func FuzzNextFlightPayload(f *testing.F) {
	f.Add("")
	f.Add(`<script>self.__next_f.push([1,"hello"])</script>`)
	f.Add(`<script>self.__next_f.push([1,"a"])</script><script>self.__next_f.push([1,"b"])</script>`)
	f.Add(`<script>self.__next_f.push([1,"unterminated`)
	f.Add(`<script>self.__next_f.push([0])</script>`)
	f.Add(`self.__next_f.push([1,`)
	f.Add(`self.__next_f.push([1,"\`)
	f.Add(`self.__next_f.push([1,"\\"])`)
	f.Add(readFixtureString("stmpd_flight.html"))

	f.Fuzz(func(t *testing.T, body string) {
		got := nextFlightPayload(body)

		// Note: the output can be *longer* than the input. json.Unmarshal
		// replaces each invalid UTF-8 byte in a chunk with U+FFFD, which is
		// three bytes, so a run of invalid bytes grows. Fuzzing found that
		// against an earlier len(got) <= len(body) assertion here.
		if !strings.Contains(body, "self.__next_f.push([1,") && got != "" {
			t.Errorf("returned %q from a body with no push marker", got)
		}
	})
}

func FuzzExtractJSONArray(f *testing.F) {
	f.Add(`{"initialReleases":[1,2,3]}`, `"initialReleases"`)
	f.Add(`{"initialReleases":[]}`, `"initialReleases"`)
	f.Add(`{"initialReleases":[{"t":"a[b]c"}]}`, `"initialReleases"`)
	f.Add(`{"initialReleases":[1,2`, `"initialReleases"`)
	f.Add(`{"other":null}`, `"initialReleases"`)
	f.Add(`[[[[[`, `[`)
	f.Add(`{"k":"\"]"}`, `"k"`)
	f.Add("", "")

	f.Fuzz(func(t *testing.T, payload, key string) {
		got, err := extractJSONArray(payload, key)
		if err != nil {
			return
		}

		if !strings.HasPrefix(got, "[") || !strings.HasSuffix(got, "]") {
			t.Errorf("returned %q, which is not bracket-delimited", got)
		}
		if !strings.Contains(payload, got) {
			t.Errorf("returned %q, which is not a substring of the payload", got)
		}
		// Deliberately not asserting json.Valid: the scanner only balances
		// brackets, so it can legitimately return a well-delimited span that is
		// not valid JSON. json.Unmarshal in the caller is what rejects that.
	})
}

func FuzzNextDataPayload(f *testing.F) {
	f.Add(`<script id="__NEXT_DATA__">{"a":1}</script>`)
	f.Add(`<script id="__NEXT_DATA__" type="application/json">{"a":1}</script>`)
	f.Add(`<script id="__NEXT_DATA__">`)
	f.Add(`<script id="__NEXT_DATA__"`)
	f.Add(`id="__NEXT_DATA__">`)
	f.Add("")
	f.Add(readFixtureString("tour_next_data.html"))
	f.Add(readFixtureString("tour_next_data_missing.html"))

	f.Fuzz(func(t *testing.T, body string) {
		got, err := nextDataPayload(body)
		if err != nil {
			return
		}

		if !strings.Contains(body, got) {
			t.Errorf("returned %q, which is not a substring of the body", got)
		}
	})
}

func FuzzSanitizeTicketURL(f *testing.F) {
	f.Add("")
	f.Add("https://tickets.example.com/s")
	f.Add("https://tickets.example.com/s?_gl=1&id=2")
	f.Add("https://tickets.example.com/s?FPAU=1")
	f.Add("https://tickets.example.com/s?a=1&a=2")
	f.Add("https://tickets.example.com/s?%zz=1")
	f.Add("://not a url")
	f.Add("//example.com/x?_ga=1")
	f.Add("mailto:someone@example.com")

	f.Fuzz(func(t *testing.T, raw string) {
		got := sanitizeTicketURL(raw)

		// An input url.Parse rejects is returned unchanged, so it carries no
		// guarantees; only assert on inputs the function actually rewrote.
		if _, err := url.Parse(raw); err != nil {
			return
		}

		parsed, err := url.Parse(got)
		if err != nil {
			t.Fatalf("sanitizeTicketURL(%q) = %q, which no longer parses: %v", raw, got, err)
		}

		for key := range parsed.Query() {
			if strings.HasPrefix(key, "_") || key == "FPAU" {
				t.Errorf("sanitizeTicketURL(%q) = %q, which still carries %q", raw, got, key)
			}
		}
	})
}

// readFixtureString is the seed-corpus variant of readFixture; a seed is not
// worth failing the whole fuzz target over if the file is unreadable.
func readFixtureString(name string) string {
	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		return ""
	}
	return string(body)
}
