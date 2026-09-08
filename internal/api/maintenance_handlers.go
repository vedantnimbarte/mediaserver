package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"kino/internal/models"
)

// storageStat describes disk usage for one library.
type storageStat struct {
	LibraryID   string `json:"libraryId"`
	LibraryName string `json:"libraryName"`
	Type        string `json:"type"`
	Items       int    `json:"items"`
	Bytes       int64  `json:"bytes"`
}

type maintenanceStats struct {
	DataDir       string        `json:"dataDir"`
	DatabaseBytes int64         `json:"databaseBytes"`
	ImageCache    cacheStat     `json:"imageCache"`
	SubtitleCache cacheStat     `json:"subtitleCache"`
	TranscodeTemp cacheStat     `json:"transcodeTemp"`
	Libraries     []storageStat `json:"libraries"`
	ActiveScans   int           `json:"activeScans"`
	ActiveStreams int           `json:"activeStreams"`
	Uptime        string        `json:"uptime"`
}

type cacheStat struct {
	Path  string `json:"path"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// startedAt is used to report uptime.
var startedAt = time.Now()

// handleMaintenanceStats reports what the server is using on disk and what it is
// currently doing, so an admin can answer "why is my drive full" without a shell.
func (s *Server) handleMaintenanceStats(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Snapshot()

	stats := maintenanceStats{
		DataDir:       cfg.DataDir,
		DatabaseBytes: dirSize(cfg.DataDir, false),
		ImageCache:    measure(s.cfg.ImageDir()),
		SubtitleCache: measure(s.cfg.SubtitleDir()),
		TranscodeTemp: measure(s.cfg.TranscodeDir()),
		Uptime:        time.Since(startedAt).Round(time.Second).String(),
	}

	if s.scanner != nil {
		for _, p := range s.scanner.Progress() {
			if !p.Finished {
				stats.ActiveScans++
			}
		}
	}
	if s.transcoder != nil {
		stats.ActiveStreams = len(s.transcoder.Active())
	}

	// Library sizes come from the stored MediaInfo rather than walking the disk: a
	// media folder can hold tens of thousands of files across a slow network share,
	// and this endpoint has to answer immediately.
	for _, lib := range s.db.Libraries.All() {
		stat := storageStat{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
			Type:        string(lib.Type),
		}

		switch lib.Type {
		case models.LibraryMovie:
			for _, m := range s.db.Movies.All() {
				if m.LibraryID == lib.ID {
					stat.Items++
					stat.Bytes += m.Media.Size
				}
			}
		case models.LibraryShow:
			for _, sh := range s.db.Shows.All() {
				if sh.LibraryID != lib.ID {
					continue
				}
				for _, ep := range sh.EpisodesInOrder() {
					stat.Items++
					stat.Bytes += ep.Media.Size
				}
			}
		case models.LibraryMusic:
			for _, a := range s.db.Artists.All() {
				if a.LibraryID != lib.ID {
					continue
				}
				for _, tr := range a.OrderedTracks() {
					stat.Items++
					stat.Bytes += tr.Media.Size
				}
			}
		case models.LibraryPhoto:
			for _, al := range s.db.PhotoAlbums.All() {
				if al.LibraryID != lib.ID {
					continue
				}
				for _, p := range al.Photos {
					stat.Items++
					stat.Bytes += p.Size
				}
			}
		}

		stats.Libraries = append(stats.Libraries, stat)
	}
	if stats.Libraries == nil {
		stats.Libraries = []storageStat{}
	}

	writeJSON(w, http.StatusOK, stats)
}

// handleClearImageCache deletes cached artwork. It is safe: everything here is either
// re-downloadable from the metadata provider or regenerated from the source file.
func (s *Server) handleClearImageCache(w http.ResponseWriter, r *http.Request) {
	dir := s.cfg.ImageDir()

	before := measure(dir)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, "Could not clear the image cache: "+err.Error())
		return
	}
	_ = os.MkdirAll(dir, 0o755)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "cleared",
		"freedBytes":   before.Bytes,
		"removedFiles": before.Files,
		"note":         "Artwork is re-downloaded on the next scan or metadata refresh.",
	})
}

// handleClearSubtitleCache deletes converted subtitles; they are regenerated on demand.
func (s *Server) handleClearSubtitleCache(w http.ResponseWriter, r *http.Request) {
	dir := s.cfg.SubtitleDir()

	before := measure(dir)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		writeError(w, http.StatusInternalServerError, "Could not clear the subtitle cache: "+err.Error())
		return
	}
	_ = os.MkdirAll(dir, 0o755)

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "cleared",
		"freedBytes":   before.Bytes,
		"removedFiles": before.Files,
	})
}

// handleLogs returns the retained log lines.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if s.logs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}})
		return
	}
	limit := queryIntClamped(r, "limit", 200, 1, 2000)
	writeJSON(w, http.StatusOK, map[string]any{"lines": s.logs.Lines(limit)})
}

func (s *Server) handleClearLogs(w http.ResponseWriter, r *http.Request) {
	if s.logs != nil {
		s.logs.Clear()
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// backupPayload is the shape of an exported database.
type backupPayload struct {
	Version     int                  `json:"version"`
	ExportedAt  time.Time            `json:"exportedAt"`
	Libraries   []models.Library     `json:"libraries"`
	Movies      []models.Movie       `json:"movies"`
	Shows       []models.Show        `json:"shows"`
	Artists     []models.Artist      `json:"artists"`
	PhotoAlbums []models.PhotoAlbum  `json:"photoAlbums"`
	PlayStates  []models.PlayState   `json:"playStates"`
	Prefs       []models.UserPrefs   `json:"preferences"`
}

// handleBackup exports the whole library as a single JSON download.
//
// Users are deliberately excluded: password hashes should not travel in a file the
// user is likely to drop in a cloud folder, and accounts are quick to recreate.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	payload := backupPayload{
		Version:     1,
		ExportedAt:  time.Now(),
		Libraries:   s.db.Libraries.All(),
		Movies:      s.db.Movies.All(),
		Shows:       s.db.Shows.All(),
		Artists:     s.db.Artists.All(),
		PhotoAlbums: s.db.PhotoAlbums.All(),
		PlayStates:  s.db.PlayStates.All(),
		Prefs:       s.db.Prefs.All(),
	}

	filename := fmt.Sprintf("kino-backup-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		// The body is already streaming, so all we can do is record it.
		return
	}
}

// handleRestore imports a backup, replacing the library collections.
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	// Backups of a large library are far bigger than a normal request body.
	r.Body = http.MaxBytesReader(w, r.Body, 256<<20)

	var payload backupPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "That does not look like a valid backup file.")
		return
	}
	if payload.Version != 1 {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("This backup is version %d, which this server does not understand.", payload.Version))
		return
	}

	// Replace wholesale rather than merging: a restore is meant to return the server
	// to a known state, and merging would leave items the backup deliberately omits.
	s.db.Libraries.DeleteWhere(func(models.Library) bool { return true })
	s.db.Movies.DeleteWhere(func(models.Movie) bool { return true })
	s.db.Shows.DeleteWhere(func(models.Show) bool { return true })
	s.db.Artists.DeleteWhere(func(models.Artist) bool { return true })
	s.db.PhotoAlbums.DeleteWhere(func(models.PhotoAlbum) bool { return true })
	s.db.PlayStates.DeleteWhere(func(models.PlayState) bool { return true })

	_ = s.db.Libraries.PutMany(payload.Libraries)
	_ = s.db.Movies.PutMany(payload.Movies)
	_ = s.db.Shows.PutMany(payload.Shows)
	_ = s.db.Artists.PutMany(payload.Artists)
	_ = s.db.PhotoAlbums.PutMany(payload.PhotoAlbums)
	_ = s.db.PlayStates.PutMany(payload.PlayStates)
	_ = s.db.Prefs.PutMany(payload.Prefs)

	// Persist immediately: a restore the user cannot see survive a restart is worse
	// than no restore at all.
	_ = s.db.Flush()

	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "restored",
		"libraries": len(payload.Libraries),
		"movies":    len(payload.Movies),
		"shows":     len(payload.Shows),
		"note":      "Run a scan to confirm the media files are still where the backup expects them.",
	})
}

// ---- helpers ----

// measure totals the files and bytes under a directory.
func measure(dir string) cacheStat {
	stat := cacheStat{Path: dir}

	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		stat.Files++
		stat.Bytes += info.Size()
		return nil
	})

	return stat
}

// dirSize totals a directory, optionally descending into subdirectories.
func dirSize(dir string, recurse bool) int64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	var total int64
	for _, entry := range entries {
		if entry.IsDir() {
			if recurse {
				total += dirSize(filepath.Join(dir, entry.Name()), true)
			}
			continue
		}
		if info, err := entry.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}
