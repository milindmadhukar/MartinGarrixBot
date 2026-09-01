package session

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestRoundTrip(t *testing.T) {
	c := NewCodec(testSecret, time.Hour, true)

	want, err := c.New(123456789, "milind", "abc123")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want.Eligible = []snowflake.ID{300, 100, 200}

	encoded, err := c.Encode(want)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	got, err := c.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.UserID != want.UserID || got.Username != want.Username {
		t.Errorf("identity did not survive the round trip: %+v", got)
	}
	// Encode sorts, so the same set always produces the same bytes.
	if got.Eligible[0] != 100 || got.Eligible[2] != 300 {
		t.Errorf("Eligible not sorted: %v", got.Eligible)
	}
}

// TestTamperedPayloadRejected is the whole point of signing the cookie: an
// attacker who edits the guild list must not gain access to those guilds.
func TestTamperedPayloadRejected(t *testing.T) {
	c := NewCodec(testSecret, time.Hour, true)

	sess, _ := c.New(1, "user", "")
	sess.Eligible = []snowflake.ID{111}
	encoded, _ := c.Encode(sess)

	payload, sig, _ := strings.Cut(encoded, ".")

	// Flip one character of the payload, keeping the original signature.
	flipped := []byte(payload)
	if flipped[0] == 'A' {
		flipped[0] = 'B'
	} else {
		flipped[0] = 'A'
	}

	if _, err := c.Decode(string(flipped) + "." + sig); err == nil {
		t.Fatal("a tampered payload was accepted")
	}
}

func TestWrongKeyRejected(t *testing.T) {
	signer := NewCodec(testSecret, time.Hour, true)
	verifier := NewCodec("fedcba9876543210fedcba9876543210", time.Hour, true)

	sess, _ := signer.New(1, "user", "")
	encoded, _ := signer.Encode(sess)

	if _, err := verifier.Decode(encoded); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Decode with the wrong key = %v, want ErrBadSignature", err)
	}
}

func TestMalformedRejected(t *testing.T) {
	c := NewCodec(testSecret, time.Hour, true)
	for _, value := range []string{"", "nodot", ".", "a.b", "!!!.???"} {
		if _, err := c.Decode(value); err == nil {
			t.Errorf("Decode(%q) succeeded, want an error", value)
		}
	}
}

// TestExpiryComesFromTheSignedPayload matters because Max-Age is only a hint
// the client may ignore. A client that keeps sending an old cookie must still
// be rejected.
func TestExpiryComesFromTheSignedPayload(t *testing.T) {
	c := NewCodec(testSecret, time.Hour, true)

	sess, _ := c.New(1, "user", "")
	sess.ExpiresAt = time.Now().Add(-time.Minute).Unix()

	encoded, _ := c.Encode(sess)
	if _, err := c.Decode(encoded); !errors.Is(err, ErrExpired) {
		t.Fatalf("Decode of an expired session = %v, want ErrExpired", err)
	}
}

func TestAdministers(t *testing.T) {
	s := Session{Eligible: []snowflake.ID{10, 20, 30}}
	if !s.Administers(20) {
		t.Error("should administer a guild in the eligible set")
	}
	if s.Administers(40) {
		t.Error("should not administer a guild outside the eligible set")
	}
	if (Session{}).Administers(10) {
		t.Error("an empty session should administer nothing")
	}
}

func TestGuildsStale(t *testing.T) {
	fresh := Session{GuildsAt: time.Now().Unix()}
	if fresh.GuildsStale(10 * time.Minute) {
		t.Error("a just-derived guild list should not be stale")
	}

	old := Session{GuildsAt: time.Now().Add(-time.Hour).Unix()}
	if !old.GuildsStale(10 * time.Minute) {
		t.Error("an hour-old guild list should be stale at a 10m TTL")
	}
}

// TestMissingGuildsAreCapped keeps the cookie under the 4KB browser limit; it
// rides on every request, including static assets.
func TestMissingGuildsAreCapped(t *testing.T) {
	c := NewCodec(testSecret, time.Hour, true)

	sess, _ := c.New(1, "user", "")
	for i := range 25 {
		sess.Missing = append(sess.Missing, MissingGuild{
			ID:   snowflake.ID(i),
			Name: strings.Repeat("x", 200),
		})
	}

	encoded, err := c.Encode(sess)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := c.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if len(decoded.Missing) != maxMissingGuilds {
		t.Errorf("kept %d missing guilds, want %d", len(decoded.Missing), maxMissingGuilds)
	}
	for _, m := range decoded.Missing {
		if len(m.Name) > maxMissingNameLen {
			t.Errorf("name not truncated: %d chars", len(m.Name))
		}
	}
	if len(encoded) > 4000 {
		t.Errorf("cookie is %d bytes, too close to the 4KB limit", len(encoded))
	}
}

