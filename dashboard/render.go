package dashboard

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5/pgtype"
)

// renderer owns the template set. Pages are parsed once at startup; the dev
// flag re-parses from disk on every request so `air` reloads pick up template
// edits without a rebuild.
type renderer struct {
	opts  *Options
	dev   bool
	pages map[string]*template.Template
}

// pageFiles lists every page template. Each is parsed together with the layout
// and the shared partials, so a page can override any block the layout defines.
var pageFiles = []string{
	"landing",
	"noaccess",
	"guilds",
	"overview",
	"modlogs",
	"members",
	"settings",
	"error",
}

func newRenderer(opts *Options, dev bool) (*renderer, error) {
	r := &renderer{opts: opts, dev: dev}
	pages, err := r.parse()
	if err != nil {
		return nil, err
	}
	r.pages = pages
	return r, nil
}

func (r *renderer) parse() (map[string]*template.Template, error) {
	pages := make(map[string]*template.Template, len(pageFiles))
	for _, name := range pageFiles {
		t := template.New("layout.html").Funcs(r.funcs())

		var err error
		if r.dev {
			t, err = t.ParseFiles(
				filepath.Join("dashboard/templates/layout.html"),
				filepath.Join("dashboard/templates/partials.html"),
				filepath.Join("dashboard/templates/pages", name+".html"),
			)
		} else {
			t, err = t.ParseFS(templatesFS,
				"templates/layout.html",
				"templates/partials.html",
				"templates/pages/"+name+".html",
			)
		}
		if err != nil {
			return nil, fmt.Errorf("parsing page %q: %w", name, err)
		}
		pages[name] = t
	}
	return pages, nil
}

// render writes either the whole page or just one named block, depending on
// whether htmx asked for a fragment.
//
// Both paths execute the same template text, so a fragment can never drift from
// how it renders inside the full page.
func (s *Server) render(w http.ResponseWriter, r *http.Request, page, fragment string, data any) {
	pages := s.renderer.pages
	if s.renderer.dev {
		reparsed, err := s.renderer.parse()
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		pages = reparsed
	}

	t, ok := pages[page]
	if !ok {
		s.serverError(w, r, fmt.Errorf("unknown page template %q", page))
		return
	}

	entry := "layout.html"
	// HX-Boosted requests set HX-Request too but expect a whole document, so
	// they must not be served a fragment.
	if fragment != "" && r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true" {
		entry = fragment
	}

	// Buffered on purpose. Executing straight into the ResponseWriter means a
	// template error halfway through has already sent a 200 and half a page,
	// and the error handler can no longer change the status.
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, entry, data); err != nil {
		s.serverError(w, r, fmt.Errorf("executing %q/%q: %w", page, entry, err))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// pageData is the common envelope every template receives.
type pageData struct {
	Title     string
	Nav       string
	LoggedIn  bool
	UserID    string
	Username  string
	AvatarURL string
	CSRFToken string

	GuildID   string
	GuildName string
	GuildIcon string

	// Bare drops the header and footer. The sign-in and refused-access screens
	// own the whole viewport and have no navigation to carry.
	Bare bool

	// Degraded is set when the bot's internal API could not be reached, so the
	// layout can explain why names are missing instead of the page just looking
	// broken.
	Degraded bool

	Data any
}

func (s *Server) newPage(r *http.Request, title string) *pageData {
	p := &pageData{Title: title}
	if sess, ok := sessionFrom(r.Context()); ok {
		p.LoggedIn = true
		p.UserID = sess.UserID.String()
		p.Username = sess.Username
		p.AvatarURL = avatarURL(sess.UserID.String(), sess.AvatarHash)
		p.CSRFToken = sess.CSRF
	}
	return p
}

// withGuild fills in the guild header. A failure to reach the bot is not an
// error: the page falls back to the raw ID and flags itself degraded.
func (s *Server) withGuild(r *http.Request, p *pageData, guildID snowflake.ID) {
	p.GuildID = guildID.String()
	p.GuildName = guildID.String()

	guild, err := s.bots.Guild(r.Context(), guildID)
	if err != nil {
		p.Degraded = true
		return
	}
	p.GuildName = guild.Name
	p.GuildIcon = guildIconURL(guild.ID, guild.Icon)
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("Dashboard error",
		slog.Any("err", err),
		slog.String("path", r.URL.Path))
	s.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
		"The dashboard hit an unexpected error. It has been logged.")
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, title, detail string) {
	p := s.newPage(r, title)
	p.Data = map[string]string{"Title": title, "Detail": detail}

	w.WriteHeader(status)
	// Rendered without the fragment path: an error must replace the whole page
	// rather than being swapped into a table body.
	s.render(w, r, "error", "", p)
}

