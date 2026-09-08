package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"kino/internal/config"
	"kino/internal/metadata"
	"kino/internal/models"
)

// settingsView is what the Settings screen renders. The API key is never sent back;
// only whether one is configured.
type settingsView struct {
	Port            int    `json:"port"`
	HasMetadataKey  bool   `json:"hasMetadataKey"`
	MetadataLang    string `json:"metadataLang"`
	FFmpegPath      string `json:"ffmpegPath"`
	FFprobePath     string `json:"ffprobePath"`
	FFmpegReady     bool   `json:"ffmpegReady"`
	FFmpegVersion   string `json:"ffmpegVersion,omitempty"`
	HWAccel         string `json:"hwAccel"`
	ActiveEncoder   string `json:"activeEncoder,omitempty"`
	AvailableAccels []encoderView `json:"availableAccels"`
	MaxTranscodes   int    `json:"maxTranscodes"`
	SegmentSeconds  int    `json:"segmentSeconds"`
	SessionIdleSecs int    `json:"sessionIdleSecs"`
	ScanOnStart     bool   `json:"scanOnStart"`
	DataDir         string `json:"dataDir"`

	EncoderPreset            string         `json:"encoderPreset"`
	BitrateOverrides         map[string]int `json:"bitrateOverrides"`
	BurnInSubtitlesByDefault bool           `json:"burnInSubtitlesByDefault"`
	AllowDirectPlay          bool           `json:"allowDirectPlay"`
	AudioChannels            int            `json:"audioChannels"`
	// PresetOptions are the values the encoder in use actually accepts, so the UI
	// never offers a preset that would make ffmpeg refuse to start.
	PresetOptions []string `json:"presetOptions"`
}

type encoderView struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Snapshot()

	view := settingsView{
		Port:            cfg.Port,
		HasMetadataKey:  cfg.TMDbAPIKey != "",
		MetadataLang:    cfg.MetadataLang,
		FFmpegPath:      cfg.FFmpegPath,
		FFprobePath:     cfg.FFprobePath,
		FFmpegReady:     s.tools != nil,
		HWAccel:         cfg.HWAccel,
		MaxTranscodes:   cfg.MaxTranscodes,
		SegmentSeconds:  cfg.SegmentSeconds,
		SessionIdleSecs: cfg.SessionIdleSecs,
		ScanOnStart:     cfg.ScanOnStart,
		DataDir:         cfg.DataDir,

		EncoderPreset:            cfg.EncoderPreset,
		BitrateOverrides:         cfg.BitrateOverrides,
		BurnInSubtitlesByDefault: cfg.BurnInSubtitlesByDefault,
		AllowDirectPlay:          cfg.AllowDirectPlay,
		AudioChannels:            cfg.AudioChannels,
	}
	if view.BitrateOverrides == nil {
		view.BitrateOverrides = map[string]int{}
	}
	if s.tools != nil {
		view.FFmpegVersion = s.tools.Version
	}

	view.PresetOptions = presetsFor(s.activeEncoderKind())

	// Always offer CPU; it is the fallback that always exists.
	view.AvailableAccels = []encoderView{{Kind: "cpu", Label: "CPU (libx264)"}}
	if s.transcoder != nil {
		view.ActiveEncoder = s.transcoder.Encoder().Label
		for _, enc := range s.transcoder.Capabilities().Available {
			view.AvailableAccels = append(view.AvailableAccels, encoderView{Kind: enc.Kind, Label: enc.Label})
		}
	}

	writeJSON(w, http.StatusOK, view)
}

// settingsUpdate uses pointers throughout so an omitted field means "leave alone"
// rather than "reset to zero".
type settingsUpdate struct {
	TMDbAPIKey      *string `json:"tmdbApiKey,omitempty"`
	MetadataLang    *string `json:"metadataLang,omitempty"`
	FFmpegPath      *string `json:"ffmpegPath,omitempty"`
	FFprobePath     *string `json:"ffprobePath,omitempty"`
	HWAccel         *string `json:"hwAccel,omitempty"`
	MaxTranscodes   *int    `json:"maxTranscodes,omitempty"`
	SegmentSeconds  *int    `json:"segmentSeconds,omitempty"`
	SessionIdleSecs *int    `json:"sessionIdleSecs,omitempty"`
	ScanOnStart     *bool   `json:"scanOnStart,omitempty"`
	Port            *int    `json:"port,omitempty"`

	EncoderPreset            *string         `json:"encoderPreset,omitempty"`
	BitrateOverrides         *map[string]int `json:"bitrateOverrides,omitempty"`
	BurnInSubtitlesByDefault *bool           `json:"burnInSubtitlesByDefault,omitempty"`
	AllowDirectPlay          *bool           `json:"allowDirectPlay,omitempty"`
	AudioChannels            *int            `json:"audioChannels,omitempty"`
}