func TestCookieFlags(t *testing.T) {
	t.Run("secure", func(t *testing.T) {
		c := NewCodec(testSecret, time.Hour, true)
		sess, _ := c.New(1, "user", "")

		w := httptest.NewRecorder()
		if err := c.Write(w, sess); err != nil {
			t.Fatalf("Write: %v", err)
		}

		cookies := w.Result().Cookies()
		byName := map[string]*http.Cookie{}
		for _, ck := range cookies {
			byName[ck.Name] = ck
		}

		s, ok := byName[SecureCookieName]
		if !ok {
			t.Fatalf("expected the __Host- prefixed cookie, got %v", byName)
		}
		if !s.Secure || !s.HttpOnly || s.Path != "/" || s.Domain != "" {
			t.Errorf("__Host- prefix requires Secure, Path=/ and no Domain; got %+v", s)
		}
		// Lax, not Strict: the OAuth callback is a top-level cross-site GET.
		if s.SameSite != http.SameSiteLaxMode {
			t.Errorf("SameSite = %v, want Lax", s.SameSite)
		}

		csrf, ok := byName[SecureCSRFCookieName]
		if !ok {
			t.Fatal("expected a CSRF mirror cookie")
		}
		if csrf.HttpOnly {
			t.Error("the CSRF cookie must be readable so htmx can echo it back")
		}
	})

	t.Run("insecure dev", func(t *testing.T) {
		c := NewCodec(testSecret, time.Hour, false)
		sess, _ := c.New(1, "user", "")

		w := httptest.NewRecorder()
		_ = c.Write(w, sess)

		for _, ck := range w.Result().Cookies() {
			if strings.HasPrefix(ck.Name, "__Host-") {
				t.Errorf("%q uses the __Host- prefix without Secure, which browsers reject", ck.Name)
			}
			if ck.Secure {
				t.Errorf("%q is Secure in a plain-HTTP dev setup", ck.Name)
			}
		}
	})
}

func TestReadAndClear(t *testing.T) {
	c := NewCodec(testSecret, time.Hour, false)
	sess, _ := c.New(42, "user", "")

	w := httptest.NewRecorder()
	_ = c.Write(w, sess)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	for _, ck := range w.Result().Cookies() {
		r.AddCookie(ck)
	}

	got, err := c.Read(r)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.UserID != 42 {
		t.Errorf("UserID = %d, want 42", got.UserID)
	}

	if _, err := c.Read(httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, ErrNoCookie) {
		t.Errorf("Read without a cookie = %v, want ErrNoCookie", err)
	}

	cleared := httptest.NewRecorder()
	c.Clear(cleared)
	for _, ck := range cleared.Result().Cookies() {
		if ck.MaxAge >= 0 {
			t.Errorf("Clear left %q with MaxAge %d", ck.Name, ck.MaxAge)
		}
	}
}

// TestStateIsSingleUse checks that reading the OAuth state also clears it, so a
// replayed callback cannot reuse it.
func TestStateIsSingleUse(t *testing.T) {
	c := NewCodec(testSecret, time.Hour, false)

	w := httptest.NewRecorder()
	c.WriteState(w, "the-state")

	r := httptest.NewRequest(http.MethodGet, "/auth/callback", nil)
	for _, ck := range w.Result().Cookies() {
		r.AddCookie(ck)
	}

	out := httptest.NewRecorder()
	if got := c.ReadState(out, r); got != "the-state" {
		t.Errorf("ReadState = %q, want %q", got, "the-state")
	}

	var cleared bool
	for _, ck := range out.Result().Cookies() {
		if ck.Name == StateCookieName && ck.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("ReadState should expire the state cookie")
	}

	// A request with no cookie must yield an empty string, which the caller
	// treats as a mismatch rather than as a match against an empty state.
	if got := c.ReadState(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Errorf("ReadState without a cookie = %q, want empty", got)
	}
}

func TestRandomTokenIsDistinct(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		token, err := RandomToken()
		if err != nil {
			t.Fatalf("RandomToken: %v", err)
		}
		if _, dup := seen[token]; dup {
			t.Fatal("RandomToken repeated a value")
		}
		seen[token] = struct{}{}
	}
}
