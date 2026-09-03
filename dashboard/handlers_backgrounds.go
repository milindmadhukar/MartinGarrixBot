package dashboard

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/disgoorg/snowflake/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
)

// maxUploadBytes bounds the multipart body a super admin can post. The crop
// step client-side always produces a modest PNG (the card is 1000x300), so
// this is generous headroom rather than a tight fit.
const maxUploadBytes = 20 << 20 // 20MB

// validBackgroundModes are the only values backgrounds.sql's CHECK constraint
// on guilds.background_mode accepts.
var validBackgroundModes = map[string]bool{"random": true, "cycle": true}

type backgroundOption struct {
	ID       int64
	Filename string
	URL      string
	Selected bool
}

func backgroundOptions(all []db.Background, selected map[int64]bool) []backgroundOption {
	options := make([]backgroundOption, 0, len(all))
	for _, b := range all {
		options = append(options, backgroundOption{
			ID:       b.ID,
			Filename: b.Filename,
			URL:      "/backgrounds/file/" + b.Filename,
			Selected: selected[b.ID],
		})
	}
	return options
}

// handleBackgrounds shows the catalogue as a checkbox grid plus a
// random/cycle mode picker, scoped to one guild.
func (s *Server) handleBackgrounds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := guildFrom(ctx)

	s.renderBackgrounds(w, r, guildID, "", nil)
}

