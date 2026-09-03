package dashboard

import (
	"bytes"
	"strings"
	"testing"
)

// TestBackgroundsFormExecutes covers the empty-catalogue state (a fresh
// deploy before SetupBackgrounds has ever run) and the populated state, both
// with and without a selection -- the three shapes handleBackgrounds and
// handleBackgroundsSave actually produce.
func TestBackgroundsFormExecutes(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}
	page := r.pages["backgrounds"]

	for _, tc := range []struct {
		name string
		data map[string]any
	}{
		{"empty catalogue", map[string]any{
			"Options": []backgroundOption{}, "Mode": "random", "Any": false,
		}},
		{"unselected catalogue", map[string]any{
			"Options": []backgroundOption{
				{ID: 1, Filename: "a.png", URL: "/backgrounds/file/a.png"},
				{ID: 2, Filename: "b.png", URL: "/backgrounds/file/b.png"},
			},
			"Mode": "random", "Any": true,
		}},
		{"with a selection, cycle mode, just saved", map[string]any{
			"Options": []backgroundOption{
				{ID: 1, Filename: "a.png", URL: "/backgrounds/file/a.png", Selected: true},
				{ID: 2, Filename: "b.png", URL: "/backgrounds/file/b.png"},
			},
			"Mode": "cycle", "Any": true, "Saved": []string{"backgrounds"},
		}},
		{"validation problem", map[string]any{
			"Options": []backgroundOption{}, "Mode": "random", "Any": false,
			"Problem": "Mode must be Random or Cycle.",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &pageData{GuildID: "1", Data: tc.data}

			var buf bytes.Buffer
			if err := page.ExecuteTemplate(&buf, "backgrounds-form", p); err != nil {
				t.Fatalf("backgrounds-form failed to execute: %v", err)
			}
			if buf.Len() == 0 {
				t.Fatal("backgrounds-form rendered nothing")
			}
		})
	}
}

// TestBackgroundsPageExecutes runs the full (non-fragment) page, which pulls
// in the layout and nav the fragment alone does not exercise.
func TestBackgroundsPageExecutes(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	p := &pageData{GuildID: "1", LoggedIn: true, Data: map[string]any{
		"Options": []backgroundOption{}, "Mode": "random", "Any": false,
	}}

	var buf bytes.Buffer
	if err := r.pages["backgrounds"].ExecuteTemplate(&buf, "page", p); err != nil {
		t.Fatalf("backgrounds page failed to execute: %v", err)
	}
	if !strings.Contains(buf.String(), "Backgrounds") {
		t.Error("the page heading is missing")
	}
}

// TestBackgroundsUploadPageExecutes covers the upload page's three states: a
// fresh visit, a validation problem, and the post-upload confirmation.
func TestBackgroundsUploadPageExecutes(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}
	page := r.pages["backgrounds-upload"]

	for _, tc := range []struct {
		name string
		data map[string]any
	}{
		{"fresh", map[string]any{"Problem": "", "Saved": false}},
		{"problem", map[string]any{"Problem": "That file is not a readable image.", "Saved": false}},
		{"saved", map[string]any{"Problem": "", "Saved": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &pageData{Data: tc.data}

			var buf bytes.Buffer
			if err := page.ExecuteTemplate(&buf, "page", p); err != nil {
				t.Fatalf("backgrounds-upload page failed to execute: %v", err)
			}
			if buf.Len() == 0 {
				t.Fatal("backgrounds-upload page rendered nothing")
			}
		})
	}
}
