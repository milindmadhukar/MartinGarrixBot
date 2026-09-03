package dashboard

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// The charts on this dashboard are CSS: a bar is a div with an inline width, a
// column is a div with an inline height, a heatmap cell is a span with an inline
// opacity. That works only if the CSP permits inline styles.
//
// It once did not. `style-src 'self'` blocks inline style ATTRIBUTES as well as
// <style> blocks, so the browser dropped every one of them and the page rendered
// full-width bars, invisible columns and a flat heatmap -- while the HTML was
// perfectly correct and every existing test passed. curl does not enforce CSP,
// so nothing caught it.
//
// These two tests are deliberately a pair. One asserts the templates still emit
// the styles; the other asserts the policy still allows them. Breaking either
// half reintroduces the bug, and either half alone would not notice.

func TestCSPAllowsInlineStyles(t *testing.T) {
	s := &Server{opts: testOptions(t)}

	rec := httptest.NewRecorder()
	s.securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("no Content-Security-Policy header")
	}

	styleSrc := directive(csp, "style-src")
	if styleSrc == "" {
		t.Fatalf("no style-src in %q", csp)
	}
	if !strings.Contains(styleSrc, "'unsafe-inline'") {
		t.Errorf("style-src is %q; without 'unsafe-inline' the browser drops every "+
			"chart's inline style and the panels render blank", styleSrc)
	}

	// The other half of the policy must stay strict -- that is where the real
	// XSS exposure is, and relaxing style-src is not a reason to relax this.
	if scriptSrc := directive(csp, "script-src"); strings.Contains(scriptSrc, "unsafe-inline") {
		t.Errorf("script-src must not allow inline script, got %q", scriptSrc)
	}
}

// Cover art is served from wherever the source published it -- the catalogue stores
// the URL, it never proxies or copies the image. So an artwork host missing from
// img-src does not degrade gracefully: every cover on the catalogue pages renders as a
// broken image, which reads as bad data rather than as a bad policy. curl does not
// enforce CSP, so nothing else here would catch it.
func TestCSPAllowsSongArtworkHosts(t *testing.T) {
	s := &Server{opts: testOptions(t)}

	rec := httptest.NewRecorder()
	s.securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	imgSrc := directive(rec.Header().Get("Content-Security-Policy"), "img-src")
	if imgSrc == "" {
		t.Fatal("no img-src directive")
	}

	// Every host the songs table actually holds artwork on.
	for _, host := range []string{
		"https://geo-media.beatport.com",        // beatport release covers
		"https://d384qsdodhwrqp.cloudfront.net", // STMPD's own CDN
		"https://cdn.sanity.io",                 // the STMPD Sanity dataset
		"https://*.mzstatic.com",                // Apple, served from is1..is5
		"https://cdn.discordapp.com",            // avatars and guild icons
	} {
		if !strings.Contains(imgSrc, host) {
			t.Errorf("img-src is %q; without %s those covers render broken", imgSrc, host)
		}
	}

	// Relaxing img-src is not a reason to relax the directive that matters.
	if scriptSrc := directive(rec.Header().Get("Content-Security-Policy"), "script-src"); strings.Contains(scriptSrc, "unsafe-inline") {
		t.Errorf("script-src must not allow inline script, got %q", scriptSrc)
	}
}

// TestChartBarsCarryDistinctWidths renders the busiest-channels panel with two
// very different values. If the bars come out the same width the chart is
// meaningless, which is exactly how the CSP bug looked in the browser.
func TestChartBarsCarryDistinctWidths(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	p := &pageData{GuildID: "1", Data: map[string]any{
		"WindowDays": 30,
		"Channels": []namedChannel{
			{ID: "1", Name: "#busy", Messages: 4000},
			{ID: "2", Name: "#quiet", Messages: 40},
		},
	}}

	var buf bytes.Buffer
	if err := r.pages["overview"].ExecuteTemplate(&buf, "panel-channels", p); err != nil {
		t.Fatalf("panel-channels: %v", err)
	}

	widths := widthPercents(buf.String())
	if len(widths) != 2 {
		t.Fatalf("expected 2 bar widths, got %d in:\n%s", len(widths), buf.String())
	}
	if widths[0] != 100 {
		t.Errorf("the largest channel should fill the bar, got %v%%", widths[0])
	}
	if widths[1] >= widths[0] {
		t.Errorf("a channel with 1%% of the traffic rendered at %v%% next to %v%% -- "+
			"bars are not proportional", widths[1], widths[0])
	}

	// html/template must not have neutered the value into ZgotmplZ.
	if strings.Contains(buf.String(), "ZgotmplZ") {
		t.Error("template escaping replaced a style value; the chart would render unstyled")
	}
}

