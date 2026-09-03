package dashboard

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/milindmadhukar/STMPDBot/db/sqlc"
	"github.com/milindmadhukar/STMPDBot/utils"
	"github.com/milindmadhukar/STMPDBot/utils/catalogue"
)

// songFromPath loads the row a write route names, or writes the response itself.
func (s *Server) songFromPath(w http.ResponseWriter, r *http.Request) (db.Song, bool) {
	id, err := strconv.ParseInt(r.PathValue("songID"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.Song{}, false
	}
	song, err := s.queries.GetSongByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.renderError(w, r, http.StatusNotFound, "Not found", "No song with that id.")
			return db.Song{}, false
		}
		s.serverError(w, r, err)
		return db.Song{}, false
	}
	return song, true
}

// rekey rewrites the columns derived from a row's title and credits.
//
// It runs after every write that can change name, artists or mix name. Without it a
// hand-renamed song keeps the keys of its old name: it stops matching across sources,
// and -- because search_text is one of them -- it becomes unfindable under the name it
// now displays, which is the very defect the search work exists to fix.
func (s *Server) rekey(ctx context.Context, song db.Song) {
	if _, err := s.queries.SetSongKeys(ctx, db.SetSongKeysParams{
		ID:         song.ID,
		MatchKey:   utils.Text(utils.MatchKey(song.Name, "", song.MixName.String, song.Artists)),
		BaseKey:    utils.Text(utils.BaseKey(song.Name, song.Artists)),
		SearchText: utils.Text(utils.SearchText(song.Artists, song.Name, song.MixName.String, song.ReleaseName.String)),
	}); err != nil {
		slog.Error("Failed to rekey an edited song",
			slog.Int64("song_id", song.ID), slog.Any("err", err))
	}
	if _, err := s.queries.SetSongNormalizedName(ctx, db.SetSongNormalizedNameParams{
		ID:             song.ID,
		NormalizedName: utils.Text(utils.NormalizedTitle(song.Name)),
	}); err != nil {
		slog.Error("Failed to set a normalized name",
			slog.Int64("song_id", song.ID), slog.Any("err", err))
	}
}

// renderSong re-renders the song page, optionally carrying a problem or a note.
func (s *Server) renderSong(w http.ResponseWriter, r *http.Request, song db.Song, problem, note string) {
	ctx := r.Context()

	detail := songDetail{
		Song:     song,
		Links:    songLinks(song),
		Lockable: catalogue.LockableFields(),
	}
	if song.ParentSongID.Valid {
		if parent, err := s.queries.GetSongByID(ctx, song.ParentSongID.Int64); err == nil {
			detail.Parent = &parent
		}
		if siblings, err := s.queries.GetSongMixes(ctx, song.ParentSongID); err == nil {
			for _, m := range siblings {
				if m.ID != song.ID {
					detail.Siblings = append(detail.Siblings, m)
				}
			}
		}
	} else {
		detail.Renditions, _ = s.queries.GetSongMixes(ctx,
			pgtype.Int8{Int64: song.ID, Valid: true})
	}
	detail.Announcements, _ = s.queries.DashSongAnnouncements(ctx, song.ID)

	if findings, err := s.audit(ctx); err == nil {
		for _, id := range catalogue.GroupBySong(findings)[song.ID] {
			if c, ok := catalogue.CheckByID(id); ok {
				detail.Problems = append(detail.Problems, c)
			}
		}
	}

	p := s.newPage(r, song.Name)
	p.Nav = "songs"
	p.Data = map[string]any{"Detail": detail, "Problem": problem, "Note": note}
	s.render(w, r, "song", "song-detail", p)
}

func (s *Server) handleSongSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, _ := sessionFrom(ctx)

	song, ok := s.songFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "That form could not be read.")
		return
	}

	params, changed, problems := buildSongUpdate(song, r.PostForm)
	if len(problems) > 0 {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderSong(w, r, song, strings.Join(problems, " "), "")
		return
	}

	updated, err := s.queries.DashUpdateSong(ctx, params)
	if err != nil {
		// unique_release is (name, artists, mix_name, release_date) NULLS NOT
		// DISTINCT. Hitting it means another row already claims this identity, which
		// makes the two the same recording -- so the answer is a merge, not a rename.
		if db.ErrorCode(err) == db.UniqueViolation {
			w.WriteHeader(http.StatusUnprocessableEntity)
			s.renderSong(w, r, song,
				"Another row already holds that name, artists, mix and date. "+
					"That makes the two the same recording — merge them instead of renaming this one.", "")
			return
		}
		s.serverError(w, r, err)
		return
	}

	if len(changed) > 0 {
		s.rekey(ctx, updated)
		s.invalidateAudit()
	}

	slog.Info("Catalogue song saved",
		slog.Int64("song_id", song.ID),
		slog.String("user_id", sess.UserID.String()),
		slog.Any("changed", changed))

	note := "No changes."
	if len(changed) > 0 {
		note = "Saved " + strings.Join(changed, ", ") + ". Those fields are now locked against the sync."
	}
	s.renderSong(w, r, updated, "", note)
}

