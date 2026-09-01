package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/dashboard/session"
)

// TestAdministers pins the permission rule. Getting this wrong in either
// direction is the worst bug this codebase can have: too strict locks out real
// admins, too loose hands a stranger someone else's moderation log.
func TestAdministers(t *testing.T) {
	cases := []struct {
		name  string
		guild discord.OAuth2Guild
		want  bool
	}{
		{
			name:  "owner without the admin bit",
			guild: discord.OAuth2Guild{Owner: true, Permissions: discord.PermissionSendMessages},
			want:  true,
		},
		{
			name:  "administrator",
			guild: discord.OAuth2Guild{Permissions: discord.PermissionAdministrator},
			want:  true,
		},
		{
			name: "administrator among other permissions",
			guild: discord.OAuth2Guild{
				Permissions: discord.PermissionAdministrator | discord.PermissionSendMessages,
			},
			want: true,
		},
		{
			name: "moderator without administrator",
			guild: discord.OAuth2Guild{
				Permissions: discord.PermissionBanMembers | discord.PermissionKickMembers |
					discord.PermissionModerateMembers | discord.PermissionManageMessages,
			},
			want: false,
		},
		{
			name:  "manage guild is not administrator",
			guild: discord.OAuth2Guild{Permissions: discord.PermissionManageGuild},
			want:  false,
		},
		{
			name:  "no permissions",
			guild: discord.OAuth2Guild{},
			want:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := administers(tc.guild); got != tc.want {
				t.Errorf("administers() = %v, want %v", got, tc.want)
			}
		})
	}
}

// botAPIStub serves the internal API's guild list so eligibility can be tested
// without a running bot.
func botAPIStub(t *testing.T, guilds []BotGuild) *BotAPI {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Token") != "shared-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(guilds)
	}))
	t.Cleanup(srv.Close)

	return NewBotAPI(srv.URL, "shared-secret", time.Minute)
}

func TestApplyEligibility(t *testing.T) {
	botGuilds := []BotGuild{
		{ID: "100", Name: "Garrix"},
		{ID: "200", Name: "STMPD"},
	}

	userGuilds := []discord.OAuth2Guild{
		// Administered and the bot is present: eligible.
		{ID: 100, Name: "Garrix", Permissions: discord.PermissionAdministrator},
		// Administered but the bot is absent: invitable, not eligible.
		{ID: 300, Name: "Somewhere else", Owner: true},
		// Bot is present but the user is only a member: neither.
		{ID: 200, Name: "STMPD", Permissions: discord.PermissionSendMessages},
	}

	s := &Server{opts: &Options{}, bots: botAPIStub(t, botGuilds)}
	sess := session.Session{}
	r := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)

	if err := s.applyEligibility(r, &sess, userGuilds); err != nil {
		t.Fatalf("applyEligibility: %v", err)
	}

	if len(sess.Eligible) != 1 || sess.Eligible[0] != 100 {
		t.Errorf("Eligible = %v, want [100]", sess.Eligible)
	}
	if len(sess.Missing) != 1 || sess.Missing[0].ID != 300 {
		t.Errorf("Missing = %v, want the guild the bot is not in", sess.Missing)
	}
	if sess.GuildsAt == 0 {
		t.Error("GuildsAt should be stamped so staleness can be detected")
	}
}

// TestApplyEligibilityOwnerSeesEverything covers the owner_ids escape hatch.
func TestApplyEligibilityOwnerSeesEverything(t *testing.T) {
	botGuilds := []BotGuild{{ID: "100"}, {ID: "200"}, {ID: "300"}}

	s := &Server{opts: &Options{}, bots: botAPIStub(t, botGuilds)}
	sess := session.Session{Owner: true}
	r := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)

	// The owner administers nothing according to Discord.
	if err := s.applyEligibility(r, &sess, nil); err != nil {
		t.Fatalf("applyEligibility: %v", err)
	}

	if len(sess.Eligible) != 3 {
		t.Errorf("an owner should reach all 3 bot guilds, got %v", sess.Eligible)
	}
	if sess.Missing != nil {
		t.Error("an owner has no invitable guilds to show")
	}
}

// TestApplyEligibilityFailsClosed: if the bot cannot be asked which guilds it
// is in, the intersection is unknowable. Granting access anyway would be the
// dangerous default.
func TestApplyEligibilityFailsClosed(t *testing.T) {
	down := NewBotAPI("http://127.0.0.1:1", "secret", time.Minute)
	s := &Server{opts: &Options{}, bots: down}

	sess := session.Session{}
	r := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)

	userGuilds := []discord.OAuth2Guild{
		{ID: 100, Permissions: discord.PermissionAdministrator},
	}
	if err := s.applyEligibility(r, &sess, userGuilds); err == nil {
		t.Fatal("applyEligibility should fail when the bot is unreachable")
	}
	if len(sess.Eligible) != 0 {
		t.Errorf("no guilds should be granted on failure, got %v", sess.Eligible)
	}
}