func (s *Server) handleBackgroundsSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	guildID := guildFrom(ctx)
	sess, _ := sessionFrom(ctx)

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "That form could not be read.")
		return
	}

	all, err := s.queries.ListBackgrounds(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	valid := make(map[int64]bool, len(all))
	for _, b := range all {
		valid[b.ID] = true
	}

	var selected []int64
	for _, raw := range r.Form["background_ids"] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || !valid[id] {
			continue
		}
		selected = append(selected, id)
	}

	mode := r.FormValue("mode")
	if !validBackgroundModes[mode] {
		s.renderBackgrounds(w, r, guildID, "Mode must be Random or Cycle.", nil)
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback(ctx)

	qtx := s.queries.WithTx(tx)
	if err := qtx.ClearGuildBackgrounds(ctx, int64(guildID)); err != nil {
		s.serverError(w, r, err)
		return
	}
	for _, id := range selected {
		if err := qtx.AddGuildBackground(ctx, db.AddGuildBackgroundParams{
			GuildID: int64(guildID), BackgroundID: id,
		}); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if err := qtx.SetGuildBackgroundMode(ctx, db.SetGuildBackgroundModeParams{
		GuildID: int64(guildID), BackgroundMode: mode,
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}

	slog.Info("Dashboard backgrounds saved",
		slog.String("guild_id", guildID.String()),
		slog.String("user_id", sess.UserID.String()),
		slog.Int("selected", len(selected)),
		slog.String("mode", mode))

	s.renderBackgrounds(w, r, guildID, "", []string{"backgrounds"})
}

func (s *Server) renderBackgrounds(w http.ResponseWriter, r *http.Request, guildID snowflake.ID, problem string, saved []string) {
	ctx := r.Context()

	all, err := s.queries.ListBackgrounds(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	selectedRows, err := s.queries.ListGuildBackgrounds(ctx, int64(guildID))
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	selectedSet := make(map[int64]bool, len(selectedRows))
	for _, b := range selectedRows {
		selectedSet[b.ID] = true
	}

	mode := "random"
	if settings, err := s.queries.GetGuildBackgroundSettings(ctx, int64(guildID)); err == nil {
		mode = settings.BackgroundMode
	}

	options := backgroundOptions(all, selectedSet)

	p := s.newPage(r, "Backgrounds")
	p.Nav = "backgrounds"
	s.withGuild(r, p, guildID)
	p.Data = map[string]any{
		"Options": options,
		"Mode":    mode,
		"Problem": problem,
		"Saved":   saved,
		"Any":     len(all) > 0,
	}
	s.render(w, r, "backgrounds", "backgrounds-form", p)
}

// handleBackgroundUpload shows the upload form, its client-side cropper, and
// the existing catalogue with delete controls.
func (s *Server) handleBackgroundUpload(w http.ResponseWriter, r *http.Request) {
	s.renderUploadPage(w, r, "", r.URL.Query().Get("saved") == "1")
}

func (s *Server) renderUploadPage(w http.ResponseWriter, r *http.Request, problem string, saved bool) {
	ctx := r.Context()

	all, err := s.queries.ListBackgrounds(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	p := s.newPage(r, "Add a background")
	p.Nav = "backgrounds-upload"
	p.Data = map[string]any{
		"Problem":     problem,
		"Saved":       saved,
		"Backgrounds": backgroundOptions(all, nil),
	}
	s.render(w, r, "backgrounds-upload", "", p)
}

// handleBackgroundUploadSave stores the already-cropped image the browser
// posts (see static/cropper.js): decode, re-encode as PNG server-side so
// nothing but a real image ever reaches disk, then catalogue it.
func (s *Server) handleBackgroundUploadSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, _ := sessionFrom(ctx)

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		s.renderUploadError(w, r, "That file is too large or the upload was interrupted. Try again.")
		return
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		s.renderUploadError(w, r, "Choose an image to upload.")
		return
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		s.renderUploadError(w, r, "That file is not a readable image.")
		return
	}

	filename, err := randomFilename()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	dest := filepath.Join(s.opts.BackgroundsDir, filename)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := os.MkdirAll(s.opts.BackgroundsDir, 0o755); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := os.WriteFile(dest, buf.Bytes(), 0o644); err != nil {
		s.serverError(w, r, err)
		return
	}

	uploadedBy := pgtype.Int8{Int64: int64(sess.UserID), Valid: true}
	if _, err := s.queries.CreateBackground(ctx, db.CreateBackgroundParams{
		Filename: filename, UploadedBy: uploadedBy,
	}); err != nil {
		_ = os.Remove(dest)
		s.serverError(w, r, err)
		return
	}

	slog.Info("Dashboard background uploaded",
		slog.String("user_id", sess.UserID.String()),
		slog.String("filename", filename))

	http.Redirect(w, r, "/backgrounds/upload?saved=1", http.StatusSeeOther)
}

func (s *Server) renderUploadError(w http.ResponseWriter, r *http.Request, problem string) {
	w.WriteHeader(http.StatusUnprocessableEntity)
	s.renderUploadPage(w, r, problem, false)
}

// handleBackgroundDelete removes a background from the catalogue: the row
// (cascading out of every guild's selection and clearing it from anyone
// mid-cycle on it), then its file. Global and permanent -- there is no
// per-guild "hide" short of this.
func (s *Server) handleBackgroundDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, _ := sessionFrom(ctx)

	id, err := strconv.ParseInt(r.PathValue("backgroundID"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	bg, err := s.queries.GetBackground(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.serverError(w, r, err)
		return
	}

	if err := s.queries.DeleteBackground(ctx, id); err != nil {
		s.serverError(w, r, err)
		return
	}

	// Best-effort: the catalogue row is already gone either way, and a file
	// that fails to remove (already missing, permissions) is not worth
	// failing the whole request over.
	if err := os.Remove(filepath.Join(s.opts.BackgroundsDir, bg.Filename)); err != nil && !os.IsNotExist(err) {
		slog.Warn("Could not remove background file from disk",
			slog.String("filename", bg.Filename), slog.Any("err", err))
	}

	slog.Info("Dashboard background deleted",
		slog.String("user_id", sess.UserID.String()),
		slog.Int64("background_id", id),
		slog.String("filename", bg.Filename))

	all, err := s.queries.ListBackgrounds(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := s.newPage(r, "Add a background")
	p.Data = map[string]any{"Backgrounds": backgroundOptions(all, nil)}
	s.render(w, r, "backgrounds-upload", "backgrounds-list", p)
}

// handleBackgroundFile serves one catalogue image. filename is looked up
// against the catalogue rather than trusted as a path -- it never reaches the
// filesystem unless it is a real row's filename, which is what keeps this
// from becoming a path-traversal read of s.opts.BackgroundsDir's parent.
func (s *Server) handleBackgroundFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requested := r.PathValue("filename")

	all, err := s.queries.ListBackgrounds(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	found := false
	for _, b := range all {
		if b.Filename == requested {
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, filepath.Join(s.opts.BackgroundsDir, requested))
}

func randomFilename() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf) + ".png", nil
}
