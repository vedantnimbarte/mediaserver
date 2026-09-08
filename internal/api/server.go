package api

import (
	"io"
	"io/fs"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"kino/internal/auth"
	"kino/internal/config"
	"kino/internal/ffmpeg"
	"kino/internal/images"
	"kino/internal/logbuf"
	"kino/internal/metadata"
	"kino/internal/scanner"
	"kino/internal/store"
	"kino/internal/subtitles"
	"kino/internal/transcode"
)

// Server owns the HTTP surface and the long-lived subsystems it delegates to.
type Server struct {
	cfg        *config.Config
	db         *store.DB
	tokens     *auth.Service
	scanner    *scanner.Scanner
	transcoder *transcode.Manager
	subtitles  *subtitles.Service
	images     *images.Cache
	logs       *logbuf.Buffer
	tools      *ffmpeg.Tools

	// metadata is swapped when the API key changes, so it is guarded.
	metaMu   sync.RWMutex
	metaImpl *metadata.Enricher

	// web is the built frontend, embedded into the binary. Nil in dev mode, where
	// Vite serves the UI and proxies /api here.
	web fs.FS

	router chi.Router
}

// Options carries the dependencies New needs.
type Options struct {
	Config     *config.Config
	DB         *store.DB
	Web        fs.FS
	Scanner    *scanner.Scanner
	Transcoder *transcode.Manager
	Subtitles  *subtitles.Service
	Images     *images.Cache
	Metadata   *metadata.Enricher
	Logs       *logbuf.Buffer
	Tools      *ffmpeg.Tools
}

// New builds the server and wires every route.
func New(opts Options) *Server {
	s := &Server{
		cfg:        opts.Config,
		db:         opts.DB,
		web:        opts.Web,
		scanner:    opts.Scanner,
		transcoder: opts.Transcoder,
		subtitles:  opts.Subtitles,
		images:     opts.Images,
		metaImpl:   opts.Metadata,
		logs:       opts.Logs,
		tools:      opts.Tools,
		tokens:     auth.NewService(opts.Config.Snapshot().JWTSecret),
	}
	s.routes()
	return s
}

// metadata returns the current enricher under the lock.
func (s *Server) metadataService() *metadata.Enricher {
	s.metaMu.RLock()
	defer s.metaMu.RUnlock()
	return s.metaImpl
}

