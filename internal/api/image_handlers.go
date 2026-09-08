package api

import (
	"errors"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"kino/internal/images"
)

// handleImage serves cached artwork, optionally resized.
//
// Cache keys are content-derived and the bytes behind one never change, so this can
// be cached in the browser indefinitely. That matters: a poster grid issues dozens of
// image requests per screen.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	if s.images == nil {
		http.NotFound(w, r)
		return
	}

	key := chi.URLParam(r, "key")
	width := queryIntClamped(r, "w", 0, 0, 2000)

	path, err := s.images.Open(key, width)
	if err != nil {
		if errors.Is(err, images.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, "Could not read the image.")
		return
	}

	f, err := os.Open(path)
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

	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	// Let the browser sniff between JPEG and PNG rather than guessing wrong: TMDb
	// serves both, and the originals are stored without an extension.
	http.ServeContent(w, r, stat.Name(), stat.ModTime(), f)
}
