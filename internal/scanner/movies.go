package scanner

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"kino/internal/models"
)

// acceptVideo is the walker filter for video libraries: playable containers only,
// with samples, trailers and extras excluded.
func acceptVideo(path string) bool {
	if !IsVideoFile(path) || IsJunkFile(path) {
		return false
	}
	return true
}

// scanMovies rebuilds a movie library from disk.
//
// The scan is incremental: files whose size and modification time are unchanged since
// the last scan skip ffprobe entirely, which is what makes a rescan of a large library
// take seconds instead of minutes.
func (s *Scanner) scanMovies(ctx context.Context, lib models.Library, prog *Progress) error {
	files, err := walkFiles(ctx, lib.Paths, acceptVideo)
	if err != nil {
		return err
	}

	// Index what we already know, so unchanged files can skip probing.
	existing := make(map[string]models.Movie)
	for _, m := range s.db.Movies.Filter(func(m models.Movie) bool { return m.LibraryID == lib.ID }) {
		existing[normalizePathKey(m.Media.Path)] = m
	}

	var (
		toProbe   []string
		unchanged []models.Movie
	)
	seenIDs := make(map[string]bool)

	for _, path := range files {
		id := StableID("mv", path)
		seenIDs[id] = true

		stat, err := os.Stat(path)
		if err != nil {
			log.Printf("scanner: stat %s: %v", path, err)
			continue
		}
		// Skip obvious sample files that slipped past the name check.
		if stat.Size() < MinFeatureBytes && IsJunkFile(path) {
			continue
		}

		if prev, ok := existing[normalizePathKey(path)]; ok && prev.Media.Unchanged(stat.Size(), stat.ModTime()) {
			prev.Media.Available = true
			unchanged = append(unchanged, prev)
			continue
		}
		toProbe = append(toProbe, path)
	}

	prog.Phase = PhaseProbing
	prog.Total = len(toProbe)
	prog.Done = 0
	s.publish(prog)

	infos := s.probeAll(ctx, toProbe, prog)
	if err := ctx.Err(); err != nil {
		return err
	}

	now := time.Now()
	updates := make([]models.Movie, 0, len(toProbe)+len(unchanged))

	for i, path := range toProbe {
		info := infos[i]
		if info.Path == "" {
			info.Path = path
		}

		id := StableID("mv", path)
		parsed := ParseMovieName(path)
		title := parsed.Title
		if title == "" {
			title = stemOf(path)
		}

		movie, existed := existing[normalizePathKey(path)]
		if !existed {
			movie = models.Movie{
				ID:         id,
				LibraryID:  lib.ID,
				AddedAt:    now,
				MetaStatus: models.MetaPending,
			}
			prog.Added++
		} else {
			prog.Updated++
		}

		movie.Media = info
		movie.UpdatedAt = now

		// A manual metadata correction must survive rescans, so only the file-derived
		// fields are refreshed for a locked item.
		if !movie.MatchLocked {
			movie.Title = title
			movie.SortTitle = SortTitle(title)
			movie.Year = parsed.Year
			if parsed.Edition != "" {
				movie.Tagline = parsed.Edition
			}
			if movie.MetaStatus == models.MetaMatched {
				// The file changed underneath us; re-derive metadata next pass.
				movie.MetaStatus = models.MetaPending
			}
		}

		updates = append(updates, movie)
	}

	updates = append(updates, unchanged...)
	if err := s.db.Movies.PutMany(updates); err != nil {
		return err
	}

	// Anything in this library we did not see on disk is gone. Mark it unavailable
	// rather than deleting it, so watch history survives an unplugged drive.
	prog.Phase = PhaseCleaning
	s.publish(prog)

	for _, m := range s.db.Movies.Filter(func(m models.Movie) bool { return m.LibraryID == lib.ID }) {
		if seenIDs[m.ID] || !m.Media.Available {
			continue
		}
		_, _ = s.db.Movies.Update(m.ID, func(mv *models.Movie) { mv.Media.Available = false })
		prog.Removed++
	}

	s.updateLibraryCount(lib.ID, s.db.Movies.Filter(func(m models.Movie) bool {
		return m.LibraryID == lib.ID && m.Media.Available
	}))

	return s.enrichMovies(ctx, lib, prog)
}

// enrichMovies fills in provider metadata for items that still need it.
func (s *Scanner) enrichMovies(ctx context.Context, lib models.Library, prog *Progress) error {
	meta := s.metadataService()
	if meta == nil || !meta.Enabled() {
		// No provider configured: mark items so the UI can explain why they look bare
		// instead of leaving them stuck on "pending" forever.
		for _, m := range s.db.Movies.Filter(func(m models.Movie) bool {
			return m.LibraryID == lib.ID && m.MetaStatus == models.MetaPending
		}) {
			_, _ = s.db.Movies.Update(m.ID, func(mv *models.Movie) { mv.MetaStatus = models.MetaLocal })
		}
		return nil
	}

	pending := s.db.Movies.Filter(func(m models.Movie) bool {
		return m.LibraryID == lib.ID && m.Media.Available && !m.MatchLocked &&
			(m.MetaStatus == models.MetaPending || m.MetaStatus == models.MetaLocal)
	})
	if len(pending) == 0 {
		return nil
	}

	prog.Phase = PhaseMetadata
	prog.Total = len(pending)
	prog.Done = 0
	s.publish(prog)

	for _, movie := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}

		prog.Current = movie.Title
		enriched := movie
		if err := meta.EnrichMovie(ctx, &enriched); err != nil {
			log.Printf("scanner: metadata for %q: %v", movie.Title, err)
			enriched.MetaStatus = models.MetaError
		}
		_ = s.db.Movies.Put(enriched)

		prog.Done++
		s.publish(prog)
	}

	return nil
}

func (s *Scanner) updateLibraryCount(libraryID string, items any) {
	count := 0
	switch v := items.(type) {
	case []models.Movie:
		count = len(v)
	case []models.Show:
		count = len(v)
	case []models.Artist:
		count = len(v)
	case []models.PhotoAlbum:
		count = len(v)
	}
	_, _ = s.db.Libraries.Update(libraryID, func(l *models.Library) { l.ItemCount = count })
}

// stemOfPath is a small convenience used where only the display name matters.
func stemOfPath(path string) string {
	return stemOf(filepath.Base(path))
}
