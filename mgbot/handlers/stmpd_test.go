package handlers

// In-package: nextFlightPayload, extractJSONArray and upscaleArtwork are all
// unexported.

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNextFlightPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"no markers", `<html><body>nothing</body></html>`, ""},
		{"empty body", "", ""},
		{
			name: "a single chunk is decoded",
			body: `<script>self.__next_f.push([1,"hello"])</script>`,
			want: "hello",
		},
		{
			name: "chunks are concatenated in order",
			body: `<script>self.__next_f.push([1,"hello "])</script>` +
				`<script>self.__next_f.push([1,"world"])</script>`,
			want: "hello world",
		},
		{
			name: "escaped quotes inside a chunk survive",
			body: `<script>self.__next_f.push([1,"say \"hi\""])</script>`,
			want: `say "hi"`,
		},
		{
			name: "escaped backslashes do not end the chunk early",
			body: `<script>self.__next_f.push([1,"back\\slash"])</script>`,
			want: `back\slash`,
		},
		{
			name: "escape sequences are decoded",
			body: `<script>self.__next_f.push([1,"line\nbreak"])</script>`,
			want: "line\nbreak",
		},
		{
			// push([0]) carries no string; the scanner sees ']' before any quote
			// and moves on.
			name: "a push with no string payload is skipped",
			body: `<script>self.__next_f.push([0])</script>` +
				`<script>self.__next_f.push([1,"kept"])</script>`,
			want: "kept",
		},
		{
			name: "an unterminated chunk stops the scan",
			body: `<script>self.__next_f.push([1,"never closed`,
			want: "",
		},
		{
			name: "a good chunk before an unterminated one is kept",
			body: `<script>self.__next_f.push([1,"kept"])</script>` +
				`<script>self.__next_f.push([1,"never closed`,
			want: "kept",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := nextFlightPayload(tt.body); got != tt.want {
				t.Errorf("nextFlightPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractJSONArray(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		payload         string
		key             string
		want            string
		wantErrContains string
	}{
		{
			name:    "a simple array",
			payload: `{"initialReleases":[1,2,3],"total":3}`,
			key:     `"initialReleases"`,
			want:    `[1,2,3]`,
		},
		{
			name:    "an empty array",
			payload: `{"initialReleases":[]}`,
			key:     `"initialReleases"`,
			want:    `[]`,
		},
		{
			name:    "nested arrays are balanced, not stopped at the first bracket",
			payload: `{"initialReleases":[[1,2],[3,4]]}`,
			key:     `"initialReleases"`,
			want:    `[[1,2],[3,4]]`,
		},
		{
			// The reason the scanner tracks string state: a bracket inside a
			// title must not be read as structure.
			name:    "brackets inside strings are data",
			payload: `{"initialReleases":[{"title":"Us [Reimagined]"}]}`,
			key:     `"initialReleases"`,
			want:    `[{"title":"Us [Reimagined]"}]`,
		},
		{
			name:    "an escaped quote does not end the string",
			payload: `{"initialReleases":[{"title":"say \"]\" now"}]}`,
			key:     `"initialReleases"`,
			want:    `[{"title":"say \"]\" now"}]`,
		},
		{
			name:    "an escaped backslash before a quote still closes the string",
			payload: `{"initialReleases":[{"t":"back\\"},{"u":2}]}`,
			key:     `"initialReleases"`,
			want:    `[{"t":"back\\"},{"u":2}]`,
		},
		{
			name:            "the key is absent",
			payload:         `{"other":[1]}`,
			key:             `"initialReleases"`,
			wantErrContains: "not found in payload",
		},
		{
			name:            "no array follows the key",
			payload:         `{"initialReleases":null}`,
			key:             `"initialReleases"`,
			wantErrContains: "no array found after key",
		},
		{
			name:            "the array is never closed",
			payload:         `{"initialReleases":[1,2`,
			key:             `"initialReleases"`,
			wantErrContains: "unterminated array after key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := extractJSONArray(tt.payload, tt.key)

			if tt.wantErrContains != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got none", tt.wantErrContains)
				}
				if !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error = %q, want it to contain %q", err, tt.wantErrContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("extractJSONArray() = %q, want %q", got, tt.want)
			}
			if !json.Valid([]byte(got)) {
				t.Errorf("extractJSONArray() returned invalid JSON: %q", got)
			}
		})
	}
}

func TestUpscaleArtwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "both dimensions are raised",
			raw:  "https://cdn.sanity.io/images/x/prod/a.jpg?w=400&h=400",
			want: "https://cdn.sanity.io/images/x/prod/a.jpg?h=1000&w=1000",
		},
		{
			name: "other parameters are kept",
			raw:  "https://cdn.sanity.io/images/x/prod/a.jpg?w=400&h=400&fit=crop",
			want: "https://cdn.sanity.io/images/x/prod/a.jpg?fit=crop&h=1000&w=1000",
		},
		{
			name: "a width alone gains a height",
			raw:  "https://cdn.sanity.io/images/x/prod/a.jpg?w=400",
			want: "https://cdn.sanity.io/images/x/prod/a.jpg?h=1000&w=1000",
		},
		{
			// Without sizing parameters this is not a CDN rendition, so it is
			// left exactly as-is rather than having parameters invented for it.
			name: "a URL with no sizing parameters is untouched",
			raw:  "https://cdn.sanity.io/images/x/prod/a.jpg",
			want: "https://cdn.sanity.io/images/x/prod/a.jpg",
		},
		{
			name: "an unrelated query is untouched",
			raw:  "https://cdn.sanity.io/images/x/prod/a.jpg?fit=crop",
			want: "https://cdn.sanity.io/images/x/prod/a.jpg?fit=crop",
		},
		{"empty stays empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := upscaleArtwork(tt.raw); got != tt.want {
				t.Errorf("upscaleArtwork(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// The two parsers have to work together on the real page shape: reassemble the
// streamed chunks, then find the releases array inside the result.
func TestStmpdParsers_AgainstTheRealPageShape(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "stmpd_flight.html")

	payload := nextFlightPayload(body)
	if payload == "" {
		t.Fatal("no payload was reassembled from the flight chunks")
	}

	rawArray, err := extractJSONArray(payload, `"initialReleases"`)
	if err != nil {
		t.Fatalf("failed to locate the releases array: %v", err)
	}

	var parsed []stmpdArchiveRelease
	if err := json.Unmarshal([]byte(rawArray), &parsed); err != nil {
		t.Fatalf("releases array is not valid JSON: %v", err)
	}

	if len(parsed) != 4 {
		t.Fatalf("got %d releases, want 4", len(parsed))
	}

	// The bracketed title is the case extractJSONArray's string tracking exists
	// for; if the scan stopped early this would be missing.
	if parsed[1].Title != "Us [Reimagined]" {
		t.Errorf("second release title = %q, want %q", parsed[1].Title, "Us [Reimagined]")
	}

	// A malformed push sits between the chunks; it must not truncate the payload.
	if parsed[3].Title != "Won't Let You Go" {
		t.Errorf("fourth release title = %q, want %q; a malformed push may have "+
			"truncated the payload", parsed[3].Title, "Won't Let You Go")
	}
}
