package stmpdbot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/milindmadhukar/STMPDBot/utils"
)

// startedAt is stamped at process start so /health can report uptime.
var startedAt = time.Now()

type CheckResult struct {
	OK        bool   `json:"ok"`
	Detail    string `json:"detail,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

type HealthResponse struct {
	Status   string                 `json:"status"`
	Version  string                 `json:"version"`
	Commit   string                 `json:"commit"`
	UptimeS  int64                  `json:"uptime_seconds"`
	Checks   map[string]CheckResult `json:"checks"`
	Degraded []string               `json:"degraded,omitempty"`
}

// StartHealthServer exposes GET /health for the container HEALTHCHECK and for
// external uptime monitoring.
//
// Discord and the database are required: if either is down the bot cannot do its
// job, so /health returns 503 and Docker will mark the container unhealthy.
// Lavalink is reported but is NOT required — main() deliberately downgrades a
// Lavalink failure to a warning and keeps running with radio disabled, so
// failing the healthcheck on it would restart a bot that is otherwise fine.
func (b *STMPDBot) StartHealthServer() {
	addr := b.Cfg.Health.Address
	if addr == "" {
		addr = ":8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", b.handleHealth)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("Health server listening", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Health server stopped", slog.Any("err", err))
		}
	}()
}

func (b *STMPDBot) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := make(map[string]CheckResult, 3)
	var degraded []string

	// --- Discord gateway (required) ---
	checks["discord"] = CheckResult{OK: b.IsReady, Detail: gatewayDetail(b.IsReady)}

	// --- Database (required) ---
	dbCheck := CheckResult{}
	if b.DB == nil {
		dbCheck.Detail = "no connection pool"
	} else {
		start := time.Now()
		if err := b.DB.Ping(ctx); err != nil {
			dbCheck.Detail = err.Error()
		} else {
			dbCheck.OK = true
		}
		dbCheck.LatencyMS = time.Since(start).Milliseconds()
	}
	checks["database"] = dbCheck

	// --- Lavalink (optional: reported, never fatal) ---
	lavalinkOK := b.RadioManager != nil && b.RadioManager.IsLavalinkConnected()
	checks["lavalink"] = CheckResult{
		OK:     lavalinkOK,
		Detail: "optional; radio disabled when down",
	}
	if !lavalinkOK {
		degraded = append(degraded, "lavalink")
	}

	// --- Content sources (optional: reported, never fatal) ---
	// A dead feed does not stop the bot doing its job, so it must not fail the
	// container healthcheck and trigger a restart that cannot fix it. It does need
	// to be visible: the beatport outage of 2026-08-25 ran for four days precisely
	// because nothing aggregated "this source has returned nothing since Tuesday".
	for name, st := range utils.SourceHealthSnapshot() {
		checks["source:"+name] = CheckResult{
			OK:     !st.Degraded(),
			Detail: sourceDetail(st),
		}
	}
	degraded = append(degraded, utils.DegradedSources()...)

	healthy := checks["discord"].OK && checks["database"].OK

	resp := HealthResponse{
		Status:   "ok",
		Version:  b.Version,
		Commit:   b.Commit,
		UptimeS:  int64(time.Since(startedAt).Seconds()),
		Checks:   checks,
		Degraded: degraded,
	}
	if !healthy {
		resp.Status = "unhealthy"
	} else if len(degraded) > 0 {
		resp.Status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if healthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("Failed to write health response", slog.Any("err", err))
	}
}

// sourceDetail renders a source's state as a short human-readable string, since
// this endpoint is read by a person debugging as often as by a monitor.
func sourceDetail(st utils.SourceState) string {
	if !st.EverSucceeded {
		return "optional; no successful fetch yet (may be unconfigured)"
	}
	if st.ConsecutiveFailures == 0 {
		return fmt.Sprintf("optional; last success %s ago", time.Since(st.LastSuccess).Truncate(time.Second))
	}
	return fmt.Sprintf("optional; %d consecutive failures, last success %s ago: %s",
		st.ConsecutiveFailures, time.Since(st.LastSuccess).Truncate(time.Second), st.LastError)
}

func gatewayDetail(ready bool) string {
	if ready {
		return "gateway ready"
	}
	return "gateway not ready"
}
