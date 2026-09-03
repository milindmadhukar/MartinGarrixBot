package dashboard

import (
	"os"
	"testing"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/STMPDBot/dashboard/session"
)

// TestMintSmokeCookie is a manual helper, not a real test: it prints a signed
// session cookie so the authenticated pages can be exercised with curl against
// a local instance. It is skipped unless STMPD_MINT_COOKIE is set.
//
// Set STMPD_MINT_OWNER to mint an owner session, which is what the catalogue's edit
// controls are gated on.
//
//	STMPD_MINT_COOKIE=1 STMPD_MINT_SECRET=... STMPD_MINT_GUILD=... go test ./dashboard -run MintSmokeCookie -v
func TestMintSmokeCookie(t *testing.T) {
	if os.Getenv("STMPD_MINT_COOKIE") == "" {
		t.Skip("set STMPD_MINT_COOKIE to mint a development session cookie")
	}

	secret := os.Getenv("STMPD_MINT_SECRET")
	if secret == "" {
		t.Fatal("STMPD_MINT_SECRET is required")
	}
	guild, err := snowflake.Parse(os.Getenv("STMPD_MINT_GUILD"))
	if err != nil {
		t.Fatalf("STMPD_MINT_GUILD: %v", err)
	}

	c := session.NewCodec(secret, time.Hour, false)
	s, err := c.New(1001, "smoke", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Eligible = []snowflake.ID{guild}
	s.Owner = os.Getenv("STMPD_MINT_OWNER") != ""

	value, err := c.Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("COOKIE=%s", value)
	// The catalogue's write routes are CSRF-guarded, and curl has no page to read the
	// token off, so print it alongside the cookie.
	t.Logf("CSRF=%s", s.CSRF)
}