// TestChartColumnsCarryHeights covers the growth panel, which sizes with height
// rather than width and failed the same way.
func TestChartColumnsCarryHeights(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	p := &pageData{GuildID: "1", Data: map[string]any{
		"WindowDays": 30,
		"Max":        int64(100),
		"Series": []db.DashJoinLeaveDailyRow{
			{Day: pgtype.Timestamp{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}, Joins: 100, Leaves: 10},
			{Day: pgtype.Timestamp{Time: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), Valid: true}, Joins: 5, Leaves: 90},
		},
	}}

	var buf bytes.Buffer
	if err := r.pages["overview"].ExecuteTemplate(&buf, "panel-growth", p); err != nil {
		t.Fatalf("panel-growth: %v", err)
	}

	if !strings.Contains(buf.String(), "style=\"height:") {
		t.Fatalf("growth columns carry no inline height:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "ZgotmplZ") {
		t.Error("template escaping replaced a style value")
	}
}

func directive(csp, name string) string {
	for _, part := range strings.Split(csp, ";") {
		part = strings.TrimSpace(part)
		if after, ok := strings.CutPrefix(part, name+" "); ok {
			return after
		}
	}
	return ""
}

var widthRe = regexp.MustCompile(`style="width:\s*([0-9.]+)%"`)

func widthPercents(html string) []float64 {
	var out []float64
	for _, m := range widthRe.FindAllStringSubmatch(html, -1) {
		v, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			continue
		}
		out = append(out, v)
	}
	return out
}

// TestAssetURLsAreContentVersioned guards the deploy failure this fixes: with a
// stable filename and a long max-age, Cloudflare served the previous build's
// stylesheet for hours after a redeploy. The new HTML plus the old CSS rendered
// a white page with white text on it.
func TestAssetURLsAreContentVersioned(t *testing.T) {
	css := assetURL("/static/app.css")
	if !strings.Contains(css, "?v=") {
		t.Fatalf("assetURL returned %q; without a content hash a cache can shadow a redeploy", css)
	}

	// The same content must produce the same URL, or every render would bust
	// the cache and the max-age would buy nothing.
	if again := assetURL("/static/app.css"); again != css {
		t.Errorf("assetURL is not stable: %q then %q", css, again)
	}

	// Different files must not collide on the same version.
	if js := assetURL("/static/htmx.min.js"); js == css {
		t.Error("two different assets produced the same URL")
	}

	// An unknown path passes through rather than breaking the page.
	if got := assetURL("/static/nope.png"); got != "/static/nope.png" {
		t.Errorf("unknown asset = %q, want it unchanged", got)
	}
}

// TestLayoutUsesVersionedAssets checks the template actually calls it -- the
// helper existing is no use if the layout still hardcodes the plain path.
func TestLayoutUsesVersionedAssets(t *testing.T) {
	r, err := newRenderer(testOptions(t), false)
	if err != nil {
		t.Fatalf("newRenderer: %v", err)
	}

	var buf bytes.Buffer
	if err := r.pages["landing"].ExecuteTemplate(&buf, "layout.html",
		&pageData{Title: "Sign in", Bare: true}); err != nil {
		t.Fatalf("layout: %v", err)
	}

	html := buf.String()
	if !strings.Contains(html, "/static/app.css?v=") {
		t.Error("the stylesheet link is not content-versioned")
	}
	if !strings.Contains(html, "/static/htmx.min.js?v=") {
		t.Error("the script src is not content-versioned")
	}
}
