package api

import (
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"

	"kino/internal/models"
)

type progressReport struct {
	PositionSec float64 `json:"positionSec"`
	DurationSec float64 `json:"durationSec,omitempty"`
	// Finished is sent when playback reaches the end, so the item is marked watched
	// even if the last progress tick landed just short of the threshold.
	Finished bool `json:"finished,omitempty"`
}

// handleReportProgress records how far through an item the viewer is.
//
// The player calls this every few seconds and on pause/unload, so it must be cheap:
// it touches one record in an in-memory collection and lets the debounced flusher
// deal with disk.
func (s *Server) handleReportProgress(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	mediaID := chi.URLParam(r, "mediaId")

	playable, err := s.db.ResolvePlayable(mediaID)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such media.")
		return
	}

	var report progressReport
	if !decodeJSON(w, r, &report) {
		return
	}

	duration := report.DurationSec
	if duration <= 0 {
		duration = playable.Media.DurationSec
	}
	position := report.PositionSec
	if position < 0 {
		position = 0
	}
	if duration > 0 && position > duration {
		position = duration
	}

	id := models.PlayStateID(user.ID, mediaID)
	state, err := s.db.PlayStates.Upsert(id, func(p *models.PlayState) {
		wasWatched := p.Watched

		p.ID = id
		p.UserID = user.ID
		p.MediaID = mediaID
		p.Kind = playable.Kind
		p.PositionSec = position
		p.DurationSec = duration
		p.UpdatedAt = time.Now()

		// Past the threshold the item counts as watched, and the resume point is
		// cleared so that pressing play starts it from the beginning next time.
		reachedEnd := report.Finished ||
			(duration > 0 && position/duration >= models.WatchedThreshold)

		if reachedEnd {
			p.Watched = true
			p.PositionSec = 0
			if !wasWatched {
				p.PlayCount++
			}
		} else {
			p.Watched = false
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save your progress.")
		return
	}

	writeJSON(w, http.StatusOK, state)
}

// handleGetProgress returns the viewer's state for one item.
func (s *Server) handleGetProgress(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	mediaID := chi.URLParam(r, "mediaId")

	state, err := s.db.PlayStates.Get(models.PlayStateID(user.ID, mediaID))
	if err != nil {
		// No history is a normal state, not an error.
		writeJSON(w, http.StatusOK, models.PlayState{
			ID: models.PlayStateID(user.ID, mediaID), UserID: user.ID, MediaID: mediaID,
		})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

type watchedRequest struct {
	Watched bool `json:"watched"`
}

// handleSetWatched marks an item watched or unwatched explicitly.
func (s *Server) handleSetWatched(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	mediaID := chi.URLParam(r, "mediaId")

	playable, err := s.db.ResolvePlayable(mediaID)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such media.")
		return
	}

	var req watchedRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	id := models.PlayStateID(user.ID, mediaID)
	state, err := s.db.PlayStates.Upsert(id, func(p *models.PlayState) {
		p.ID = id
		p.UserID = user.ID
		p.MediaID = mediaID
		p.Kind = playable.Kind
		p.Watched = req.Watched
		p.PositionSec = 0
		p.DurationSec = playable.Media.DurationSec
		p.UpdatedAt = time.Now()
		if req.Watched {
			p.PlayCount++
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update the watched state.")
		return
	}

	writeJSON(w, http.StatusOK, state)
}

// handleMarkSeasonWatched marks every episode of a season at once, which is tedious
// to do one by one from the UI.
func (s *Server) handleMarkSeasonWatched(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	show, err := s.db.Shows.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such show.")
		return
	}
	seasonNum := queryInt(r, "season", -1)

	var req watchedRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	changed := 0
	for _, season := range show.Seasons {
		if seasonNum >= 0 && season.Number != seasonNum {
			continue
		}
		for _, ep := range season.Episodes {
			if !ep.Media.Available {
				continue
			}
			id := models.PlayStateID(user.ID, ep.ID)
			epID := ep.ID
			duration := ep.Media.DurationSec
			_, _ = s.db.PlayStates.Upsert(id, func(p *models.PlayState) {
				p.ID = id
				p.UserID = user.ID
				p.MediaID = epID
				p.Kind = models.PlayableEpisode
				p.Watched = req.Watched
				p.PositionSec = 0
				p.DurationSec = duration
				p.UpdatedAt = time.Now()
			})
			changed++
		}
	}

	writeJSON(w, http.StatusOK, map[string]int{"updated": changed})
}

// handleClearProgress removes a resume point, taking the item out of Continue Watching.
func (s *Server) handleClearProgress(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	mediaID := chi.URLParam(r, "mediaId")

	s.db.PlayStates.Delete(models.PlayStateID(user.ID, mediaID))
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// ---- subtitles ----

// handleListSubtitles returns every subtitle option for an item.
func (s *Server) handleListSubtitles(w http.ResponseWriter, r *http.Request) {
	playable, err := s.db.ResolvePlayable(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such media.")
		return
	}
	if s.subtitles == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	tracks := s.subtitles.Tracks(playable.Media)
	if tracks == nil {
		tracks = nil // encoded as [] below
	}
	if len(tracks) == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, tracks)
}

// handleGetSubtitle serves one track as WebVTT, converting it if necessary.
func (s *Server) handleGetSubtitle(w http.ResponseWriter, r *http.Request) {
	playable, err := s.db.ResolvePlayable(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such media.")
		return
	}
	if s.subtitles == nil {
		writeError(w, http.StatusServiceUnavailable, "Subtitles are not available.")
		return
	}

	trackID := chi.URLParam(r, "trackId")
	track, ok := s.subtitles.FindTrack(playable.Media, trackID)
	if !ok {
		writeError(w, http.StatusNotFound, "No such subtitle track.")
		return
	}
	if track.BurnInOnly {
		writeErrorCode(w, http.StatusConflict, "BURN_IN_REQUIRED",
			"This subtitle track is image-based and has to be rendered into the video. "+
				"Restart playback with this track selected for burn-in.")
		return
	}

	path, err := s.subtitles.WebVTT(r.Context(), playable.Media, track)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not prepare the subtitles: "+err.Error())
		return
	}

	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read the subtitles.")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read the subtitles.")
		return
	}

	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, "subtitles.vtt", stat.ModTime(), f)
}