func (s *Server) handleSongUnlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, _ := sessionFrom(ctx)

	song, ok := s.songFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "That form could not be read.")
		return
	}

	field := r.PostForm.Get("field")
	if !catalogue.IsLockable(field) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderSong(w, r, song, "That is not a field that can be locked.", "")
		return
	}

	updated, err := s.queries.DashUnlockSongField(ctx, db.DashUnlockSongFieldParams{
		ID: song.ID, Field: field,
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	slog.Info("Catalogue field unlocked",
		slog.Int64("song_id", song.ID),
		slog.String("user_id", sess.UserID.String()),
		slog.String("field", field))

	s.renderSong(w, r, updated, "",
		field+" is back under automation and may be overwritten by the next sync.")
}

// checkParentShape enforces the invariants checkParents audits, so the page cannot
// create a row the problems view will immediately flag.
func (s *Server) checkParentShape(ctx context.Context, child db.Song, parentID int64) (string, error) {
	if parentID == child.ID {
		return "A song cannot be its own parent.", nil
	}
	parent, err := s.queries.GetSongByID(ctx, parentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "There is no song with that id to file this under.", nil
		}
		return "", err
	}
	if parent.IsCollection {
		return "That row is a release, not a song. A rendition hangs off the song, not off the EP it appeared on.", nil
	}
	if parent.ParentSongID.Valid {
		return "That row is itself a rendition. The tree is one level deep, so file this under its parent instead.", nil
	}
	if !utils.ArtistsSubsume(child.Artists, parent.Artists) {
		return "That song credits different artists. A rendition's credits have to contain its song's.", nil
	}
	return "", nil
}

