package dashboard

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/disgoorg/disgo/oauth2"
	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/milindmadhukar/MartinGarrixBot/dashboard/session"
	db "github.com/milindmadhukar/MartinGarrixBot/db/sqlc"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Server holds everything the dashboard handlers need. It is constructed once
// in cmd/dashboard and is safe for concurrent use.
type Server struct {
	opts     *Options
	pool     *pgxpool.Pool
	queries  *db.Queries
	bots     *BotAPI
	sessions *session.Codec
	oauth    *oauth2.Client
	renderer *renderer
	http     *http.Server
}

func NewServer(opts *Options, pool *pgxpool.Pool, dev bool) (*Server, error) {
	clientID, err := snowflake.Parse(opts.ClientID)
	if err != nil {
		return nil, errors.New("dashboard.client_id is not a valid Discord application ID")
	}

	renderer, err := newRenderer(opts, dev)
	if err != nil {
		return nil, err
	}

	s := &Server{
		opts:     opts,
		pool:     pool,
		queries:  db.New(pool),
		bots:     NewBotAPI(opts.BotAPIURL, opts.BotAPISecret, opts.BotCacheTTL),
		sessions: session.NewCodec(opts.SessionSecret, opts.SessionTTL, opts.Secure),
		oauth:    oauth2.New(clientID, opts.ClientSecret),
		renderer: renderer,
	}

	s.http = &http.Server{
		Addr:    opts.Address,
		Handler: s.routes(),
		// ReadHeaderTimeout is the one that matters for a public listener: it
		// is what stops a slowloris client from holding a connection open
		// forever by dribbling out headers.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return s, nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /static/", s.staticHandler())
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("GET /auth/callback", s.handleCallback)
	mux.HandleFunc("POST /logout", s.handleLogout)

	// Authenticated.
	mux.Handle("GET /guilds", s.authed(s.handleGuildList))

	// Authenticated and guild-scoped. authed and guildScoped are per-route
	// wrappers rather than global middleware so the route table itself shows
	// which routes need a session -- there is no way to forget to exclude
	// /static or /login from an auth check that was never global.
	mux.Handle("GET /g/{guildID}", s.guildScoped(s.handleOverview))
	mux.Handle("GET /g/{guildID}/panels/{panel}", s.guildScoped(s.handlePanel))
	mux.Handle("GET /g/{guildID}/modlogs", s.guildScoped(s.handleModlogs))
	mux.Handle("GET /g/{guildID}/modlogs/table", s.guildScoped(s.handleModlogs))
	mux.Handle("GET /g/{guildID}/members", s.guildScoped(s.handleMemberLogs))
	mux.Handle("GET /g/{guildID}/members/table", s.guildScoped(s.handleMemberLogs))
	mux.Handle("GET /g/{guildID}/settings", s.guildScoped(s.handleSettings))

	return chain(mux,
		s.recoverer,
		s.requestLogger,
		s.securityHeaders,
	)
}

// ListenAndServe blocks until the context is cancelled, then drains in-flight
// requests.
//
// The bot's health server has no shutdown path at all; the dashboard needs one
// because it serves people mid-request, and killing a redeploy's last request
// is a visible failure rather than a dropped healthcheck.
func (s *Server) ListenAndServe(ctx context.Context) error {
	errs := make(chan error, 1)
	go func() {
		slog.Info("Dashboard listening", slog.String("addr", s.opts.Address))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()

	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
		slog.Info("Dashboard shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}

// handleHealthz backs the container HEALTHCHECK. Like the bot's /health it
// fails on a dead database, since a dashboard that cannot read is not serving.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if _, err := s.sessions.Read(r); err != nil {
		s.render(w, r, "landing", "", s.newPage(r, "Sign in"))
		return
	}
	http.Redirect(w, r, "/guilds", http.StatusSeeOther)
}

func (s *Server) staticHandler() http.Handler {
	fileServer := http.FileServer(http.FS(staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// embed.FS reports a zero ModTime, so http.FileServer sends no
		// Last-Modified or ETag and every asset is re-downloaded on each load.
		// The assets are versioned by the binary they are baked into, so a
		// long max-age is safe and is the whole fix.
		w.Header().Set("Cache-Control", "public, max-age=3600")
		fileServer.ServeHTTP(w, r)
	})
}
