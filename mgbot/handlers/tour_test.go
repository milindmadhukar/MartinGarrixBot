package handlers

// In-package: nextDataPayload, sanitizeTicketURL and prismicRichText are all
// unexported.

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestPrismicRichText_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   prismicRichText
		want string
	}{
		{"no blocks", prismicRichText{}, ""},
		{"one block", prismicRichText{{Text: "Tomorrowland"}}, "Tomorrowland"},
		{
			name: "blocks are joined with a space",
			in:   prismicRichText{{Text: "Ushuaia"}, {Text: "Ibiza Residency"}},
			want: "Ushuaia Ibiza Residency",
		},
		{
			name: "each block is trimmed",
			in:   prismicRichText{{Text: "  Ultra  "}, {Text: "\tMiami\n"}},
			want: "Ultra Miami",
		},
		{
			name: "blank blocks are dropped rather than doubling the spaces",
			in:   prismicRichText{{Text: "Ultra"}, {Text: "   "}, {Text: "Miami"}},
			want: "Ultra Miami",
		},
		{"only blank blocks", prismicRichText{{Text: ""}, {Text: "  "}}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.in.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNextDataPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		body            string
		want            string
		wantErrContains string
	}{
		{
			name: "extracts the payload",
			body: `<html><script id="__NEXT_DATA__" type="application/json">{"a":1}</script></html>`,
			want: `{"a":1}`,
		},
		{
			name: "handles attributes after the id",
			body: `<script id="__NEXT_DATA__" type="application/json" nonce="x">{"a":1}</script>`,
			want: `{"a":1}`,
		},
		{
			name: "stops at the first closing tag",
			body: `<script id="__NEXT_DATA__">{"a":1}</script><script>ignored</script>`,
			want: `{"a":1}`,
		},
		{
			name: "an empty payload is not an error",
			body: `<script id="__NEXT_DATA__"></script>`,
			want: ``,
		},
		{
			name:            "no script tag",
			body:            `<html><body>nothing here</body></html>`,
			wantErrContains: "no __NEXT_DATA__ script found",
		},
		{
			name:            "the tag is never closed with a bracket",
			body:            `<script id="__NEXT_DATA__"`,
			wantErrContains: "malformed __NEXT_DATA__ script tag",
		},
		{
			name:            "no closing script tag",
			body:            `<script id="__NEXT_DATA__">{"a":1}`,
			wantErrContains: "unterminated __NEXT_DATA__ script tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := nextDataPayload(tt.body)

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
				t.Errorf("nextDataPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNextDataPayload_AgainstTheRealPageShape(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "tour_next_data.html")

	payload, err := nextDataPayload(body)
	if err != nil {
		t.Fatalf("failed to extract the payload: %v", err)
	}

	// The extracted span must be valid JSON, which is the whole point of it.
	var parsed tourNextData
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		t.Fatalf("extracted payload is not valid tour JSON: %v", err)
	}

	if got := len(parsed.Props.PageProps.ToursData); got != 6 {
		t.Errorf("got %d shows in the payload, want 6", got)
	}
}

func TestNextDataPayload_MissingScript(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "tour_next_data_missing.html")

	if _, err := nextDataPayload(body); err == nil {
		t.Fatal("expected an error when the page carries no __NEXT_DATA__ script")
	}
}

// The marker is matched as a plain substring anywhere in the body, so the first
// occurrence wins even inside an HTML comment or another script's source. Next.js
// emits the real tag before any such text in practice, so this is recorded rather
// than fixed — but it is why the fixture's own comment must not mention the marker.
func TestNextDataPayload_MatchesInsideComments(t *testing.T) {
	t.Parallel()

	body := `<!-- see <script id="__NEXT_DATA__"> below --></script>` +
		`<script id="__NEXT_DATA__">{"real":true}</script>`

	got, err := nextDataPayload(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == `{"real":true}` {
		t.Error("the parser now skips HTML comments; update this test and the fixture comment")
	}
}

func TestSanitizeTicketURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty stays empty", "", ""},
		{
			name: "a URL with no query is untouched",
			raw:  "https://tickets.example.com/show/1",
			want: "https://tickets.example.com/show/1",
		},
		{
			name: "a non-analytics query is kept",
			raw:  "https://tickets.example.com/show/1?ref=partner",
			want: "https://tickets.example.com/show/1?ref=partner",
		},
		{
			name: "the Google linker parameter is dropped",
			raw:  "https://tickets.example.com/show/1?_gl=1*abc*_ga*MTIz",
			want: "https://tickets.example.com/show/1",
		},
		{
			name: "every underscore-prefixed parameter is dropped",
			raw:  "https://tickets.example.com/s?_ga=1&_ga_ABC=2&_gcl_au=3&_gcl_aw=4&_fplc=5",
			want: "https://tickets.example.com/s",
		},
		{
			name: "FPAU is dropped despite having no underscore",
			raw:  "https://tickets.example.com/s?FPAU=1.1.99.16",
			want: "https://tickets.example.com/s",
		},
		{
			name: "analytics parameters are stripped and the rest kept",
			raw:  "https://tickets.example.com/s?ref=partner&_gl=1*abc&FPAU=9&id=42",
			want: "https://tickets.example.com/s?id=42&ref=partner",
		},
		{
			name: "the fragment survives",
			raw:  "https://tickets.example.com/s?_gl=1#seating",
			want: "https://tickets.example.com/s#seating",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeTicketURL(tt.raw); got != tt.want {
				t.Errorf("sanitizeTicketURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// url.Values.Encode sorts by key, so the surviving parameters come back in
// alphabetical order rather than their original one. Harmless for opening a
// page, but worth recording since it means the output is not a substring of
// the input.
func TestSanitizeTicketURL_ReordersRemainingParameters(t *testing.T) {
	t.Parallel()

	got := sanitizeTicketURL("https://tickets.example.com/s?z=1&a=2&_gl=3")
	const want = "https://tickets.example.com/s?a=2&z=1"

	if got != want {
		t.Errorf("sanitizeTicketURL() = %q, want %q", got, want)
	}
}

// The parameter names are matched case-sensitively, so a lowercase variant
// survives. Vendors send the uppercase form, so this is recorded rather than
// fixed.
func TestSanitizeTicketURL_IsCaseSensitive(t *testing.T) {
	t.Parallel()

	got := sanitizeTicketURL("https://tickets.example.com/s?fpau=1")
	if !strings.Contains(got, "fpau=1") {
		t.Errorf("sanitizeTicketURL() = %q, expected the lowercase fpau to survive", got)
	}
}

// The point of the function: taogroup's links run past Discord's 512-character
// button limit, and exceeding it rejects the whole message, not just the button.
func TestSanitizeTicketURL_BringsLongVendorLinksUnderDiscordsLimit(t *testing.T) {
	t.Parallel()

	raw := "https://tickets.example.com/event/12345/checkout?" +
		"_gl=1*" + strings.Repeat("a", 200) +
		"*_ga*" + strings.Repeat("b", 200) +
		"&FPAU=1." + strings.Repeat("c", 120) +
		"&id=42"

	if len(raw) <= discordMaxButtonURL {
		t.Fatalf("the test input is only %d characters; it needs to exceed %d",
			len(raw), discordMaxButtonURL)
	}

	got := sanitizeTicketURL(raw)
	if len(got) > discordMaxButtonURL {
		t.Errorf("sanitized URL is still %d characters, over Discord's %d limit: %q",
			len(got), discordMaxButtonURL, got)
	}
	if !strings.Contains(got, "id=42") {
		t.Errorf("sanitized URL dropped a real parameter: %q", got)
	}
}

// An input url.Parse rejects is passed through untouched rather than being
// replaced with something wrong.
func TestSanitizeTicketURL_UnparseableInputIsReturnedAsIs(t *testing.T) {
	t.Parallel()

	const raw = "https://tickets.example.com/\x7f\x00path"

	if _, err := url.Parse(raw); err == nil {
		t.Skip("this input is no longer rejected by url.Parse")
	}
	if got := sanitizeTicketURL(raw); got != raw {
		t.Errorf("sanitizeTicketURL(%q) = %q, want it returned unchanged", raw, got)
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return string(body)
}
