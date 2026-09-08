package scanner

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"kino/internal/ffmpeg"
	"kino/internal/models"
)

// makeTaggedAudio writes a real audio file carrying ID3 tags.
func makeTaggedAudio(t *testing.T, tools *ffmpeg.Tools, path, title, artist, album string, track int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tools.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:a", "libmp3lame", "-b:a", "64k",
		"-metadata", "title="+title,
		"-metadata", "artist="+artist,
		"-metadata", "album="+album,
		"-metadata", "album_artist="+artist,
		"-metadata", "track="+itoa(track),
		"-metadata", "date=2021",
		"-metadata", "genre=Electronic",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate audio %s: %v\n%s", path, err, out)
	}
}

func TestScanMusicLibrary(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	makeTaggedAudio(t, tools, filepath.Join(root, "Aphex Twin", "Selected Ambient Works", "01 Xtal.mp3"),
		"Xtal", "Aphex Twin", "Selected Ambient Works", 1)
	makeTaggedAudio(t, tools, filepath.Join(root, "Aphex Twin", "Selected Ambient Works", "02 Tha.mp3"),
		"Tha", "Aphex Twin", "Selected Ambient Works", 2)
	makeTaggedAudio(t, tools, filepath.Join(root, "Boards of Canada", "Music Has the Right", "01 Wildlife.mp3"),
		"Wildlife Analysis", "Boards of Canada", "Music Has the Right to Children", 1)

	lib := addLibrary(t, db, "Music", models.LibraryMusic, root)
	prog := runScan(t, s, lib.ID)

	if prog.Phase != PhaseDone {
		t.Fatalf("scan phase = %s, error = %s", prog.Phase, prog.Error)
	}
	if db.Artists.Len() != 2 {
		var names []string
		for _, a := range db.Artists.All() {
			names = append(names, a.Name)
		}
		t.Fatalf("found %d artists %v, want 2", db.Artists.Len(), names)
	}

	byName := map[string]models.Artist{}
	for _, a := range db.Artists.All() {
		byName[a.Name] = a
	}

	aphex, ok := byName["Aphex Twin"]
	if !ok {
		t.Fatalf("Aphex Twin not found")
	}
	if len(aphex.Albums) != 1 {
		t.Fatalf("Aphex Twin has %d albums, want 1", len(aphex.Albums))
	}

	album := aphex.Albums[0]
	if album.Title != "Selected Ambient Works" {
		t.Errorf("album title = %q", album.Title)
	}
	if len(album.Tracks) != 2 {
		t.Fatalf("album has %d tracks, want 2", len(album.Tracks))
	}

	// Tracks must come back in track-number order, not filesystem order.
	if album.Tracks[0].TrackNo != 1 || album.Tracks[0].Title != "Xtal" {
		t.Errorf("first track = %d %q, want 1 \"Xtal\"", album.Tracks[0].TrackNo, album.Tracks[0].Title)
	}
	if album.Tracks[1].Title != "Tha" {
		t.Errorf("second track = %q, want \"Tha\"", album.Tracks[1].Title)
	}
	if album.Tracks[0].DurationSec < 1 || album.Tracks[0].DurationSec > 4 {
		t.Errorf("duration = %.2f, want roughly 2s", album.Tracks[0].DurationSec)
	}
	if album.Year != 2021 {
		t.Errorf("album year = %d, want 2021", album.Year)
	}

	// A track must be resolvable for playback like any other media.
	playable, err := db.ResolvePlayable(album.Tracks[0].ID)
	if err != nil {
		t.Fatalf("resolve track: %v", err)
	}
	if playable.Kind != models.PlayableTrack {
		t.Errorf("kind = %s, want track", playable.Kind)
	}
}

