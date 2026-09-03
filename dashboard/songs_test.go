package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/STMPDBot/dashboard/session"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

// superAdminSession builds a request carrying a session, super admin or not.
func superAdminSession(t *testing.T, superAdmin bool) *http.Request {
	t.Helper()
	codec := session.NewCodec(strings.Repeat("k", 64), time.Hour, false)
	sess, err := codec.New(1001, "tester", "")
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	sess.SuperAdmin = superAdmin
	sess.Eligible = []snowflake.ID{690950056202731521}

	value, err := codec.Encode(sess)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/songs/1", nil)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: value})
	return r
}

// The catalogue is global: a song row is read by every guild the bot is in, so unlike a
// guild setting there is no scope to contain a mistake to. Browsing is open to any
// authenticated user; writing is not.
//
// This is the highest-value test in this file. Everything else about the catalogue
// pages is cosmetic if a stranger who happens to administer any server the bot is in
// can rewrite the whole label's discography.
func TestSuperAdminOnlyRefusesANonSuperAdmin(t *testing.T) {
	renderer, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}
	opts := testOptions(t)
	opts.SessionSecret = strings.Repeat("k", 64)
	// authed sends a session whose guild list has aged past this back through OAuth, and
	// a zero value means every session is stale on arrival.
	opts.GuildCacheTTL = time.Hour
	s := &Server{
		opts:     opts,
		renderer: renderer,
		sessions: session.NewCodec(opts.SessionSecret, time.Hour, false),
	}

	reached := false
	handler := s.superAdminOnly(func(http.ResponseWriter, *http.Request) { reached = true })

	t.Run("non-super-admin", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, superAdminSession(t, false))

		if reached {
			t.Fatal("a non-super-admin reached a catalogue write handler")
		}
		// 404 rather than 403, for the same reason guildScoped uses 404: a 403 confirms
		// the route is there and that someone else may use it.
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
	})

	t.Run("super-admin", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, superAdminSession(t, true))

		if !reached {
			t.Errorf("a super admin was refused; status = %d", rec.Code)
		}
	})

	t.Run("no session at all", func(t *testing.T) {
		reached = false
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/songs/1", nil))

		if reached {
			t.Fatal("an anonymous request reached a catalogue write handler")
		}
	})
}

// The edit controls are hidden from a non-super-admin as well as refused. Both halves
// are needed: hiding is not a permission check, and refusing while still showing the
// button is a page that looks broken.
func TestSongPageHidesEditControlsFromNonSuperAdmins(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	detail := songDetail{
		Song:     db.Song{ID: 1, Name: "Probe", Artists: "Someone"},
		Links:    songLinks(db.Song{}),
		Lockable: catalogue.LockableFields(),
	}

	for _, tc := range []struct{ superAdmin, wantControls bool }{{false, false}, {true, true}} {
		p := &pageData{
			IsSuperAdmin: tc.superAdmin,
			Data:         map[string]any{"Detail": detail, "Problem": "", "Note": ""},
		}
		var buf bytes.Buffer
		if err := r.pages["song"].ExecuteTemplate(&buf, "song-detail", p); err != nil {
			t.Fatalf("song-detail failed to execute (superAdmin=%v): %v", tc.superAdmin, err)
		}
		body := buf.String()
		if buf.Len() == 0 {
			t.Fatalf("song-detail rendered nothing (superAdmin=%v)", tc.superAdmin)
		}

		hasForm := strings.Contains(body, `hx-post="/songs/1"`)
		if hasForm != tc.wantControls {
			t.Errorf("superAdmin=%v: edit form present = %v, want %v", tc.superAdmin, hasForm, tc.wantControls)
		}
		if !tc.superAdmin && strings.Contains(body, "/unlock") {
			t.Error("a non-super-admin was shown the unlock control")
		}
	}
}

// Catalogue fragments are reached only over htmx, so a nil-deref in one shows up as a
// silently blank page rather than as an error. Executing each against empty data is
// what catches that before production does.
func TestSongFragmentsExecute(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	for _, tc := range []struct {
		page, fragment string
		data           any
	}{
		{"songs", "songs-table", map[string]any{
			"Rows": nil, "Checks": catalogue.Checks(),
			"Pagination": newPagination(1, 50, 0, ""),
			"Filters":    map[string]string{},
		}},
		{"song", "song-detail", map[string]any{
			"Detail":  songDetail{Song: db.Song{ID: 1}, Links: songLinks(db.Song{})},
			"Problem": "", "Note": "",
		}},
		{"songproblems", "problems-list", map[string]any{
			"Failing": nil, "Passing": nil, "Total": 0,
		}},
		{"songmerge", "merge-candidates", map[string]any{
			"Song": db.Song{ID: 1}, "Candidates": nil, "Query": "",
		}},
	} {
		t.Run(tc.fragment, func(t *testing.T) {
			var buf bytes.Buffer
			p := &pageData{IsSuperAdmin: true, Data: tc.data}
			if err := r.pages[tc.page].ExecuteTemplate(&buf, tc.fragment, p); err != nil {
				t.Fatalf("%s failed to execute: %v", tc.fragment, err)
			}
			if buf.Len() == 0 {
				t.Fatalf("%s rendered nothing", tc.fragment)
			}
		})
	}
}