// TestGuildScopedRejectsForeignGuild is the per-request half of the check: a
// signed session is not enough, the guild in the path has to be in it.
func TestGuildScopedRejectsForeignGuild(t *testing.T) {
	opts := testOptions(t)
	opts.SessionSecret = "0123456789abcdef0123456789abcdef"
	opts.SessionTTL = time.Hour
	opts.GuildCacheTTL = time.Hour

	renderer, err := newRenderer(opts, false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	s := &Server{
		opts:     opts,
		renderer: renderer,
		sessions: session.NewCodec(opts.SessionSecret, opts.SessionTTL, false),
	}

	sess, _ := s.sessions.New(1, "user", "")
	sess.Eligible = []snowflake.ID{100}

	var reached bool
	handler := s.guildScoped(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})

	mux := http.NewServeMux()
	mux.Handle("GET /g/{guildID}", handler)

	newRequest := func(guildID string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/g/"+guildID, nil)
		value, _ := s.sessions.Encode(sess)
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: value})
		return r
	}

	t.Run("own guild is allowed", func(t *testing.T) {
		reached = false
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, newRequest("100"))
		if !reached {
			t.Errorf("handler not reached for an administered guild (status %d)", w.Code)
		}
	})

	t.Run("foreign guild is 404 not 403", func(t *testing.T) {
		reached = false
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, newRequest("999"))
		if reached {
			t.Fatal("handler ran for a guild the user does not administer")
		}
		// 404 rather than 403: a 403 would confirm the guild exists and that
		// the bot is in it.
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("no session redirects to login", func(t *testing.T) {
		reached = false
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/g/100", nil))
		if reached {
			t.Fatal("handler ran without a session")
		}
		if w.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want a redirect", w.Code)
		}
	})

	t.Run("tampered cookie redirects to login", func(t *testing.T) {
		reached = false
		r := httptest.NewRequest(http.MethodGet, "/g/100", nil)
		value, _ := s.sessions.Encode(sess)
		r.AddCookie(&http.Cookie{Name: session.CookieName, Value: value[:len(value)-4] + "AAAA"})

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if reached {
			t.Fatal("handler ran with a tampered cookie")
		}
		if w.Code != http.StatusSeeOther {
			t.Errorf("status = %d, want a redirect", w.Code)
		}
	})
}

// TestStaleGuildListForcesRelogin: a demoted admin keeps access only until the
// cached guild list expires, so the expiry has to actually be enforced.
func TestStaleGuildListForcesRelogin(t *testing.T) {
	opts := testOptions(t)
	opts.SessionSecret = "0123456789abcdef0123456789abcdef"
	opts.SessionTTL = time.Hour
	opts.GuildCacheTTL = time.Minute

	renderer, _ := newRenderer(opts, false)
	s := &Server{
		opts:     opts,
		renderer: renderer,
		sessions: session.NewCodec(opts.SessionSecret, opts.SessionTTL, false),
	}

	sess, _ := s.sessions.New(1, "user", "")
	sess.Eligible = []snowflake.ID{100}
	sess.GuildsAt = time.Now().Add(-time.Hour).Unix()

	var reached bool
	handler := s.authed(func(w http.ResponseWriter, r *http.Request) { reached = true })

	r := httptest.NewRequest(http.MethodGet, "/guilds", nil)
	value, _ := s.sessions.Encode(sess)
	r.AddCookie(&http.Cookie{Name: session.CookieName, Value: value})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if reached {
		t.Fatal("a stale guild list should not reach the handler")
	}
	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want a redirect to /login", w.Code)
	}
}

// TestHtmxUnauthorizedUsesHXRedirect: without this, htmx swaps the login page's
// HTML into whatever table body made the request.
func TestHtmxUnauthorizedUsesHXRedirect(t *testing.T) {
	opts := testOptions(t)
	opts.SessionSecret = "0123456789abcdef0123456789abcdef"
	opts.SessionTTL = time.Hour

	renderer, _ := newRenderer(opts, false)
	s := &Server{
		opts:     opts,
		renderer: renderer,
		sessions: session.NewCodec(opts.SessionSecret, opts.SessionTTL, false),
	}

	r := httptest.NewRequest(http.MethodGet, "/guilds", nil)
	r.Header.Set("HX-Request", "true")

	w := httptest.NewRecorder()
	s.authed(func(http.ResponseWriter, *http.Request) {}).ServeHTTP(w, r)

	if got := w.Header().Get("HX-Redirect"); got != "/login" {
		t.Errorf("HX-Redirect = %q, want /login", got)
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d; htmx only acts on HX-Redirect with a 2xx", w.Code)
	}
}

// TestNoAccessPageRenders covers the refusal path that now replaces Authelia as
// the outer gate. A refused login must render a real page and, critically, must
// not have issued a session cookie on the way.
func TestNoAccessPageRenders(t *testing.T) {
	opts := testOptions(t)
	opts.SessionSecret = "0123456789abcdef0123456789abcdef"
	opts.SessionTTL = time.Hour
	opts.ClientID = "799613778052382720"

	renderer, err := newRenderer(opts, false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}
	s := &Server{
		opts:     opts,
		renderer: renderer,
		sessions: session.NewCodec(opts.SessionSecret, opts.SessionTTL, false),
	}

	t.Run("with invitable guilds", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.renderNoAccess(w, httptest.NewRequest(http.MethodGet, "/auth/callback", nil),
			[]session.MissingGuild{{ID: 123, Name: "Somewhere"}})

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "Somewhere") {
			t.Error("the administered-but-bot-absent guild was not offered an invite")
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("a refused login must not set any cookie")
		}
	})

	t.Run("with nothing at all", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.renderNoAccess(w, httptest.NewRequest(http.MethodGet, "/auth/callback", nil), nil)

		if w.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403", w.Code)
		}
		if !strings.Contains(w.Body.String(), "Add the bot to a server") {
			t.Error("expected the generic invite fallback")
		}
		if len(w.Result().Cookies()) != 0 {
			t.Fatal("a refused login must not set any cookie")
		}
	})
}
