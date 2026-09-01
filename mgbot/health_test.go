package mgbot

// In-package: handleHealth and gatewayDetail are unexported. handleHealth is a
// plain http.HandlerFunc that nil-checks b.DB and b.RadioManager and never
// reads b.Cfg, so a struct literal is enough to drive it.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestGatewayDetail(t *testing.T) {
	t.Parallel()

	if got := gatewayDetail(true); got != "gateway ready" {
		t.Errorf("gatewayDetail(true) = %q, want %q", got, "gateway ready")
	}
	if got := gatewayDetail(false); got != "gateway not ready" {
		t.Errorf("gatewayDetail(false) = %q, want %q", got, "gateway not ready")
	}
}

// callHealth drives the handler and decodes the response.
func callHealth(t *testing.T, b *MartinGarrixBot) (*httptest.ResponseRecorder, HealthResponse) {
	t.Helper()

	rec := httptest.NewRecorder()
	b.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	var resp HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode the health response: %v", err)
	}
	return rec, resp
}

// The database is required, so no pool means unhealthy and a 503. Docker's
// HEALTHCHECK and any uptime monitor both key off that status code.
func TestHandleHealth_UnhealthyWithoutADatabase(t *testing.T) {
	t.Parallel()

	rec, resp := callHealth(t, &MartinGarrixBot{
		IsReady: true,
		Version: "1.2.3",
		Commit:  "abcdef",
	})

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status code = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if resp.Status != "unhealthy" {
		t.Errorf("status = %q, want %q", resp.Status, "unhealthy")
	}
	if resp.Checks["database"].OK {
		t.Error("the database check passed without a connection pool")
	}
	if got := resp.Checks["database"].Detail; got != "no connection pool" {
		t.Errorf("database detail = %q, want %q", got, "no connection pool")
	}
}

func TestHandleHealth_ReportsTheGatewayState(t *testing.T) {
	t.Parallel()

	t.Run("ready", func(t *testing.T) {
		t.Parallel()

		_, resp := callHealth(t, &MartinGarrixBot{IsReady: true})
		if !resp.Checks["discord"].OK {
			t.Error("the discord check failed while the gateway was ready")
		}
		if got := resp.Checks["discord"].Detail; got != "gateway ready" {
			t.Errorf("discord detail = %q, want %q", got, "gateway ready")
		}
	})

	t.Run("not ready", func(t *testing.T) {
		t.Parallel()

		rec, resp := callHealth(t, &MartinGarrixBot{IsReady: false})
		if resp.Checks["discord"].OK {
			t.Error("the discord check passed while the gateway was not ready")
		}
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status code = %d, want %d", rec.Code, http.StatusServiceUnavailable)
		}
	})
}

// Lavalink being down must never fail the check: main() downgrades a Lavalink
// failure to a warning and keeps running with radio disabled, so a 503 here
// would restart a bot that is working.
func TestHandleHealth_LavalinkIsReportedButNeverFatal(t *testing.T) {
	t.Parallel()

	_, resp := callHealth(t, &MartinGarrixBot{IsReady: true})

	if resp.Checks["lavalink"].OK {
		t.Error("the lavalink check passed without a radio manager")
	}
	if !slices.Contains(resp.Degraded, "lavalink") {
		t.Errorf("degraded = %v, want it to list lavalink", resp.Degraded)
	}
	if got := resp.Checks["lavalink"].Detail; got != "optional; radio disabled when down" {
		t.Errorf("lavalink detail = %q, want it to say the check is optional", got)
	}
}

func TestHandleHealth_ResponseShape(t *testing.T) {
	t.Parallel()

	rec, resp := callHealth(t, &MartinGarrixBot{
		IsReady: true,
		Version: "1.2.3",
		Commit:  "abcdef0",
	})

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	// A cached health response would let a monitor read a stale status.
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}

	if resp.Version != "1.2.3" {
		t.Errorf("version = %q, want %q", resp.Version, "1.2.3")
	}
	if resp.Commit != "abcdef0" {
		t.Errorf("commit = %q, want %q", resp.Commit, "abcdef0")
	}

	// startedAt is a package-level var stamped at process start, so only the
	// sign is assertable; never mutate it, the tests run in parallel.
	if resp.UptimeS < 0 {
		t.Errorf("uptime = %d, want it to be non-negative", resp.UptimeS)
	}

	for _, name := range []string{"discord", "database", "lavalink"} {
		if _, ok := resp.Checks[name]; !ok {
			t.Errorf("the response is missing the %q check", name)
		}
	}
}
