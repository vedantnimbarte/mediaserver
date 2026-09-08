package api

import (
	"net/http"
	"time"

	"kino/internal/models"
)

// handleGetPrefs returns the caller's preferences, or the defaults if they have never
// changed anything.
func (s *Server) handleGetPrefs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.db.PrefsFor(mustUser(r).ID))
}

// prefsUpdate uses pointers so an omitted field keeps its current value; a partial
// update from one settings panel must not reset the others.
type prefsUpdate struct {
	MaxQuality         *string `json:"maxQuality,omitempty"`
	AutoplayNext       *bool   `json:"autoplayNext,omitempty"`
	AutoplayCountdown  *int    `json:"autoplayCountdown,omitempty"`
	PreferredAudioLang *string `json:"preferredAudioLang,omitempty"`
	PreferredSubLang   *string `json:"preferredSubLang,omitempty"`
	SubtitlesDefaultOn *bool   `json:"subtitlesDefaultOn,omitempty"`
	SeekStepSec        *int    `json:"seekStepSec,omitempty"`
	SkipIntroEnabled   *bool   `json:"skipIntroEnabled,omitempty"`

	AccentColor   *string `json:"accentColor,omitempty"`
	PosterSize    *string `json:"posterSize,omitempty"`
	ShowTitles    *bool   `json:"showTitles,omitempty"`
	ReducedMotion *bool   `json:"reducedMotion,omitempty"`
	HeroOnHome    *bool   `json:"heroOnHome,omitempty"`
}

func (s *Server) handleUpdatePrefs(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var req prefsUpdate
	if !decodeJSON(w, r, &req) {
		return
	}

	current := s.db.PrefsFor(user.ID)

	updated, err := s.db.Prefs.Upsert(user.ID, func(p *models.UserPrefs) {
		*p = current

		if req.MaxQuality != nil {
			p.MaxQuality = *req.MaxQuality
		}
		if req.AutoplayNext != nil {
			p.AutoplayNext = *req.AutoplayNext
		}
		if req.AutoplayCountdown != nil {
			p.AutoplayCountdown = *req.AutoplayCountdown
		}
		if req.PreferredAudioLang != nil {
			p.PreferredAudioLang = *req.PreferredAudioLang
		}
		if req.PreferredSubLang != nil {
			p.PreferredSubLang = *req.PreferredSubLang
		}
		if req.SubtitlesDefaultOn != nil {
			p.SubtitlesDefaultOn = *req.SubtitlesDefaultOn
		}
		if req.SeekStepSec != nil {
			p.SeekStepSec = *req.SeekStepSec
		}
		if req.SkipIntroEnabled != nil {
			p.SkipIntroEnabled = *req.SkipIntroEnabled
		}

		if req.AccentColor != nil {
			p.AccentColor = *req.AccentColor
		}
		if req.PosterSize != nil {
			p.PosterSize = *req.PosterSize
		}
		if req.ShowTitles != nil {
			p.ShowTitles = *req.ShowTitles
		}
		if req.ReducedMotion != nil {
			p.ReducedMotion = *req.ReducedMotion
		}
		if req.HeroOnHome != nil {
			p.HeroOnHome = *req.HeroOnHome
		}

		// Repair anything out of range before it is stored, so a bad value from an old
		// client cannot wedge the UI.
		p.Normalize()
		p.UpdatedAt = time.Now()
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save your preferences.")
		return
	}

	writeJSON(w, http.StatusOK, updated)
}

// handleResetPrefs restores the defaults.
func (s *Server) handleResetPrefs(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	s.db.Prefs.Delete(user.ID)
	writeJSON(w, http.StatusOK, models.DefaultPrefs(user.ID))
}