func (s *Server) handleSongParent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, _ := sessionFrom(ctx)

	song, ok := s.songFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "That form could not be read.")
		return
	}

	raw := strings.TrimSpace(r.PostForm.Get("parent_song_id"))
	next := pgtype.Int8{}
	if raw != "" {
		parentID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			s.renderSong(w, r, song, "That is not a song id.", "")
			return
		}
		problem, err := s.checkParentShape(ctx, song, parentID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		if problem != "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			s.renderSong(w, r, song, problem, "")
			return
		}
		next = pgtype.Int8{Int64: parentID, Valid: true}
	}

	updated, err := s.queries.DashSetSongParent(ctx, db.DashSetSongParentParams{
		ID: song.ID, ParentSongID: next,
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.invalidateAudit()

	slog.Info("Catalogue song re-parented",
		slog.Int64("song_id", song.ID),
		slog.String("user_id", sess.UserID.String()),
		slog.Int64("parent_song_id", next.Int64))

	note := "Filed under #" + strconv.FormatInt(next.Int64, 10) + "."
	if !next.Valid {
		note = "This is now a song in its own right."
	}
	s.renderSong(w, r, updated, "", note+" The next link-remix-parents run will leave it alone.")
}

// handleSongPromote makes a rendition the canonical row for its song.
//
// This is the one-click repair for the defect that hid "Break Through The Silence": the
// row every listener sees had no streaming links, and the row that had them was filed
// underneath it. Where the automated comparator cannot tell which row a listener means,
// a person can.
//
// The three steps are ordered and must run together. Detaching the new canonical first
// keeps step two from sweeping it back up as one of its own siblings; re-pointing the
// siblings before demoting the old parent keeps the tree from ever being two levels
// deep, which is an invariant the audit would flag even transiently.
func (s *Server) handleSongPromote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, _ := sessionFrom(ctx)

	song, ok := s.songFromPath(w, r)
	if !ok {
		return
	}
	if !song.ParentSongID.Valid {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderSong(w, r, song, "This row is already the song, not a rendition of one.", "")
		return
	}
	oldParent := song.ParentSongID.Int64

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	q := s.queries.WithTx(tx)

	if _, err = q.DashSetSongParent(ctx, db.DashSetSongParentParams{
		ID: song.ID, ParentSongID: pgtype.Int8{},
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err = q.DashRepointChildren(ctx, db.DashRepointChildrenParams{
		OldParent: pgtype.Int8{Int64: oldParent, Valid: true},
		NewParent: pgtype.Int8{Int64: song.ID, Valid: true},
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err = q.DashSetSongParent(ctx, db.DashSetSongParentParams{
		ID: oldParent, ParentSongID: pgtype.Int8{Int64: song.ID, Valid: true},
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.invalidateAudit()

	slog.Info("Catalogue rendition promoted to canonical",
		slog.Int64("song_id", song.ID),
		slog.String("user_id", sess.UserID.String()),
		slog.Int64("demoted", oldParent))

	updated, err := s.queries.GetSongByID(ctx, song.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderSong(w, r, updated, "",
		"This is now the song people see. #"+strconv.FormatInt(oldParent, 10)+
			" and its other renditions are filed underneath it.")
}

// handleSongMerge folds another row into this one.
//
// The order is the same as dedupe-songs' merge, with the announcement repoint added in
// front of it. song_announcements.song_id is ON DELETE CASCADE, so deleting the merged
// row without moving its announcements first destroys the record of every message
// already posted for that song, and the refresh loop then has no way to know they exist.
func (s *Server) handleSongMerge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, _ := sessionFrom(ctx)

	winner, ok := s.songFromPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Bad request", "That form could not be read.")
		return
	}

	loserID, err := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("loser_id")), 10, 64)
	if err != nil || loserID == winner.ID {
		w.WriteHeader(http.StatusUnprocessableEntity)
		s.renderSong(w, r, winner, "Pick a different song to merge into this one.", "")
		return
	}
	loser, err := s.queries.GetSongByID(ctx, loserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			s.renderSong(w, r, winner, "There is no song with that id.", "")
			return
		}
		s.serverError(w, r, err)
		return
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	q := s.queries.WithTx(tx)

	if _, err = q.DashRepointAnnouncements(ctx, db.DashRepointAnnouncementsParams{
		OldSong: loser.ID, NewSong: winner.ID,
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if _, err = q.RepointChildren(ctx, db.RepointChildrenParams{
		OldParent: pgtype.Int8{Int64: loser.ID, Valid: true},
		NewParent: pgtype.Int8{Int64: winner.ID, Valid: true},
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	// beatport_id and stmpd_slug each carry a partial unique index, so the loser has to
	// stop claiming them before the winner can take them.
	if err = q.ReleaseSongIdentifiers(ctx, loser.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err = q.AdoptSongIdentifiers(ctx, db.AdoptSongIdentifiersParams{
		ID: winner.ID, BeatportID: loser.BeatportID, StmpdSlug: loser.StmpdSlug,
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err = q.MergeSongRows(ctx, db.MergeSongRowsParams{
		WinnerID: winner.ID, LoserID: loser.ID,
	}); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err = q.DeleteSong(ctx, loser.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.invalidateAudit()

	slog.Info("Catalogue songs merged",
		slog.Int64("song_id", winner.ID),
		slog.String("user_id", sess.UserID.String()),
		slog.Int64("merged_away", loser.ID),
		slog.String("merged_name", loser.Name))

	updated, err := s.queries.GetSongByID(ctx, winner.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.rekey(ctx, updated)
	s.renderSong(w, r, updated, "",
		"Merged #"+strconv.FormatInt(loser.ID, 10)+" ("+loser.Name+") into this row.")
}

// handleSongMergePick offers rows this song could be merged with or filed under.
func (s *Server) handleSongMergePick(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	song, ok := s.songFromPath(w, r)
	if !ok {
		return
	}

	q := r.URL.Query().Get("q")
	if strings.TrimSpace(q) == "" {
		q = song.Artists + " " + song.Name
	}
	candidates, err := s.queries.DashSongCandidates(ctx, db.DashSongCandidatesParams{
		ExcludeID: song.ID,
		Terms:     utils.SearchTerms(q),
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	p := s.newPage(r, "Merge "+song.Name)
	p.Nav = "songs"
	p.Data = map[string]any{"Song": song, "Candidates": candidates, "Query": q}
	s.render(w, r, "songmerge", "merge-candidates", p)
}
