package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"kino/internal/transcode"
)

type startPlaybackRequest struct {
	MediaID string `json:"mediaId"`
	// AudioIndex selects an audio track; omit or use -1 for the container default.
	AudioIndex *int `json:"audioIndex,omitempty"`
	// BurnSubtitleIndex burns an image-based subtitle track into the video.
	BurnSubtitleIndex *int `json:"burnSubtitleIndex,omitempty"`
	// ForceTranscode skips the direct-play check, which the player uses when the
	// browser rejected the file despite our codec check saying it should work.
	ForceTranscode bool `json:"forceTranscode,omitempty"`
}

// handleStartPlayback decides how to play an item and, when transcoding is needed,
// creates the session.
func (s *Server) handleStartPlayback(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var req startPlaybackRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	playable, err := s.db.ResolvePlayable(req.MediaID)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such media.")
		return
	}
	if !playable.Media.Available {
		writeErrorCode(w, http.StatusGone, "FILE_MISSING", "The media file is no longer on disk.")
		return
	}

	info := s.buildPlaybackInfo(user.ID, playable)

	// Direct play needs no session at all, and is the outcome we want whenever the
	// browser can handle the file: it costs the server nothing.
	audioIndex := -1
	if req.AudioIndex != nil {
		audioIndex = *req.AudioIndex
	}
	wantsNonDefaultAudio := req.AudioIndex != nil && *req.AudioIndex != playable.Media.DefaultAudioIndex()
	burn := -1
	if req.BurnSubtitleIndex != nil {
		burn = *req.BurnSubtitleIndex
	}

	if info.Method == "direct" && !req.ForceTranscode && !wantsNonDefaultAudio && burn < 0 {
		writeJSON(w, http.StatusOK, info)
		return
	}

	if s.transcoder == nil {
		writeErrorCode(w, http.StatusServiceUnavailable, "NO_FFMPEG",
			"This file needs to be converted for playback, but ffmpeg is not available. "+ffmpegInstallHint())
		return
	}

	session, err := s.transcoder.Start(transcode.StartOptions{
		UserID:            user.ID,
		MediaID:           playable.ID,
		Media:             playable.Media,
		AudioIndex:        audioIndex,
		BurnSubtitleIndex: burn,
	})
	if err != nil {
		if errors.Is(err, transcode.ErrNoTools) {
			writeErrorCode(w, http.StatusServiceUnavailable, "NO_FFMPEG",
				"ffmpeg is not available. "+ffmpegInstallHint())
			return
		}
		writeError(w, http.StatusInternalServerError, "Could not start playback: "+err.Error())
		return
	}

	sessionInfo := session.Info()
	info.Method = "hls"
	info.SessionID = session.ID
	info.URL = sessionInfo.MasterURL
	if info.Reason == "" && req.ForceTranscode {
		info.Reason = "Transcoding was requested by the player."
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"playback": info,
		"session":  sessionInfo,
	})
}