// activeEncoderKind reports which encoder family is in use, for preset options.
func (s *Server) activeEncoderKind() string {
	if s.transcoder == nil {
		return "cpu"
	}
	return s.transcoder.Encoder().Kind
}

// presetsFor lists the presets a given encoder accepts. Offering libx264's names to
// NVENC (or the reverse) would produce an ffmpeg error at playback time.
func presetsFor(kind string) []string {
	switch kind {
	case "nvenc":
		return []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7"}
	case "qsv":
		return []string{"veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow"}
	case "amf":
		return []string{"speed", "balanced", "quality"}
	case "videotoolbox":
		return []string{}
	default:
		return []string{"ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow"}
	}
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsUpdate
	if !decodeJSON(w, r, &req) {
		return
	}

	// Some changes only take effect on restart; tell the user which, rather than
	// leaving them wondering why nothing happened.
	var needsRestart []string

	err := s.cfg.Update(func(c *config.Config) {
		if req.TMDbAPIKey != nil {
			c.TMDbAPIKey = strings.TrimSpace(*req.TMDbAPIKey)
		}
		if req.MetadataLang != nil && *req.MetadataLang != "" {
			c.MetadataLang = *req.MetadataLang
		}
		if req.FFmpegPath != nil && *req.FFmpegPath != c.FFmpegPath {
			c.FFmpegPath = strings.TrimSpace(*req.FFmpegPath)
			needsRestart = append(needsRestart, "ffmpeg path")
		}
		if req.FFprobePath != nil && *req.FFprobePath != c.FFprobePath {
			c.FFprobePath = strings.TrimSpace(*req.FFprobePath)
			needsRestart = append(needsRestart, "ffprobe path")
		}
		if req.HWAccel != nil && *req.HWAccel != c.HWAccel {
			c.HWAccel = *req.HWAccel
			needsRestart = append(needsRestart, "hardware acceleration")
		}
		if req.MaxTranscodes != nil && *req.MaxTranscodes > 0 && *req.MaxTranscodes != c.MaxTranscodes {
			c.MaxTranscodes = *req.MaxTranscodes
			needsRestart = append(needsRestart, "transcode limit")
		}
		if req.SegmentSeconds != nil && *req.SegmentSeconds >= 2 && *req.SegmentSeconds != c.SegmentSeconds {
			c.SegmentSeconds = *req.SegmentSeconds
			needsRestart = append(needsRestart, "segment length")
		}
		if req.SessionIdleSecs != nil && *req.SessionIdleSecs >= 10 {
			c.SessionIdleSecs = *req.SessionIdleSecs
		}
		if req.ScanOnStart != nil {
			c.ScanOnStart = *req.ScanOnStart
		}
		if req.Port != nil && *req.Port > 0 && *req.Port < 65536 && *req.Port != c.Port {
			c.Port = *req.Port
			needsRestart = append(needsRestart, "port")
		}

		if req.EncoderPreset != nil && *req.EncoderPreset != c.EncoderPreset {
			c.EncoderPreset = *req.EncoderPreset
			needsRestart = append(needsRestart, "encoder preset")
		}
		if req.BitrateOverrides != nil {
			// Drop entries below the floor rather than storing a value the ladder
			// would silently ignore.
			cleaned := map[string]int{}
			for name, bitrate := range *req.BitrateOverrides {
				if bitrate >= 200_000 {
					cleaned[name] = bitrate
				}
			}
			c.BitrateOverrides = cleaned
			needsRestart = append(needsRestart, "bitrate overrides")
		}
		if req.BurnInSubtitlesByDefault != nil {
			c.BurnInSubtitlesByDefault = *req.BurnInSubtitlesByDefault
		}
		if req.AllowDirectPlay != nil {
			// Takes effect on the next playback start, so no restart is needed.
			c.AllowDirectPlay = *req.AllowDirectPlay
		}
		if req.AudioChannels != nil && *req.AudioChannels >= 1 && *req.AudioChannels <= 8 &&
			*req.AudioChannels != c.AudioChannels {
			c.AudioChannels = *req.AudioChannels
			needsRestart = append(needsRestart, "audio channels")
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save the settings.")
		return
	}

	// The metadata client holds the key, so rebuild it immediately: requiring a
	// restart just to start fetching posters would be a poor first-run experience.
	if req.TMDbAPIKey != nil || req.MetadataLang != nil {
		s.rebuildMetadata()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "saved",
		"needsRestart": needsRestart,
	})
}

// handleVerifyMetadataKey checks the configured TMDb key against the API.
func (s *Server) handleVerifyMetadataKey(w http.ResponseWriter, r *http.Request) {
	md := s.metadataService()
	if md == nil || !md.Enabled() {
		writeErrorCode(w, http.StatusBadRequest, "NO_KEY",
			"No TMDb API key is configured. Add one in Settings to fetch posters and descriptions.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	if err := md.VerifyKey(ctx); err != nil {
		if errors.Is(err, metadata.ErrBadAPIKey) {
			writeErrorCode(w, http.StatusBadRequest, "BAD_KEY", "TMDb rejected that API key.")
			return
		}
		writeError(w, http.StatusBadGateway, "Could not reach TMDb: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- manual metadata matching ----

// handleSearchMetadata lists provider candidates so an admin can correct a bad match.
func (s *Server) handleSearchMetadata(w http.ResponseWriter, r *http.Request) {
	md := s.metadataService()
	if md == nil || !md.Enabled() {
		writeErrorCode(w, http.StatusBadRequest, "NO_KEY", "No TMDb API key is configured.")
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "A search term is required.")
		return
	}
	year := queryInt(r, "year", 0)

	var (
		candidates []metadata.Candidate
		err        error
	)
	if r.URL.Query().Get("type") == "show" {
		candidates, err = md.SearchShows(r.Context(), query, year)
	} else {
		candidates, err = md.SearchMovies(r.Context(), query, year)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "Search failed: "+err.Error())
		return
	}
	if candidates == nil {
		candidates = []metadata.Candidate{}
	}

	writeJSON(w, http.StatusOK, candidates)
}

type matchRequest struct {
	TMDbID int `json:"tmdbId"`
}

// handleMatchMovie pins a movie to a specific TMDb entry and re-fetches its metadata.
func (s *Server) handleMatchMovie(w http.ResponseWriter, r *http.Request) {
	md := s.metadataService()
	if md == nil || !md.Enabled() {
		writeErrorCode(w, http.StatusBadRequest, "NO_KEY", "No TMDb API key is configured.")
		return
	}

	movie, err := s.db.Movies.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such movie.")
		return
	}

	var req matchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.TMDbID <= 0 {
		writeError(w, http.StatusBadRequest, "A TMDb ID is required.")
		return
	}

	movie.TMDbID = req.TMDbID
	if err := md.EnrichMovie(r.Context(), &movie); err != nil {
		writeError(w, http.StatusBadGateway, "Could not fetch that entry: "+err.Error())
		return
	}
	// Lock the match so the next rescan does not overwrite this correction.
	movie.MatchLocked = true
	movie.UpdatedAt = time.Now()

	if err := s.db.Movies.Put(movie); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save the match.")
		return
	}
	writeJSON(w, http.StatusOK, movie)
}

// handleMatchShow pins a show to a specific TMDb entry.
func (s *Server) handleMatchShow(w http.ResponseWriter, r *http.Request) {
	md := s.metadataService()
	if md == nil || !md.Enabled() {
		writeErrorCode(w, http.StatusBadRequest, "NO_KEY", "No TMDb API key is configured.")
		return
	}

	show, err := s.db.Shows.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such show.")
		return
	}

	var req matchRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.TMDbID <= 0 {
		writeError(w, http.StatusBadRequest, "A TMDb ID is required.")
		return
	}

	show.TMDbID = req.TMDbID
	if err := md.EnrichShow(r.Context(), &show); err != nil {
		writeError(w, http.StatusBadGateway, "Could not fetch that entry: "+err.Error())
		return
	}
	show.MatchLocked = true
	show.UpdatedAt = time.Now()

	if err := s.db.Shows.Put(show); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save the match.")
		return
	}
	writeJSON(w, http.StatusOK, show)
}

// handleRefreshMetadata clears cached metadata so the next scan looks it up again.
func (s *Server) handleRefreshMetadata(w http.ResponseWriter, r *http.Request) {
	unlock := queryBool(r, "unlock")

	movies := 0
	for _, m := range s.db.Movies.All() {
		if m.MatchLocked && !unlock {
			continue
		}
		_, _ = s.db.Movies.Update(m.ID, func(x *models.Movie) {
			x.MetaStatus = models.MetaPending
			if unlock {
				x.MatchLocked = false
				x.TMDbID = 0
			}
		})
		movies++
	}

	shows := 0
	for _, sh := range s.db.Shows.All() {
		if sh.MatchLocked && !unlock {
			continue
		}
		_, _ = s.db.Shows.Update(sh.ID, func(x *models.Show) {
			x.MetaStatus = models.MetaPending
			if unlock {
				x.MatchLocked = false
				x.TMDbID = 0
			}
		})
		shows++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "queued",
		"movies": movies,
		"shows":  shows,
		"note":   "Run a scan to fetch the refreshed metadata.",
	})
}
