package mgbot

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
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
func (b *MartinGarrixBot) StartHealthServer() {
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

func (b *MartinGarrixBot) handleHealth(w http.ResponseWriter, r *http.Request) {
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

func gatewayDetail(ready bool) string {
	if ready {
		return "gateway ready"
	}
	return "gateway not ready"
}
