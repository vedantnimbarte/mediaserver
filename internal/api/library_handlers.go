package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"kino/internal/models"
	"kino/internal/scanner"
)

func (s *Server) handleListLibraries(w http.ResponseWriter, r *http.Request) {
	libs := s.db.Libraries.All()
	sort.Slice(libs, func(i, j int) bool { return libs[i].Name < libs[j].Name })
	if libs == nil {
		libs = []models.Library{}
	}
	writeJSON(w, http.StatusOK, libs)
}

type libraryRequest struct {
	Name  string             `json:"name"`
	Type  models.LibraryType `json:"type"`
	Paths []string           `json:"paths"`
}

func (s *Server) handleCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var req libraryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "A library name is required.")
		return
	}
	if !req.Type.Valid() {
		writeError(w, http.StatusBadRequest,
			"Library type must be one of: movie, show, music, photo.")
		return
	}

	paths, err := validatePaths(req.Paths)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	lib := models.Library{
		ID:        uuid.NewString(),
		Name:      req.Name,
		Type:      req.Type,
		Paths:     paths,
		CreatedAt: time.Now(),
	}
	if err := s.db.Libraries.Put(lib); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not save the library.")
		return
	}

	writeJSON(w, http.StatusCreated, lib)
}

func (s *Server) handleUpdateLibrary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := s.db.Libraries.Get(id); err != nil {
		writeError(w, http.StatusNotFound, "No such library.")
		return
	}

	var req libraryRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	var paths []string
	if req.Paths != nil {
		var err error
		if paths, err = validatePaths(req.Paths); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	updated, err := s.db.Libraries.Update(id, func(l *models.Library) {
		if name := strings.TrimSpace(req.Name); name != "" {
			l.Name = name
		}
		if paths != nil {
			l.Paths = paths
		}
		// The type is deliberately immutable: changing it would orphan every item
		// already scanned into the old collection.
	})
	if err != nil {
		writeError(w, storeErrStatus(err), "Could not update the library.")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	lib, err := s.db.Libraries.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such library.")
		return
	}

	if s.scanner != nil {
		s.scanner.Cancel(id)
	}

	// Remove the library's items. Media files on disk are never touched.
	switch lib.Type {
	case models.LibraryMovie:
		s.db.Movies.DeleteWhere(func(m models.Movie) bool { return m.LibraryID == id })
	case models.LibraryShow:
		s.db.Shows.DeleteWhere(func(sh models.Show) bool { return sh.LibraryID == id })
	case models.LibraryMusic:
		s.db.Artists.DeleteWhere(func(a models.Artist) bool { return a.LibraryID == id })
	case models.LibraryPhoto:
		s.db.PhotoAlbums.DeleteWhere(func(a models.PhotoAlbum) bool { return a.LibraryID == id })
	}

	s.db.Libraries.Delete(id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleScanLibrary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if s.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "The scanner is not available.")
		return
	}

	if err := s.scanner.Scan(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, scanner.ErrAlreadyScanning):
			writeError(w, http.StatusConflict, "This library is already being scanned.")
		case errors.Is(err, scanner.ErrNoProbe):
			writeErrorCode(w, http.StatusServiceUnavailable, "NO_FFMPEG",
				"ffprobe could not be found, so media cannot be scanned. "+s.ffmpegHint())
		default:
			writeError(w, http.StatusInternalServerError, "Could not start the scan: "+err.Error())
		}
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "scanning"})
}

func (s *Server) handleScanAll(w http.ResponseWriter, r *http.Request) {
	if s.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "The scanner is not available.")
		return
	}
	started := 0
	for _, lib := range s.db.Libraries.All() {
		if err := s.scanner.Scan(r.Context(), lib.ID); err == nil {
			started++
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]int{"started": started})
}

func (s *Server) handleCancelScan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if s.scanner == nil || !s.scanner.Cancel(id) {
		writeError(w, http.StatusNotFound, "No scan is running for this library.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

// handleScanStatus returns the latest progress snapshot for every library, which the
// UI uses to render immediately on load without waiting for the next SSE event.
func (s *Server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	if s.scanner == nil {
		writeJSON(w, http.StatusOK, []scanner.Progress{})
		return
	}
	progress := s.scanner.Progress()
	if progress == nil {
		progress = []scanner.Progress{}
	}
	writeJSON(w, http.StatusOK, progress)
}

// handleScanProgress streams scan progress as Server-Sent Events.
func (s *Server) handleScanProgress(w http.ResponseWriter, r *http.Request) {
	if s.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "The scanner is not available.")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming is not supported on this connection.")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Disable proxy buffering, which would otherwise hold events until the scan ends.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, unsubscribe := s.scanner.Hub().Subscribe()
	defer unsubscribe()

	// Send the current state immediately so a client that connects mid-scan is not
	// left staring at an empty progress bar until the next update.
	for _, p := range s.scanner.Progress() {
		writeSSE(w, p)
	}
	flusher.Flush()

	// A periodic comment keeps intermediaries from closing an idle connection.
	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case p, ok := <-events:
			if !ok {
				return
			}
			writeSSE(w, p)
			flusher.Flush()

		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, v any) {
	data, err := marshalJSON(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// ---- filesystem browser ----

type dirEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

// handleBrowse lets the admin UI pick library folders without typing paths by hand.
// It only ever lists directories, and never reveals file contents.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")

	// An empty path means "show me the roots": drive letters on Windows, / elsewhere.
	if path == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"path":    "",
			"parent":  "",
			"entries": listRoots(),
		})
		return
	}

	path = filepath.Clean(path)
	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Cannot read that folder: "+err.Error())
		return
	}

	out := make([]dirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out = append(out, dirEntry{
			Name:  e.Name(),
			Path:  filepath.Join(path, e.Name()),
			IsDir: true,
		})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })

	parent := filepath.Dir(path)
	if parent == path {
		parent = "" // already at a root
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    path,
		"parent":  parent,
		"entries": out,
	})
}

// ---- helpers ----

// validatePaths cleans and checks the folders a library points at.
func validatePaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("Add at least one folder to this library.")
	}

	seen := make(map[string]bool)
	out := make([]string, 0, len(paths))

	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid path.", p)
		}
		abs = filepath.Clean(abs)

		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("The folder %q does not exist.", p)
			}
			return nil, fmt.Errorf("Cannot access %q: %v", p, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%q is a file, not a folder.", p)
		}
		if seen[strings.ToLower(abs)] {
			continue
		}
		seen[strings.ToLower(abs)] = true
		out = append(out, abs)
	}

	if len(out) == 0 {
		return nil, errors.New("Add at least one folder to this library.")
	}
	return out, nil
}

func (s *Server) ffmpegHint() string {
	return ffmpegInstallHint()
}
