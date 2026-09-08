package scanner

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dhowden/tag"

	"kino/internal/models"
)

// scanMusic rebuilds a music library from disk.
//
// Unlike video, the authoritative metadata lives inside the files themselves, so tags
// are read first and the folder layout is only a fallback for untagged rips.
func (s *Scanner) scanMusic(ctx context.Context, lib models.Library, prog *Progress) error {
	files, err := walkFiles(ctx, lib.Paths, func(p string) bool { return IsAudioFile(p) })
	if err != nil {
		return err
	}

	// Index existing tracks so unchanged files can skip both tag reading and ffprobe.
	existing := make(map[string]models.Track)
	for _, artist := range s.db.Artists.All() {
		if artist.LibraryID != lib.ID {
			continue
		}
		for _, album := range artist.Albums {
			for _, tr := range album.Tracks {
				existing[normalizePathKey(tr.Media.Path)] = tr
			}
		}
	}

	prog.Phase = PhaseProbing
	prog.Total = len(files)
	prog.Done = 0
	s.publish(prog)

	// artists is keyed by a normalized artist name so that inconsistent casing across
	// albums does not split one artist into several.
	artists := make(map[string]*models.Artist)
	seenTracks := make(map[string]bool)
	now := time.Now()

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}

		prog.Done++
		prog.Current = filepath.Base(path)
		if prog.Done%10 == 0 || prog.Done == prog.Total {
			s.publish(prog)
		}

		stat, err := os.Stat(path)
		if err != nil {
			continue
		}

		trackID := StableID("tr", path)
		seenTracks[trackID] = true

		prev, wasKnown := existing[normalizePathKey(path)]
		var meta trackMetadata

		if wasKnown && prev.Media.Unchanged(stat.Size(), stat.ModTime()) {
			meta = metadataFromTrack(prev)
			meta.media = prev.Media
			meta.media.Available = true
			prog.Updated++
		} else {
			meta = s.readTrackMetadata(ctx, path, stat)
			if wasKnown {
				prog.Updated++
			} else {
				prog.Added++
			}
		}

		artistKey := strings.ToLower(meta.albumArtist)
		artist, ok := artists[artistKey]
		if !ok {
			artist = s.loadOrCreateArtist(lib.ID, meta.albumArtist, now)
			artists[artistKey] = artist
		}

		track := models.Track{
			ID:          trackID,
			ArtistID:    artist.ID,
			Title:       meta.title,
			Artist:      meta.trackArtist,
			TrackNo:     meta.trackNo,
			DiscNo:      meta.discNo,
			Year:        meta.year,
			Genre:       meta.genre,
			DurationSec: meta.media.DurationSec,
			Media:       meta.media,
			AddedAt:     now,
		}
		if wasKnown {
			track.AddedAt = prev.AddedAt
		}

		addTrack(artist, meta, track, now)
	}

	prog.Phase = PhaseCleaning
	s.publish(prog)

	for _, artist := range artists {
		artist.SortDiscography()
		artist.UpdatedAt = now
		if err := s.db.Artists.Put(*artist); err != nil {
			return err
		}
	}

	// Mark vanished tracks unavailable, and drop artists that have nothing left.
	for _, artist := range s.db.Artists.All() {
		if artist.LibraryID != lib.ID {
			continue
		}
		remaining := 0
		_, _ = s.db.Artists.Update(artist.ID, func(a *models.Artist) {
			for ai := range a.Albums {
				for ti := range a.Albums[ai].Tracks {
					tr := &a.Albums[ai].Tracks[ti]
					if seenTracks[tr.ID] {
						remaining++
						continue
					}
					if tr.Media.Available {
						tr.Media.Available = false
						prog.Removed++
					}
				}
			}
		})
		if remaining == 0 {
			s.db.Artists.Delete(artist.ID)
		}
	}

	s.updateLibraryCount(lib.ID, s.db.Artists.Filter(func(a models.Artist) bool {
		return a.LibraryID == lib.ID
	}))

	return nil
}

// trackMetadata is everything we worked out about one audio file.
type trackMetadata struct {
	title       string
	trackArtist string
	albumArtist string
	album       string
	year        int
	genre       string
	trackNo     int
	discNo      int
	coverID     string
	folder      string
	media       models.MediaInfo
}

func metadataFromTrack(tr models.Track) trackMetadata {
	return trackMetadata{
		title:       tr.Title,
		trackArtist: tr.Artist,
		year:        tr.Year,
		genre:       tr.Genre,
		trackNo:     tr.TrackNo,
		discNo:      tr.DiscNo,
		folder:      filepath.Dir(tr.Media.Path),
	}
}

