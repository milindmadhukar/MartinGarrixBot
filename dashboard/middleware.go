package dashboard

import (
	"context"
	"crypto/hmac"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/milindmadhukar/MartinGarrixBot/dashboard/session"
)

type ctxKey int

const (
	ctxSession ctxKey = iota
	ctxGuildID
)

func chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	// Applied in reverse so the first argument ends up outermost, which is what
	// makes recoverer able to catch panics from everything below it.
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("Panic serving request",
					slog.Any("panic", rec),
					slog.String("path", r.URL.Path),
					slog.String("stack", string(debug.Stack())))
				// The status may already be written; WriteHeader then logs a
				// superfluous-write warning and is otherwise harmless, which
				// beats leaving the connection hanging.
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		// Static assets are the bulk of the requests and none of the signal.
		if len(r.URL.Path) >= 8 && r.URL.Path[:8] == "/static/" {
			return
		}

		attrs := []any{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("dur", time.Since(start)),
		}
		if sess, ok := sessionFrom(r.Context()); ok {
			attrs = append(attrs, slog.String("user_id", sess.UserID.String()))
		}
		slog.Info("dashboard request", attrs...)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	// script-src stays strict: htmx is the only script and it is served from
	// this origin, so no inline JavaScript is ever needed.
	//
	// style-src MUST keep 'unsafe-inline'. Under CSP2+ that keyword governs
	// inline style ATTRIBUTES as well as <style> blocks, and every chart on this
	// dashboard sizes itself with one -- bar widths, column heights, heatmap
	// opacity. Without it the browser silently drops all three: bars render
	// full-width, columns render zero-height, and every heatmap cell comes out
	// identical. That shipped once already, and curl cannot catch it because
	// curl does not enforce CSP. See TestCSPAllowsInlineStyles.
	//
	// cdn.discordapp.com is required for avatars and guild icons.
	const csp = "default-src 'self'; " +
		"img-src 'self' https://cdn.discordapp.com data:; " +
		"style-src 'self' 'unsafe-inline'; script-src 'self'; " +
		"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// authed requires a valid session and a guild list that is not stale.
func (s *Server) authed(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := s.sessions.Read(r)
		if err != nil {
			s.redirectToLogin(w, r)
			return
		}

		// Re-deriving the eligible set means re-running OAuth, so a stale list
		// sends the user back through /login rather than being refreshed in
		// place. With prompt=none that is an invisible double redirect.
		if sess.GuildsStale(s.opts.GuildCacheTTL) {
			s.redirectToLogin(w, r)
			return
		}

		next(w, r.WithContext(context.WithValue(r.Context(), ctxSession, sess)))
	})
}

// guildScoped adds the {guildID} access check on top of authed.
func (s *Server) guildScoped(next http.HandlerFunc) http.Handler {
	return s.authed(func(w http.ResponseWriter, r *http.Request) {
		guildID, err := snowflake.Parse(r.PathValue("guildID"))
		if err != nil {
			http.NotFound(w, r)
			return
		}

		sess, _ := sessionFrom(r.Context())
		if !sess.Administers(guildID) {
			// 404 rather than 403 on purpose: a 403 would confirm that the
			// guild exists and that the bot is in it. A 404 leaks nothing.
			s.renderError(w, r, http.StatusNotFound, "Not found",
				"No such server, or you do not administer it.")
			return
		}

		next(w, r.WithContext(context.WithValue(r.Context(), ctxGuildID, guildID)))
	})
}

// requireCSRF guards mutating requests. Unused in the read-only v1 and wired in
// with the settings form in phase 2.
func (s *Server) requireCSRF(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := sessionFrom(r.Context())
		if !ok {
			s.redirectToLogin(w, r)
			return
		}

		// Origin is checked independently of the token. SameSite=Lax already
		// blocks cross-site POSTs in current browsers; this covers the cases it
		// does not, notably a same-site subdomain attacker.
		if origin := r.Header.Get("Origin"); origin != "" && origin != s.opts.PublicBaseURL {
			s.renderError(w, r, http.StatusForbidden, "Blocked",
				"That request came from an unexpected origin.")
			return
		}

		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			token = r.FormValue("csrf_token")
		}
		// Constant time: a plain != leaks the length of the matching prefix to
		// anything that can time the response, which is enough to recover the
		// token a character at a time.
		if token == "" || !hmac.Equal([]byte(token), []byte(sess.CSRF)) {
			s.renderError(w, r, http.StatusForbidden, "Session expired",
				"Please reload the page and try again.")
			return
		}

		next(w, r)
	}
}

// redirectToLogin sends a browser to /login, but answers an htmx request with
// HX-Redirect instead: htmx would otherwise swap the login page's HTML into
// whatever fragment target the request came from.
func (s *Server) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func sessionFrom(ctx context.Context) (session.Session, bool) {
	sess, ok := ctx.Value(ctxSession).(session.Session)
	return sess, ok
}

func guildFrom(ctx context.Context) snowflake.ID {
	id, _ := ctx.Value(ctxGuildID).(snowflake.ID)
	return id
}
