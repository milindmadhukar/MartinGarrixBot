// Package session implements the dashboard's login session as an HMAC-signed
// cookie.
//
// There is no server-side session table on purpose. The only thing worth
// revoking would be the Discord access token, and that is never persisted: the
// OAuth callback exchanges the code, reads the user and their guilds, derives
// the eligible set, and drops the token. What is left in the cookie is
// information about the user, shown to that same user, so signing is enough and
// encryption would buy nothing.
package session

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
)

var (
	ErrNoCookie     = errors.New("session: no cookie")
	ErrMalformed    = errors.New("session: malformed value")
	ErrBadSignature = errors.New("session: bad signature")
	ErrExpired      = errors.New("session: expired")
)

const (
	// CookieName is used when Secure is off (plain-HTTP local development).
	CookieName = "stmpd_session"
	// SecureCookieName carries the __Host- prefix, which browsers only accept
	// on a Secure cookie with Path=/ and no Domain. That combination makes the
	// cookie unsettable by any sibling subdomain.
	SecureCookieName = "__Host-stmpd_session"

	StateCookieName       = "stmpd_oauth_state"
	SecureStateCookieName = "__Host-stmpd_oauth_state"

	CSRFCookieName       = "stmpd_csrf"
	SecureCSRFCookieName = "__Host-stmpd_csrf"

	// stateTTL bounds how long a login may sit half-finished.
	stateTTL = 10 * time.Minute
)

// MissingGuild is a guild the user administers that the bot is not in, kept so
// the guild picker can offer a per-guild invite link. Capped and truncated by
// Codec.Encode: this cookie rides on every request, including static assets.
type MissingGuild struct {
	ID   snowflake.ID `json:"i"`
	Name string       `json:"n"`
}

const (
	maxMissingGuilds  = 10
	maxMissingNameLen = 40
)

