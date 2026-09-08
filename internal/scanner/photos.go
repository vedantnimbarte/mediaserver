package scanner

import (
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"

	"kino/internal/models"
)

// scanPhotos rebuilds a photo library. Albums are folders: that is how people
// actually organize photos, and it needs no configuration.
func (s *Scanner) scanPhotos(ctx context.Context, lib models.Library, prog *Progress) error {
	files, err := walkFiles(ctx, lib.Paths, func(p string) bool { return IsImageFile(p) })
	if err != nil {
		return err
	}

	existing := make(map[string]models.Photo)
	for _, album := range s.db.PhotoAlbums.All() {
		if album.LibraryID != lib.ID {
			continue
		}
		for _, p := range album.Photos {
			existing[normalizePathKey(p.Path)] = p
		}
	}

	prog.Phase = PhaseProbing
	prog.Total = len(files)
	prog.Done = 0
	s.publish(prog)

	albums := make(map[string]*models.PhotoAlbum)
	seen := make(map[string]bool)
	now := time.Now()

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return err
		}

		prog.Done++
		if prog.Done%25 == 0 || prog.Done == prog.Total {
			prog.Current = filepath.Base(path)
			s.publish(prog)
		}

		stat, err := os.Stat(path)
		if err != nil {
			continue
		}

		folder := filepath.Dir(path)
		albumID := StableID("pa", folder)

		album, ok := albums[albumID]
		if !ok {
			album = s.loadOrCreateAlbum(albumID, lib.ID, folder, now)
			albums[albumID] = album
		}

		photoID := StableID("ph", path)
		seen[photoID] = true

		if prev, known := existing[normalizePathKey(path)]; known && prev.Unchanged(stat.Size(), stat.ModTime()) {
			prev.Available = true
			prev.AlbumID = albumID
			album.Photos = append(album.Photos, prev)
			prog.Updated++
			continue
		}

		photo := readPhoto(path, stat)
		photo.ID = photoID
		photo.AlbumID = albumID
		album.Photos = append(album.Photos, photo)
		prog.Added++
	}

	prog.Phase = PhaseCleaning
	s.publish(prog)

	for _, album := range albums {
		sortPhotos(album)
		if album.CoverID == "" && len(album.Photos) > 0 {
			album.CoverID = album.Photos[0].ID
		}
		album.UpdatedAt = now
		if err := s.db.PhotoAlbums.Put(*album); err != nil {
			return err
		}
	}

	// An album whose folder is gone disappears entirely; there is no watch history to
	// preserve for photos, so keeping empty shells would only clutter the UI.
	for _, album := range s.db.PhotoAlbums.All() {
		if album.LibraryID != lib.ID {
			continue
		}
		if _, alive := albums[album.ID]; !alive {
			s.db.PhotoAlbums.Delete(album.ID)
			prog.Removed++
			continue
		}
		_ = seen
	}

	s.updateLibraryCount(lib.ID, s.db.PhotoAlbums.Filter(func(a models.PhotoAlbum) bool {
		return a.LibraryID == lib.ID
	}))

	return nil
}

func (s *Scanner) loadOrCreateAlbum(id, libraryID, folder string, now time.Time) *models.PhotoAlbum {
	name := cleanTitle(normalizeSeparators(filepath.Base(folder)))
	if name == "" {
		name = filepath.Base(folder)
	}

	if stored, err := s.db.PhotoAlbums.Get(id); err == nil {
		fresh := stored
		fresh.Photos = nil // rebuilt from disk
		fresh.Name = name
		return &fresh
	}

	return &models.PhotoAlbum{
		ID:         id,
		LibraryID:  libraryID,
		Name:       name,
		FolderPath: folder,
		AddedAt:    now,
	}
}

