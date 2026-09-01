package mgbot

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestInternalAuthRejectsWrongToken covers the bot's only authenticated network
// surface. Everything behind it lists the guilds the bot is in and the members
// of each, so a hole here is a data leak on the compose network.
func TestInternalAuthRejectsWrongToken(t *testing.T) {
	b := &MartinGarrixBot{}
	b.Cfg.Internal.Secret = "correct-horse-battery-staple"

	var reached bool
	handler := b.internalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name       string
		token      string
		wantStatus int
		wantThru   bool
	}{
		{"correct token", "correct-horse-battery-staple", http.StatusOK, true},
		{"no token", "", http.StatusUnauthorized, false},
		{"wrong token", "hunter2", http.StatusUnauthorized, false},
		// A prefix must not pass: this is what the constant-time compare and
		// the length check are there for.
		{"prefix of the token", "correct-horse", http.StatusUnauthorized, false},
		{"token with a suffix", "correct-horse-battery-staple-extra", http.StatusUnauthorized, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reached = false
			r := httptest.NewRequest(http.MethodGet, "/internal/guilds", nil)
			if tc.token != "" {
				r.Header.Set("X-Internal-Token", tc.token)
			}

			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if reached != tc.wantThru {
				t.Errorf("handler reached = %v, want %v", reached, tc.wantThru)
			}
			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
		})
	}
}

// TestInternalAuthRejectsEverythingWithNoSecret documents the fail-closed
// posture. StartInternalAPI refuses to listen at all in this state, but if the
// middleware were ever reused, an empty configured secret must not mean "any
// empty token is fine".
func TestInternalAuthWithEmptySecret(t *testing.T) {
	b := &MartinGarrixBot{}

	handler := b.internalAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/internal/guilds", nil)
	r.Header.Set("X-Internal-Token", "anything")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 when no secret is configured", w.Code)
	}
}

func TestParseGuildID(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/internal/guilds/690950056202731521", nil)
		r.SetPathValue("guildID", "690950056202731521")

		got, ok := parseGuildID(httptest.NewRecorder(), r)
		if !ok {
			t.Fatal("a valid snowflake should parse")
		}
		if got.String() != "690950056202731521" {
			t.Errorf("guildID = %s", got)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/internal/guilds/nope", nil)
		r.SetPathValue("guildID", "nope")

		w := httptest.NewRecorder()
		if _, ok := parseGuildID(w, r); ok {
			t.Fatal("a non-numeric guild id should not parse")
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", w.Code)
		}
	})
}

func TestDerefString(t *testing.T) {
	if got := derefString(nil); got != "" {
		t.Errorf("derefString(nil) = %q, want empty", got)
	}
	value := "hash"
	if got := derefString(&value); got != "hash" {
		t.Errorf("derefString = %q, want %q", got, value)
	}
}
