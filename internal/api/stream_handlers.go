package api

import (
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"kino/internal/models"
	"kino/internal/store"
)

// directPlayContainers are the containers a browser can play natively.
var directPlayContainers = map[string]bool{
	".mp4": true, ".m4v": true, ".webm": true, ".ogg": true, ".ogv": true,
	".mp3": true, ".m4a": true, ".flac": true, ".wav": true, ".opus": true, ".oga": true,
}

// browserVideoCodecs are the video codecs a browser can decode without help.
// HEVC is deliberately excluded: support is patchy and inconsistent enough that
// assuming it would produce a black screen rather than a clean fallback.
var browserVideoCodecs = map[string]bool{
	"h264": true, "vp8": true, "vp9": true, "av1": true,
}

// browserAudioCodecs are the audio codecs a browser can decode.
var browserAudioCodecs = map[string]bool{
	"aac": true, "mp3": true, "opus": true, "vorbis": true, "flac": true, "pcm_s16le": true,
}

// CanDirectPlay reports whether a browser can play this file as-is, which lets the
// server hand over the raw bytes and spend no CPU at all.
func CanDirectPlay(media models.MediaInfo) bool {
	if !media.Probed || !media.Available {
		return false
	}
	if !directPlayContainers[strings.ToLower(filepath.Ext(media.Path))] {
		return false
	}

	video := media.VideoStream()
	if video != nil && !browserVideoCodecs[video.Codec] {
		return false
	}

	// Every audio track must be playable: the browser picks the first one, and we
	// cannot tell it to choose differently without remuxing.
	audio := media.AudioStreams()
	if len(audio) > 0 && !browserAudioCodecs[audio[0].Codec] {
		return false
	}

	return true
}

// handleMediaFile streams the original file with HTTP range support.
//
// http.ServeContent does the heavy lifting: it parses Range headers, answers with
// 206 Partial Content and the right Content-Range, and handles conditional requests.
// This is what makes seeking work during direct play at zero CPU cost.
func (s *Server) handleMediaFile(w http.ResponseWriter, r *http.Request) {
	playable, err := s.db.ResolvePlayable(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such media.")
		return
	}

	f, err := os.Open(playable.Media.Path)
	if err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusGone,
				"The media file is no longer on disk. Rescan the library to update it.")
			return
		}
		log.Printf("api: open %s: %v", playable.Media.Path, err)
		writeError(w, http.StatusInternalServerError, "Could not open the media file.")
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not read the media file.")
		return
	}

	if ct := contentTypeFor(playable.Media.Path); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	// Media files are immutable in practice; caching avoids re-fetching on replay.
	w.Header().Set("Cache-Control", "private, max-age=86400")

	if queryBool(r, "download") {
		w.Header().Set("Content-Disposition",
			`attachment; filename="`+sanitizeFilename(filepath.Base(playable.Media.Path))+`"`)
	}

	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}

func contentTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".ts":
		return "video/mp2t"
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".opus":
		return "audio/opus"
	case ".wav":
		return "audio/wav"
	}
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// sanitizeFilename strips characters that would break a Content-Disposition header.
func sanitizeFilename(name string) string {
	return strings.NewReplacer(`"`, "", "\\", "", "\r", "", "\n", "").Replace(name)
}

// playbackInfo is what the client needs in order to start playing something.
type playbackInfo struct {
	MediaID string `json:"mediaId"`
	Title   string `json:"title"`
	// Method is "direct" (play the file as-is) or "hls" (transcoded stream).
	Method      string        `json:"method"`
	URL         string        `json:"url"`
	SessionID   string        `json:"sessionId,omitempty"`
	DurationSec float64       `json:"durationSec"`
	ResumeSec   float64       `json:"resumeSec,omitempty"`
	Container   string        `json:"container,omitempty"`
	VideoCodec  string        `json:"videoCodec,omitempty"`
	AudioCodec  string        `json:"audioCodec,omitempty"`
	Width       int           `json:"width,omitempty"`
	Height      int           `json:"height,omitempty"`
	AudioTracks []trackOption `json:"audioTracks,omitempty"`
	// Reason explains why a transcode was chosen, shown in the player's info panel.
	Reason string `json:"reason,omitempty"`
}

// handlePlaybackInfo reports how a given item should be played, without starting
// anything. The player calls this first so it knows which pipeline to set up.
func (s *Server) handlePlaybackInfo(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	id := chi.URLParam(r, "id")

	playable, err := s.db.ResolvePlayable(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such media.")
		return
	}
	if !playable.Media.Available {
		writeErrorCode(w, http.StatusGone, "FILE_MISSING",
			"The media file is no longer on disk.")
		return
	}

	info := s.buildPlaybackInfo(user.ID, playable)
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) buildPlaybackInfo(userID string, playable store.Playable) playbackInfo {
	media := playable.Media

	info := playbackInfo{
		MediaID:     playable.ID,
		Title:       playable.Title,
		DurationSec: media.DurationSec,
		Container:   media.Container,
		AudioTracks: audioOptions(media),
	}
	if v := media.VideoStream(); v != nil {
		info.VideoCodec = v.Codec
		info.Width = v.Width
		info.Height = v.Height
	}
	if a := media.AudioStreams(); len(a) > 0 {
		info.AudioCodec = a[0].Codec
	}

	if st, err := s.db.PlayStates.Get(models.PlayStateID(userID, playable.ID)); err == nil && st.Resumable() {
		info.ResumeSec = st.PositionSec
	}

	// Direct play can be disabled server-wide, which is the quickest way to reproduce
	// a playback problem against the transcoder.
	if CanDirectPlay(media) && s.cfg.Snapshot().AllowDirectPlay {
		info.Method = "direct"
		info.URL = "/api/media/" + playable.ID + "/file"
		return info
	}

	info.Method = "hls"
	if CanDirectPlay(media) {
		info.Reason = "Direct play is turned off in Settings."
		return info
	}
	info.URL = "" // filled in by /api/playback/start once a session exists
	info.Reason = transcodeReason(media)
	return info
}

// transcodeReason explains in plain language why direct play is not possible.
func transcodeReason(media models.MediaInfo) string {
	ext := strings.ToLower(filepath.Ext(media.Path))
	if !directPlayContainers[ext] {
		return "The " + strings.TrimPrefix(ext, ".") + " container is not supported by browsers."
	}
	if v := media.VideoStream(); v != nil && !browserVideoCodecs[v.Codec] {
		return strings.ToUpper(v.Codec) + " video is not supported by browsers."
	}
	if a := media.AudioStreams(); len(a) > 0 && !browserAudioCodecs[a[0].Codec] {
		return strings.ToUpper(a[0].Codec) + " audio is not supported by browsers."
	}
	return "This file needs to be converted for playback."
}
