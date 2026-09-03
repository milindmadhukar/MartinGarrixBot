package dashboard

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"image"
	_ "image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/disgoorg/snowflake/v2"
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

	options := make([]backgroundOption, 0, len(all))
	for _, b := range all {
		options = append(options, backgroundOption{
			ID:       b.ID,
			Filename: b.Filename,
			URL:      "/backgrounds/file/" + b.Filename,
			Selected: selectedSet[b.ID],
		})
	}

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

// handleBackgroundUpload shows the upload form and its client-side cropper.
func (s *Server) handleBackgroundUpload(w http.ResponseWriter, r *http.Request) {
	p := s.newPage(r, "Add a background")
	p.Nav = "backgrounds-upload"
	p.Data = map[string]any{
		"Problem": "",
		"Saved":   r.URL.Query().Get("saved") == "1",
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
	p := s.newPage(r, "Add a background")
	p.Nav = "backgrounds-upload"
	w.WriteHeader(http.StatusUnprocessableEntity)
	p.Data = map[string]any{"Problem": problem}
	s.render(w, r, "backgrounds-upload", "", p)
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
