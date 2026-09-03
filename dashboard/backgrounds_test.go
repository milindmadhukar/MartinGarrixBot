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

// TestBackgroundsUploadPageExecutes covers the upload page's states: a fresh
// visit, a validation problem, the post-upload confirmation, and both an
// empty and populated catalogue -- the populated case is what renders the
// delete controls.
func TestBackgroundsUploadPageExecutes(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}
	page := r.pages["backgrounds-upload"]

	populated := []backgroundOption{
		{ID: 1, Filename: "a.png", URL: "/backgrounds/file/a.png"},
		{ID: 2, Filename: "b.png", URL: "/backgrounds/file/b.png"},
	}

	for _, tc := range []struct {
		name string
		data map[string]any
	}{
		{"fresh, empty catalogue", map[string]any{"Problem": "", "Saved": false, "Backgrounds": []backgroundOption{}}},
		{"problem", map[string]any{"Problem": "That file is not a readable image.", "Saved": false, "Backgrounds": []backgroundOption{}}},
		{"saved, populated catalogue", map[string]any{"Problem": "", "Saved": true, "Backgrounds": populated}},
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

// TestBackgroundsListFragmentExecutes covers what handleBackgroundDelete
// actually re-renders: just the catalogue list, both empty (the last
// background just got deleted) and populated (with delete forms present).
func TestBackgroundsListFragmentExecutes(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}
	page := r.pages["backgrounds-upload"]

	for _, tc := range []struct {
		name string
		data map[string]any
	}{
		{"empty", map[string]any{"Backgrounds": []backgroundOption{}}},
		{"populated", map[string]any{"Backgrounds": []backgroundOption{
			{ID: 1, Filename: "a.png", URL: "/backgrounds/file/a.png"},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &pageData{Data: tc.data}

			var buf bytes.Buffer
			if err := page.ExecuteTemplate(&buf, "backgrounds-list", p); err != nil {
				t.Fatalf("backgrounds-list failed to execute: %v", err)
			}
			if buf.Len() == 0 {
				t.Fatal("backgrounds-list rendered nothing")
			}
			if tc.name == "populated" && !strings.Contains(buf.String(), "/backgrounds/1/delete") {
				t.Error("delete form for the background is missing")
			}
		})
	}
}