// rebuildMetadata recreates the enricher after the API key or language changes, so a
// newly entered key takes effect without a restart.
func (s *Server) rebuildMetadata() {
	cfg := s.cfg.Snapshot()
	enricher := metadata.NewEnricher(metadata.NewClient(cfg.TMDbAPIKey, cfg.MetadataLang), s.images)

	s.metaMu.Lock()
	s.metaImpl = enricher
	s.metaMu.Unlock()

	// The scanner holds its own reference, so hand it the new one too.
	if s.scanner != nil {
		s.scanner.SetMetadata(enricher)
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() {
	r := chi.NewRouter()

	r.Use(middleware.RealIP)
	r.Use(requestLogger)
	r.Use(recoverer)
	r.Use(middleware.Compress(5, "application/json", "text/html", "text/css",
		"application/javascript", "text/plain", "text/vtt"))

	r.Route("/api", func(r chi.Router) {
		// Public: reachable before anyone holds a token.
		r.Get("/health", s.handleHealth)
		r.Get("/server-info", s.handleServerInfo)
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/register", s.handleRegister)

		// Artwork is public: cache keys are unguessable hashes, and requiring a token
		// would stop the browser caching images across sessions, which is the single
		// biggest win for a poster grid.
		r.Get("/images/{key}", s.handleImage)

		// Authenticated.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Get("/auth/me", s.handleMe)
			r.Post("/auth/logout", s.handleLogout)
			r.Post("/auth/password", s.handleChangePassword)

			r.Get("/libraries", s.handleListLibraries)
			r.Get("/scan/status", s.handleScanStatus)
			r.Get("/scan/progress", s.handleScanProgress)

			r.Get("/home", s.handleHome)
			r.Get("/search", s.handleSearch)

			r.Get("/movies", s.handleListMovies)
			r.Get("/movies/{id}", s.handleGetMovie)
			r.Get("/shows", s.handleListShows)
			r.Get("/shows/{id}", s.handleGetShow)
			r.Get("/episodes/{id}", s.handleGetEpisode)

			r.Get("/artists", s.handleListArtists)
			r.Get("/artists/{id}", s.handleGetArtist)
			r.Get("/albums", s.handleListAlbums)
			r.Get("/albums/{id}", s.handleGetAlbum)

			r.Get("/photos/albums", s.handleListPhotoAlbums)
			r.Get("/photos/albums/{id}", s.handleGetPhotoAlbum)
			r.Get("/photos/{id}/file", s.handlePhotoFile)

			r.Get("/media/{id}/file", s.handleMediaFile)
			r.Get("/media/{id}/playback-info", s.handlePlaybackInfo)

			r.Get("/media/{id}/subtitles", s.handleListSubtitles)
			r.Get("/media/{id}/subtitles/{trackId}.vtt", s.handleGetSubtitle)

			r.Post("/playback/start", s.handleStartPlayback)
			r.Post("/playback/{sessionId}/stop", s.handleStopPlayback)
			r.Get("/playback/sessions", s.handleActiveSessions)

			r.Get("/preferences", s.handleGetPrefs)
			r.Put("/preferences", s.handleUpdatePrefs)
			r.Post("/preferences/reset", s.handleResetPrefs)

			r.Get("/playstate/{mediaId}", s.handleGetProgress)
			r.Post("/playstate/{mediaId}", s.handleReportProgress)
			r.Post("/playstate/{mediaId}/watched", s.handleSetWatched)
			r.Delete("/playstate/{mediaId}", s.handleClearProgress)
			r.Post("/shows/{id}/watched", s.handleMarkSeasonWatched)

			// Admin only.
			r.Group(func(r chi.Router) {
				r.Use(s.requireAdmin)

				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Patch("/users/{id}", s.handleUpdateUser)
				r.Delete("/users/{id}", s.handleDeleteUser)

				r.Post("/libraries", s.handleCreateLibrary)
				r.Patch("/libraries/{id}", s.handleUpdateLibrary)
				r.Delete("/libraries/{id}", s.handleDeleteLibrary)
				r.Post("/libraries/{id}/scan", s.handleScanLibrary)
				r.Post("/libraries/{id}/cancel-scan", s.handleCancelScan)
				r.Post("/scan/all", s.handleScanAll)

				r.Get("/browse", s.handleBrowse)

				r.Get("/settings", s.handleGetSettings)
				r.Put("/settings", s.handleUpdateSettings)
				r.Post("/settings/verify-metadata-key", s.handleVerifyMetadataKey)

				r.Get("/metadata/search", s.handleSearchMetadata)
				r.Post("/metadata/refresh", s.handleRefreshMetadata)
				r.Post("/movies/{id}/match", s.handleMatchMovie)
				r.Post("/shows/{id}/match", s.handleMatchShow)

				r.Get("/maintenance/stats", s.handleMaintenanceStats)
				r.Post("/maintenance/cache/images/clear", s.handleClearImageCache)
				r.Post("/maintenance/cache/subtitles/clear", s.handleClearSubtitleCache)
				r.Get("/maintenance/logs", s.handleLogs)
				r.Post("/maintenance/logs/clear", s.handleClearLogs)
				r.Get("/maintenance/backup", s.handleBackup)
				r.Post("/maintenance/restore", s.handleRestore)
			})
		})
	})

	// HLS lives outside /api because hls.js resolves segment URLs relative to the
	// playlist, and a shorter prefix keeps those relative paths simple.
	r.Route("/hls/{sessionId}", func(r chi.Router) {
		r.Use(s.requireAuth)

		r.Get("/master.m3u8", s.handleMasterPlaylist)
		r.Get("/{variant}/index.m3u8", s.handleVariantPlaylist)
		r.Get("/{variant}/{segment}", s.handleSegment)
	})

	// Everything not under /api is the single-page app.
	r.NotFound(s.serveWeb)

	s.router = r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

// handleServerInfo is deliberately unauthenticated: the login screen needs to know
// whether first-run setup is required before anyone can possibly hold a token.
func (s *Server) handleServerInfo(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Snapshot()

	info := map[string]any{
		"name":           "KINO",
		"version":        version(),
		"setupRequired":  s.db.Users.Len() == 0,
		"hasMetadataKey": cfg.TMDbAPIKey != "",
		"ffmpegReady":    s.tools != nil,
	}
	if s.tools != nil {
		info["ffmpegVersion"] = s.tools.Version
	} else {
		// The UI shows this verbatim, so it has to be actionable rather than just
		// telling the user something is missing.
		info["ffmpegHint"] = ffmpegInstallHint()
	}

	writeJSON(w, http.StatusOK, info)
}

// serveWeb serves the embedded SPA, falling back to index.html for client-side routes.
func (s *Server) serveWeb(w http.ResponseWriter, r *http.Request) {
	if s.web == nil {
		writeError(w, http.StatusNotFound,
			"The web UI is not embedded in this build. Run the Vite dev server, or build with `npm run build` before `go build`.")
		return
	}

	upath := strings.TrimPrefix(r.URL.Path, "/")
	if upath == "" {
		upath = "index.html"
	}

	f, err := s.web.Open(upath)
	if err != nil {
		// Unknown path with no file extension: a client-side route, serve the shell.
		if !strings.Contains(pathTail(upath), ".") {
			s.serveIndex(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		s.serveIndex(w, r)
		return
	}

	// Vite emits content-hashed asset filenames, so /assets/* is safe to cache hard.
	if strings.HasPrefix(upath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	rs, ok := f.(io.ReadSeeker)
	if !ok {
		// Should not happen for embed.FS, but degrade rather than panic.
		http.Error(w, "cannot serve asset", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), rs)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := s.web.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		http.Error(w, "cannot serve index", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", stat.ModTime(), rs)
}

func pathTail(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// requestLogger logs one line per request, skipping the high-frequency segment
// requests that would otherwise drown the log during playback.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/hls/") || strings.HasPrefix(r.URL.Path, "/api/images/") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, ww.Status(), time.Since(start).Round(time.Millisecond))
	})
}

// recoverer turns a handler panic into a 500 instead of killing the process, which
// matters because a single bad media file should never take the whole server down.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec) // the standard library's signal for a deliberate abort
				}
				log.Printf("panic serving %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				writeError(w, http.StatusInternalServerError, "Internal server error.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