// readTrackMetadata reads embedded tags, falling back to the folder layout.
func (s *Scanner) readTrackMetadata(ctx context.Context, path string, stat os.FileInfo) trackMetadata {
	meta := trackMetadata{folder: filepath.Dir(path)}

	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		if tags, err := tag.ReadFrom(f); err == nil {
			meta.title = strings.TrimSpace(tags.Title())
			meta.trackArtist = strings.TrimSpace(tags.Artist())
			meta.albumArtist = strings.TrimSpace(tags.AlbumArtist())
			meta.album = strings.TrimSpace(tags.Album())
			meta.year = tags.Year()
			meta.genre = strings.TrimSpace(tags.Genre())
			meta.trackNo, _ = tags.Track()
			meta.discNo, _ = tags.Disc()

			// Embedded cover art is stored once, keyed by content, so an album's
			// twelve tracks share a single cached image.
			if pic := tags.Picture(); pic != nil && len(pic.Data) > 0 && s.images != nil {
				if key, err := s.images.Store(pic.Data, path); err == nil {
					meta.coverID = key
				}
			}
		}
	}

	// Fall back to the conventional Artist/Album/NN Title layout for untagged files.
	fillMusicGapsFromPath(&meta, path)

	// ffprobe gives the duration, which no tag reliably provides.
	if s.tools != nil {
		if info, err := s.tools.Probe(ctx, path); err == nil {
			meta.media = info
		} else {
			log.Printf("scanner: probe %s: %v", filepath.Base(path), err)
		}
	}
	if meta.media.Path == "" {
		meta.media = models.MediaInfo{Path: path}
	}
	meta.media.Size = stat.Size()
	meta.media.ModTime = stat.ModTime()
	meta.media.Available = true

	return meta
}

// fillMusicGapsFromPath uses the folder structure for anything the tags did not say.
func fillMusicGapsFromPath(meta *trackMetadata, path string) {
	if meta.title == "" {
		title := stemOf(path)
		// Strip a leading track number such as "03 - " or "03.".
		title = strings.TrimSpace(trimLeadingTrackNumber(title, meta))
		meta.title = cleanTitle(normalizeSeparators(title))
	}

	albumDir := filepath.Dir(path)
	if meta.album == "" {
		meta.album = cleanTitle(normalizeSeparators(filepath.Base(albumDir)))
	}
	if meta.albumArtist == "" {
		meta.albumArtist = meta.trackArtist
	}
	if meta.albumArtist == "" {
		// The grandparent folder is conventionally the artist.
		parent := filepath.Base(filepath.Dir(albumDir))
		if parent != "" && parent != "." && parent != string(filepath.Separator) {
			meta.albumArtist = cleanTitle(normalizeSeparators(parent))
		}
	}
	if meta.albumArtist == "" {
		meta.albumArtist = "Unknown Artist"
	}
	if meta.album == "" {
		meta.album = "Unknown Album"
	}
	if meta.title == "" {
		meta.title = stemOf(path)
	}
}

// trimLeadingTrackNumber removes a "03 - " style prefix and records the number.
func trimLeadingTrackNumber(name string, meta *trackMetadata) string {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 || i > 3 {
		return name
	}

	rest := strings.TrimLeft(name[i:], " .-_")
	if rest == "" || rest == name {
		return name
	}

	if meta.trackNo == 0 {
		n := 0
		for _, c := range name[:i] {
			n = n*10 + int(c-'0')
		}
		meta.trackNo = n
	}
	return rest
}

func (s *Scanner) loadOrCreateArtist(libraryID, name string, now time.Time) *models.Artist {
	id := StableID("ar", libraryID+"|"+strings.ToLower(name))

	if stored, err := s.db.Artists.Get(id); err == nil {
		// Keep provider metadata but rebuild the album list from disk.
		fresh := stored
		fresh.Albums = nil
		return &fresh
	}

	return &models.Artist{
		ID:        id,
		LibraryID: libraryID,
		Name:      name,
		SortName:  SortTitle(name),
		AddedAt:   now,
	}
}

// addTrack files a track under the right album, creating the album if needed.
func addTrack(artist *models.Artist, meta trackMetadata, track models.Track, now time.Time) {
	albumID := StableID("al", artist.ID+"|"+strings.ToLower(meta.album))
	track.AlbumID = albumID

	for i := range artist.Albums {
		if artist.Albums[i].ID != albumID {
			continue
		}
		album := &artist.Albums[i]
		if album.CoverID == "" {
			album.CoverID = meta.coverID
		}
		if album.Year == 0 {
			album.Year = meta.year
		}
		album.Tracks = append(album.Tracks, track)
		return
	}

	artist.Albums = append(artist.Albums, models.Album{
		ID:          albumID,
		ArtistID:    artist.ID,
		Title:       meta.album,
		AlbumArtist: artist.Name,
		Year:        meta.year,
		Genre:       meta.genre,
		CoverID:     meta.coverID,
		FolderPath:  meta.folder,
		Tracks:      []models.Track{track},
		AddedAt:     now,
	})
}

// sortArtistsByName is used by the API when listing a music library.
func sortArtistsByName(artists []models.Artist) {
	sort.Slice(artists, func(i, j int) bool {
		return strings.ToLower(artists[i].SortName) < strings.ToLower(artists[j].SortName)
	})
}