// readPhoto extracts dimensions and EXIF metadata.
//
// Dimensions come from the image header alone, which is a few hundred bytes rather
// than the whole file: decoding every photo fully would make a large library scan
// take minutes instead of seconds.
func readPhoto(path string, stat os.FileInfo) models.Photo {
	photo := models.Photo{
		Path:      path,
		Filename:  filepath.Base(path),
		Size:      stat.Size(),
		ModTime:   stat.ModTime(),
		Available: true,
		TakenAt:   stat.ModTime(), // replaced by EXIF below when present
	}

	f, err := os.Open(path)
	if err != nil {
		return photo
	}
	defer f.Close()

	if cfg, _, err := image.DecodeConfig(f); err == nil {
		photo.Width = cfg.Width
		photo.Height = cfg.Height
	}

	if _, err := f.Seek(0, 0); err != nil {
		return photo
	}

	x, err := exif.Decode(f)
	if err != nil {
		return photo // no EXIF is perfectly normal, especially for PNG and screenshots
	}

	if tm, err := x.DateTime(); err == nil && !tm.IsZero() {
		photo.TakenAt = tm
	}
	if orientation, err := x.Get(exif.Orientation); err == nil {
		if v, err := orientation.Int(0); err == nil {
			photo.Orientation = v
		}
	}

	make := exifString(x, exif.Make)
	model := exifString(x, exif.Model)
	photo.Camera = strings.TrimSpace(strings.TrimPrefix(model, make))
	if photo.Camera == "" {
		photo.Camera = strings.TrimSpace(make)
	} else if make != "" {
		photo.Camera = strings.TrimSpace(make + " " + photo.Camera)
	}

	photo.Lens = exifString(x, exif.LensModel)
	if iso, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if v, err := iso.Int(0); err == nil {
			photo.ISO = v
		}
	}
	photo.Aperture = exifRational(x, exif.FNumber, "f/")
	photo.Shutter = exifShutter(x)
	photo.FocalLen = exifRational(x, exif.FocalLength, "")

	if lat, lon, err := x.LatLong(); err == nil {
		photo.Lat, photo.Lon = lat, lon
	}

	return photo
}

func exifString(x *exif.Exif, name exif.FieldName) string {
	tagValue, err := x.Get(name)
	if err != nil {
		return ""
	}
	s, err := tagValue.StringVal()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Trim(s, "\x00"))
}

// exifRational renders a rational EXIF value such as an aperture or focal length.
func exifRational(x *exif.Exif, name exif.FieldName, prefix string) string {
	tagValue, err := x.Get(name)
	if err != nil {
		return ""
	}
	num, den, err := tagValue.Rat2(0)
	if err != nil || den == 0 {
		return ""
	}
	value := float64(num) / float64(den)
	return prefix + trimFloat(value)
}

// exifShutter renders the exposure time the way a camera would: "1/250" or "2.5s".
func exifShutter(x *exif.Exif) string {
	tagValue, err := x.Get(exif.ExposureTime)
	if err != nil {
		return ""
	}
	num, den, err := tagValue.Rat2(0)
	if err != nil || den == 0 {
		return ""
	}
	if num == 1 {
		return "1/" + itoa64(den)
	}
	seconds := float64(num) / float64(den)
	if seconds < 1 {
		return "1/" + trimFloat(1/seconds)
	}
	return trimFloat(seconds) + "s"
}

func trimFloat(v float64) string {
	s := strconvFormat(v)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// sortPhotos orders an album chronologically, which is how people expect to browse.
func sortPhotos(album *models.PhotoAlbum) {
	photos := album.Photos
	for i := 1; i < len(photos); i++ {
		for j := i; j > 0 && photos[j].TakenAt.Before(photos[j-1].TakenAt); j-- {
			photos[j], photos[j-1] = photos[j-1], photos[j]
		}
	}
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [24]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func strconvFormat(v float64) string {
	// Two decimal places is plenty for apertures and focal lengths.
	scaled := int64(v*100 + 0.5)
	whole := scaled / 100
	frac := scaled % 100
	if frac < 0 {
		frac = -frac
	}
	return itoa64(whole) + "." + pad2i(frac)
}

func pad2i(n int64) string {
	if n < 10 {
		return "0" + itoa64(n)
	}
	return itoa64(n)
}