// Session is the decoded cookie payload. Field names are short because they are
// serialised on every request.
type Session struct {
	UserID     snowflake.ID `json:"u"`
	Username   string       `json:"n"`
	AvatarHash string       `json:"a,omitempty"`
	// Eligible is sorted, so an unchanged set encodes to identical bytes.
	Eligible []snowflake.ID `json:"g"`
	Missing  []MissingGuild `json:"m,omitempty"`
	// SuperAdmin marks a user listed in dashboard.super_admin_ids, who may
	// administer every guild the bot is in. Distinct from a Discord guild's
	// own owner.
	SuperAdmin bool `json:"sa,omitempty"`
	// CSRF is the double-submit token, mirrored into a readable cookie.
	CSRF string `json:"c"`

	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`
	// GuildsAt records when Eligible was derived, so the middleware can force a
	// re-login once it is older than the configured TTL.
	GuildsAt int64 `json:"gat"`
}

// Administers reports whether this session may act on the given guild.
func (s Session) Administers(guildID snowflake.ID) bool {
	return slices.Contains(s.Eligible, guildID)
}

// GuildsStale reports whether the cached guild list is older than ttl. A demoted
// admin keeps access until this returns true, which is why the TTL should be
// short once the dashboard can write.
func (s Session) GuildsStale(ttl time.Duration) bool {
	return time.Since(time.Unix(s.GuildsAt, 0)) > ttl
}

// Codec signs and verifies session values.
type Codec struct {
	key    []byte
	ttl    time.Duration
	secure bool
}

func NewCodec(secret string, ttl time.Duration, secure bool) *Codec {
	return &Codec{key: []byte(secret), ttl: ttl, secure: secure}
}

func (c *Codec) cookieName() string {
	if c.secure {
		return SecureCookieName
	}
	return CookieName
}

func (c *Codec) stateCookieName() string {
	if c.secure {
		return SecureStateCookieName
	}
	return StateCookieName
}

func (c *Codec) csrfCookieName() string {
	if c.secure {
		return SecureCSRFCookieName
	}
	return CSRFCookieName
}

// Encode renders a session as the cookie value `base64(json).base64(mac)`.
//
// The MAC covers the base64 text rather than the raw JSON so that verification
// never has to re-serialise and risk a canonicalisation mismatch between what
// was signed and what is checked.
func (c *Codec) Encode(s Session) (string, error) {
	if len(s.Missing) > maxMissingGuilds {
		s.Missing = s.Missing[:maxMissingGuilds]
	}
	for i := range s.Missing {
		if len(s.Missing[i].Name) > maxMissingNameLen {
			s.Missing[i].Name = s.Missing[i].Name[:maxMissingNameLen]
		}
	}
	slices.Sort(s.Eligible)

	body, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return payload + "." + base64.RawURLEncoding.EncodeToString(c.sign(payload)), nil
}

// Decode verifies the signature and the embedded expiry.
func (c *Codec) Decode(value string) (Session, error) {
	payload, sig, ok := strings.Cut(value, ".")
	if !ok {
		return Session{}, ErrMalformed
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return Session{}, ErrMalformed
	}
	// hmac.Equal is constant time. A plain bytes.Equal here would be a timing
	// oracle on the signature, which is the one comparison that must not leak.
	if !hmac.Equal(got, c.sign(payload)) {
		return Session{}, ErrBadSignature
	}

	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Session{}, ErrMalformed
	}
	var s Session
	if err := json.Unmarshal(body, &s); err != nil {
		return Session{}, ErrMalformed
	}

	// Expiry is enforced from the signed ExpiresAt, never from the cookie's
	// Max-Age: Max-Age is a hint the client is free to ignore, ExpiresAt is
	// inside the signature.
	if time.Now().Unix() >= s.ExpiresAt {
		return Session{}, ErrExpired
	}
	return s, nil
}

func (c *Codec) sign(payload string) []byte {
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

// New builds a session with timestamps and a fresh CSRF token filled in.
func (c *Codec) New(userID snowflake.ID, username, avatar string) (Session, error) {
	token, err := RandomToken()
	if err != nil {
		return Session{}, err
	}
	now := time.Now()
	return Session{
		UserID:     userID,
		Username:   username,
		AvatarHash: avatar,
		CSRF:       token,
		IssuedAt:   now.Unix(),
		ExpiresAt:  now.Add(c.ttl).Unix(),
		GuildsAt:   now.Unix(),
	}, nil
}

// Write sets the session cookie and the readable CSRF mirror.
func (c *Codec) Write(w http.ResponseWriter, s Session) error {
	value, err := c.Encode(s)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     c.cookieName(),
		Value:    value,
		Path:     "/",
		MaxAge:   int(c.ttl.Seconds()),
		HttpOnly: true,
		Secure:   c.secure,
		// Lax, not Strict: the OAuth callback is a top-level cross-site GET
		// from discord.com, and Strict would withhold the cookie on it.
		SameSite: http.SameSiteLaxMode,
	})

	// Readable by design: htmx has to echo it back in X-CSRF-Token. Its secrecy
	// from JavaScript is not what makes double-submit work -- the attacker's
	// origin being unable to read it is.
	http.SetCookie(w, &http.Cookie{
		Name:     c.csrfCookieName(),
		Value:    s.CSRF,
		Path:     "/",
		MaxAge:   int(c.ttl.Seconds()),
		HttpOnly: false,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Read returns the verified session on the request, if any.
func (c *Codec) Read(r *http.Request) (Session, error) {
	cookie, err := r.Cookie(c.cookieName())
	if err != nil {
		return Session{}, ErrNoCookie
	}
	return c.Decode(cookie.Value)
}

// Clear expires the session and CSRF cookies.
func (c *Codec) Clear(w http.ResponseWriter) {
	for _, name := range []string{c.cookieName(), c.csrfCookieName()} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == c.cookieName(),
			Secure:   c.secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

// WriteState stores the OAuth state parameter in a short-lived cookie.
//
// This is deliberately not disgo's StateController, which is an in-memory TTL
// map: that loses every pending login on restart and cannot work across more
// than one replica. A cookie is stateless and survives both.
func (c *Codec) WriteState(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     c.stateCookieName(),
		Value:    state,
		Path:     "/",
		MaxAge:   int(stateTTL.Seconds()),
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ReadState returns the stored state and clears the cookie, so a state value is
// only ever usable once.
func (c *Codec) ReadState(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(c.stateCookieName())
	http.SetCookie(w, &http.Cookie{
		Name:     c.stateCookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
	})
	if err != nil {
		return ""
	}
	return cookie.Value
}

// RandomToken returns 32 bytes of crypto/rand as base64url text.
func RandomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