func (r *renderer) funcs() template.FuncMap {
	return template.FuncMap{
		// fmtTime is the only correct way to display a naive timestamp column
		// in this codebase. pgx hands those back with a UTC location, so
		// formatting one directly prints UTC even though every other timestamp
		// the operator sees is in the configured zone.
		"fmtTime": func(ts pgtype.Timestamp) string {
			if !ts.Valid {
				return "—"
			}
			return ts.Time.UTC().In(r.opts.Location).Format("2006-01-02 15:04")
		},
		"fmtDate": func(ts pgtype.Timestamp) string {
			if !ts.Valid {
				return "—"
			}
			return ts.Time.UTC().In(r.opts.Location).Format("Jan 2")
		},
		"relTime": func(ts pgtype.Timestamp) string {
			if !ts.Valid {
				return "—"
			}
			return humanDuration(time.Since(ts.Time.UTC()))
		},
		// snowflakeTime recovers a Discord ID's creation time, which is how the
		// member pages show account age without storing it.
		"snowflakeTime": func(id string) string {
			parsed, err := snowflake.Parse(id)
			if err != nil {
				return "—"
			}
			return parsed.Time().In(r.opts.Location).Format("2006-01-02")
		},
		"avatarURL": avatarURL,
		// comma takes any so templates can pass the int, int32 and int64 that
		// the generated row structs mix freely.
		"comma": commaAny,
		"pct": func(part, total int64) float64 {
			if total == 0 {
				return 0
			}
			return float64(part) / float64(total) * 100
		},
		"int64": func(v int32) int64 { return int64(v) },
		"add":   func(a, b int) int { return a + b },
		"sub":   func(a, b int) int { return a - b },
		"mod":   func(a, b int) int { return a % b },
		// list builds an ordered slice, which map-based dict cannot do:
		// ranging a map in a template sorts by key.
		"list": func(values ...any) []any { return values },
		"seq": func(n int) []int {
			out := make([]int, n)
			for i := range out {
				out[i] = i
			}
			return out
		},
		// dict lets a template pass more than one value into a partial.
		"dict": func(values ...any) (map[string]any, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict needs an even number of arguments")
			}
			out := make(map[string]any, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings")
				}
				out[key] = values[i+1]
			}
			return out, nil
		},
		"title": func(s string) string {
			if s == "" {
				return s
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
	}
}

// commaAny adapts the numeric types the generated sqlc rows use. Templates
// cannot convert, so the function has to.
func commaAny(v any) string {
	switch n := v.(type) {
	case int:
		return comma(int64(n))
	case int32:
		return comma(int64(n))
	case int64:
		return comma(n)
	case pgtype.Int4:
		if !n.Valid {
			return "0"
		}
		return comma(int64(n.Int32))
	case pgtype.Int8:
		if !n.Valid {
			return "0"
		}
		return comma(n.Int64)
	case float64:
		return comma(int64(n))
	default:
		return fmt.Sprintf("%v", v)
	}
}

// comma formats an integer with thousands separators. Counts on this dashboard
// routinely run to seven digits and are unreadable without them.
func comma(n int64) string {
	if n < 0 {
		return "-" + comma(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
	}
	for i := pre; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/24/30))
	}
}