// TestScanMusicUntaggedFallsBackToPaths covers rips with no tags at all, where the
// folder layout is the only information available.
func TestScanMusicUntaggedFallsBackToPaths(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	path := filepath.Join(root, "Some Artist", "Some Album", "03 Third Song.mp3")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tools.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:a", "libmp3lame", "-b:a", "64k",
		"-map_metadata", "-1", // strip every tag
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate: %v\n%s", err, out)
	}

	lib := addLibrary(t, db, "Music", models.LibraryMusic, root)
	runScan(t, s, lib.ID)

	if db.Artists.Len() != 1 {
		t.Fatalf("found %d artists, want 1", db.Artists.Len())
	}
	artist := db.Artists.All()[0]
	if artist.Name != "Some Artist" {
		t.Errorf("artist = %q, want \"Some Artist\" from the folder", artist.Name)
	}
	if len(artist.Albums) != 1 || artist.Albums[0].Title != "Some Album" {
		t.Fatalf("album not derived from the folder: %+v", artist.Albums)
	}
	track := artist.Albums[0].Tracks[0]
	if track.TrackNo != 3 {
		t.Errorf("track number = %d, want 3 from the filename prefix", track.TrackNo)
	}
	if track.Title != "Third Song" {
		t.Errorf("title = %q, want \"Third Song\"", track.Title)
	}
}

// makeJPEG writes a real JPEG of the given size.
func makeJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
}

func TestScanPhotoLibrary(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	makeJPEG(t, filepath.Join(root, "Holiday 2023", "beach.jpg"), 800, 600)
	makeJPEG(t, filepath.Join(root, "Holiday 2023", "sunset.jpg"), 600, 800)
	makeJPEG(t, filepath.Join(root, "Family", "portrait.jpg"), 400, 400)

	lib := addLibrary(t, db, "Photos", models.LibraryPhoto, root)
	prog := runScan(t, s, lib.ID)

	if prog.Phase != PhaseDone {
		t.Fatalf("scan phase = %s, error = %s", prog.Phase, prog.Error)
	}
	if db.PhotoAlbums.Len() != 2 {
		t.Fatalf("found %d albums, want 2", db.PhotoAlbums.Len())
	}

	byName := map[string]models.PhotoAlbum{}
	for _, a := range db.PhotoAlbums.All() {
		byName[a.Name] = a
	}

	holiday, ok := byName["Holiday 2023"]
	if !ok {
		t.Fatalf("Holiday 2023 album not found; got %v", albumNames(byName))
	}
	if len(holiday.Photos) != 2 {
		t.Fatalf("Holiday album has %d photos, want 2", len(holiday.Photos))
	}
	if holiday.CoverID == "" {
		t.Error("album has no cover photo")
	}

	// Dimensions must be read so the masonry grid can lay out before images load.
	var landscape, portrait bool
	for _, p := range holiday.Photos {
		if p.Width == 0 || p.Height == 0 {
			t.Errorf("%s has no dimensions", p.Filename)
		}
		if p.Width == 800 && p.Height == 600 {
			landscape = true
			if ar := p.AspectRatio(); ar < 1.3 || ar > 1.4 {
				t.Errorf("aspect ratio = %.2f, want about 1.33", ar)
			}
		}
		if p.Width == 600 && p.Height == 800 {
			portrait = true
		}
		if !p.Available {
			t.Errorf("%s should be available", p.Filename)
		}
	}
	if !landscape || !portrait {
		t.Errorf("expected both orientations to be recorded (landscape=%v portrait=%v)", landscape, portrait)
	}
}

func TestPhotoRescanIsIncremental(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	makeJPEG(t, filepath.Join(root, "Album", "one.jpg"), 400, 300)
	makeJPEG(t, filepath.Join(root, "Album", "two.jpg"), 400, 300)

	lib := addLibrary(t, db, "Photos", models.LibraryPhoto, root)

	first := runScan(t, s, lib.ID)
	if first.Added != 2 {
		t.Fatalf("first scan added %d, want 2", first.Added)
	}

	second := runScan(t, s, lib.ID)
	if second.Added != 0 {
		t.Errorf("rescan added %d, want 0", second.Added)
	}
	if second.Updated != 2 {
		t.Errorf("rescan reused %d photos, want 2", second.Updated)
	}
	if db.PhotoAlbums.Len() != 1 {
		t.Errorf("album count = %d, want 1", db.PhotoAlbums.Len())
	}
}

func albumNames(m map[string]models.PhotoAlbum) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