// handleStopPlayback ends a transcode session.
func (s *Server) handleStopPlayback(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	id := chi.URLParam(r, "sessionId")

	if s.transcoder == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no session"})
		return
	}

	// Only the owner may stop a session, so one user cannot interrupt another's film.
	if session, err := s.transcoder.Get(id); err == nil && session.UserID != user.ID && !user.IsAdmin {
		writeError(w, http.StatusForbidden, "That session belongs to another user.")
		return
	}

	s.transcoder.Stop(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

// handleMasterPlaylist serves the master playlist for a session.
func (s *Server) handleMasterPlaylist(w http.ResponseWriter, r *http.Request) {
	session, ok := s.lookupSession(w, r)
	if !ok {
		return
	}

	// The player fetches variant playlists and segments from <video>/hls.js, which
	// cannot attach an Authorization header, so the token has to ride along in the
	// URL. Propagate whatever the client used to reach us.
	suffix := tokenSuffix(r)

	playlist := transcode.MasterPlaylist(session.Ladder, func(rend transcode.Rendition) string {
		return rend.Name + "/index.m3u8" + suffix
	})

	writePlaylist(w, playlist)
}

// handleVariantPlaylist serves the segment list for one rendition.
func (s *Server) handleVariantPlaylist(w http.ResponseWriter, r *http.Request) {
	session, ok := s.lookupSession(w, r)
	if !ok {
		return
	}

	variant := chi.URLParam(r, "variant")
	if _, found := transcode.FindRendition(session.Ladder, variant); !found {
		writeError(w, http.StatusNotFound, "Unknown quality level.")
		return
	}

	suffix := tokenSuffix(r)
	playlist := transcode.VariantPlaylist(session.DurationSec, session.SegmentSec, func(n int) string {
		return "seg" + strconv.Itoa(n) + ".ts" + suffix
	})

	writePlaylist(w, playlist)
}

// handleSegment serves one transcoded segment, encoding it on demand.
func (s *Server) handleSegment(w http.ResponseWriter, r *http.Request) {
	session, ok := s.lookupSession(w, r)
	if !ok {
		return
	}

	variant := chi.URLParam(r, "variant")
	name := chi.URLParam(r, "segment")

	n, err := parseSegmentName(name)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Malformed segment name.")
		return
	}

	path, err := session.SegmentPath(r.Context(), variant, n)
	if err != nil {
		switch {
		case errors.Is(err, r.Context().Err()) && r.Context().Err() != nil:
			// The client navigated away mid-encode; nothing to report.
			return
		case errors.Is(err, transcode.ErrSegmentTimeout):
			log.Printf("api: segment %s/%d timed out for session %s", variant, n, session.ID)
			writeError(w, http.StatusServiceUnavailable,
				"The server could not encode this part of the video in time.")
		case errors.Is(err, transcode.ErrSessionClosed):
			writeErrorCode(w, http.StatusGone, "SESSION_ENDED", "This playback session has ended.")
		default:
			log.Printf("api: segment %s/%d failed for session %s: %v", variant, n, session.ID, err)
			writeError(w, http.StatusInternalServerError, "Could not produce this part of the video.")
		}
		return
	}

	w.Header().Set("Content-Type", "video/mp2t")
	// A segment's content never changes once written, and the session directory is
	// deleted when playback ends, so caching it hard is safe and saves re-encoding
	// on a rewind.
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

// lookupSession resolves the session in the URL and checks the caller owns it.
func (s *Server) lookupSession(w http.ResponseWriter, r *http.Request) (*transcode.Session, bool) {
	if s.transcoder == nil {
		writeError(w, http.StatusServiceUnavailable, "Transcoding is not available.")
		return nil, false
	}

	session, err := s.transcoder.Get(chi.URLParam(r, "sessionId"))
	if err != nil {
		// An expired session is the normal outcome of leaving a tab open overnight,
		// so give the player a code it can act on by restarting playback.
		writeErrorCode(w, http.StatusGone, "SESSION_ENDED",
			"This playback session has expired. Press play again to restart it.")
		return nil, false
	}

	user, ok := userFrom(r)
	if !ok {
		writeErrorCode(w, http.StatusUnauthorized, "NO_TOKEN", "Authentication required.")
		return nil, false
	}
	if session.UserID != user.ID && !user.IsAdmin {
		writeError(w, http.StatusForbidden, "That session belongs to another user.")
		return nil, false
	}

	return session, true
}

func writePlaylist(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	// Playlists are generated per request and must never be cached: a stale one would
	// point at a session that no longer exists.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(body))
}

// tokenSuffix rebuilds the api_key query parameter for URLs embedded in a playlist.
func tokenSuffix(r *http.Request) string {
	if token := r.URL.Query().Get("api_key"); token != "" {
		return "?api_key=" + urlQueryEscape(token)
	}
	return ""
}

// parseSegmentName extracts N from "segN.ts".
func parseSegmentName(name string) (int, error) {
	trimmed := strings.TrimSuffix(name, ".ts")
	trimmed = strings.TrimPrefix(trimmed, "seg")
	n, err := strconv.Atoi(trimmed)
	if err != nil || n < 0 {
		return 0, errors.New("malformed segment name")
	}
	return n, nil
}

// handleActiveSessions lists running transcodes, so an admin can see what is loading
// the machine.
func (s *Server) handleActiveSessions(w http.ResponseWriter, r *http.Request) {
	if s.transcoder == nil {
		writeJSON(w, http.StatusOK, []transcode.Info{})
		return
	}
	sessions := s.transcoder.Active()
	if sessions == nil {
		sessions = []transcode.Info{}
	}
	writeJSON(w, http.StatusOK, sessions)
}
