package scanner

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"kino/internal/models"
)

// showFolderOf determines the directory that represents the series.
//
// For "Show/Season 01/ep.mkv" that is the grandparent; for "Show/ep.mkv" the parent.
// Anchoring on a folder gives shows a stable identity even when episode filenames are
// inconsistent, which they very often are.
func showFolderOf(path string) string {
	dir := filepath.Dir(path)
	if _, isSeason := seasonFromFolder(path); isSeason {
		return filepath.Dir(dir)
	}
	return dir
}

// candidate is one episode file plus what its path told us, carried from the walk
// through to the grouping pass.
type candidate struct {
	path   string
	parsed EpisodeName
	folder string
}

// scanShows rebuilds a TV library from disk.
func (s *Scanner) scanShows(ctx context.Context, lib models.Library, prog *Progress) error {
	files, err := walkFiles(ctx, lib.Paths, acceptVideo)
	if err != nil {
		return err
	}

	// Index existing episodes by file path so unchanged files can skip probing.
	type located struct {
		show    models.Show
		episode models.Episode
	}
	existing := make(map[string]located)
	for _, show := range s.db.Shows.All() {
		if show.LibraryID != lib.ID {
			continue
		}
		for _, season := range show.Seasons {
			for _, ep := range season.Episodes {
				existing[normalizePathKey(ep.Media.Path)] = located{show: show, episode: ep}
			}
		}
	}

	var (
		candidates []candidate
		toProbe    []string
		probeIdx   = make(map[string]int)
	)

	for _, path := range files {
		parsed, ok := ParseEpisodeName(path)
		if !ok {
			log.Printf("scanner: %s does not look like an episode, skipping", filepath.Base(path))
			continue
		}

		c := candidate{path: path, parsed: parsed, folder: showFolderOf(path)}
		candidates = append(candidates, c)

		stat, err := os.Stat(path)
		if err != nil {
			continue
		}
		if prev, ok := existing[normalizePathKey(path)]; ok && prev.episode.Media.Unchanged(stat.Size(), stat.ModTime()) {
			continue // unchanged: reuse the stored MediaInfo
		}
		probeIdx[normalizePathKey(path)] = len(toProbe)
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

	// Group episodes into shows keyed by folder.
	now := time.Now()
	shows := make(map[string]*models.Show)
	seenEpisodes := make(map[string]bool)

	for _, c := range candidates {
		showID := StableID("sh", c.folder)
		show, ok := shows[showID]
		if !ok {
			show = s.loadOrCreateShow(showID, lib.ID, c, now)
			shows[showID] = show
		}

		var info models.MediaInfo
		if idx, needsProbe := probeIdx[normalizePathKey(c.path)]; needsProbe {
			info = infos[idx]
			if info.Path == "" {
				info.Path = c.path
			}
		} else {
			info = existing[normalizePathKey(c.path)].episode.Media
			info.Available = true
		}

		epID := StableID("ep", c.path)
		seenEpisodes[epID] = true

		episode := models.Episode{
			ID:      epID,
			ShowID:  showID,
			Season:  c.parsed.Season,
			Episode: c.parsed.Episode,
			Title:   c.parsed.Title,
			Media:   info,
			AddedAt: now,
		}
		// Preserve provider metadata and the original AddedAt across rescans.
		if prev, ok := existing[normalizePathKey(c.path)]; ok {
			episode.AddedAt = prev.episode.AddedAt
			if prev.episode.Overview != "" {
				episode.Overview = prev.episode.Overview
				episode.AirDate = prev.episode.AirDate
				episode.Rating = prev.episode.Rating
				episode.StillID = prev.episode.StillID
			}
			if episode.Title == "" {
				episode.Title = prev.episode.Title
			}
			prog.Updated++
		} else {
			prog.Added++
		}
		episode.UpdatedAt = now

		addEpisode(show, episode)

		// A multi-episode file covers several numbers; register the extras so the
		// episode list has no visible gaps.
		for _, extra := range c.parsed.Extra {
			dup := episode
			dup.ID = StableID("ep", c.path+"#"+strconv.Itoa(extra))
			dup.Episode = extra
			seenEpisodes[dup.ID] = true
			addEpisode(show, dup)
		}
	}

	// Persist, marking vanished episodes unavailable.
	prog.Phase = PhaseCleaning
	s.publish(prog)

	for _, show := range shows {
		show.SortSeasons()
		show.UpdatedAt = now
		if err := s.db.Shows.Put(*show); err != nil {
			return err
		}
	}

	for _, show := range s.db.Shows.All() {
		if show.LibraryID != lib.ID {
			continue
		}
		if _, stillPresent := shows[show.ID]; stillPresent {
			// Mark episodes of a surviving show that were not seen this pass.
			_, _ = s.db.Shows.Update(show.ID, func(sh *models.Show) {
				for si := range sh.Seasons {
					for ei := range sh.Seasons[si].Episodes {
						ep := &sh.Seasons[si].Episodes[ei]
						if !seenEpisodes[ep.ID] && ep.Media.Available {
							ep.Media.Available = false
							prog.Removed++
						}
					}
				}
			})
			continue
		}
		// The whole show folder is gone.
		_, _ = s.db.Shows.Update(show.ID, func(sh *models.Show) {
			for si := range sh.Seasons {
				for ei := range sh.Seasons[si].Episodes {
					sh.Seasons[si].Episodes[ei].Media.Available = false
				}
			}
		})
		prog.Removed++
	}

	s.updateLibraryCount(lib.ID, s.db.Shows.Filter(func(sh models.Show) bool {
		return sh.LibraryID == lib.ID
	}))

	return s.enrichShows(ctx, lib, prog)
}

// loadOrCreateShow returns the stored show for this folder, or a new one seeded from
// the folder name.
func (s *Scanner) loadOrCreateShow(showID, libraryID string, c candidate, now time.Time) *models.Show {
	if stored, err := s.db.Shows.Get(showID); err == nil {
		// Start from the stored record but drop the episode lists: they are rebuilt
		// from disk, and keeping stale ones would resurrect deleted files.
		fresh := stored
		fresh.Seasons = nil
		for _, season := range stored.Seasons {
			fresh.Seasons = append(fresh.Seasons, models.Season{
				Number:   season.Number,
				Name:     season.Name,
				Overview: season.Overview,
				PosterID: season.PosterID,
			})
		}
		return &fresh
	}

	folderName := filepath.Base(c.folder)
	title := c.parsed.Show
	if title == "" {
		title = cleanTitle(normalizeSeparators(folderName))
	}

	return &models.Show{
		ID:         showID,
		LibraryID:  libraryID,
		Title:      title,
		SortTitle:  SortTitle(title),
		Year:       ShowYear(folderName),
		FolderPath: c.folder,
		MetaStatus: models.MetaPending,
		AddedAt:    now,
	}
}

// addEpisode inserts an episode into the right season, replacing any existing entry
// with the same season and episode number.
func addEpisode(show *models.Show, ep models.Episode) {
	for si := range show.Seasons {
		if show.Seasons[si].Number != ep.Season {
			continue
		}
		for ei := range show.Seasons[si].Episodes {
			if show.Seasons[si].Episodes[ei].Episode == ep.Episode {
				show.Seasons[si].Episodes[ei] = ep
				return
			}
		}
		show.Seasons[si].Episodes = append(show.Seasons[si].Episodes, ep)
		return
	}
	show.Seasons = append(show.Seasons, models.Season{
		Number:   ep.Season,
		Episodes: []models.Episode{ep},
	})
}

// enrichShows fills in provider metadata for shows that still need it.
func (s *Scanner) enrichShows(ctx context.Context, lib models.Library, prog *Progress) error {
	meta := s.metadataService()
	if meta == nil || !meta.Enabled() {
		for _, sh := range s.db.Shows.Filter(func(sh models.Show) bool {
			return sh.LibraryID == lib.ID && sh.MetaStatus == models.MetaPending
		}) {
			_, _ = s.db.Shows.Update(sh.ID, func(x *models.Show) { x.MetaStatus = models.MetaLocal })
		}
		return nil
	}

	pending := s.db.Shows.Filter(func(sh models.Show) bool {
		return sh.LibraryID == lib.ID && !sh.MatchLocked &&
			(sh.MetaStatus == models.MetaPending || sh.MetaStatus == models.MetaLocal)
	})
	if len(pending) == 0 {
		return nil
	}

	prog.Phase = PhaseMetadata
	prog.Total = len(pending)
	prog.Done = 0
	s.publish(prog)

	for _, show := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}

		prog.Current = show.Title
		enriched := show
		if err := meta.EnrichShow(ctx, &enriched); err != nil {
			log.Printf("scanner: metadata for %q: %v", show.Title, err)
			enriched.MetaStatus = models.MetaError
		}
		_ = s.db.Shows.Put(enriched)

		prog.Done++
		s.publish(prog)
	}

	return nil
}

