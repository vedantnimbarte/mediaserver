package scanner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"kino/internal/ffmpeg"
	"kino/internal/images"
	"kino/internal/models"
	"kino/internal/store"
)

// testTools locates ffmpeg, skipping the test when it is not installed.
func testTools(t *testing.T) *ffmpeg.Tools {
	t.Helper()
	tools, err := ffmpeg.Locate("", "")
	if err != nil {
		t.Skipf("ffmpeg is not installed: %v", err)
	}
	return tools
}

// makeVideo synthesizes a tiny real video file so the scanner and ffprobe have
// something genuine to work on rather than a stubbed-out fixture.
func makeVideo(t *testing.T, tools *ffmpeg.Tools, path string, seconds int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(tools.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration="+itoa(seconds)+":size=160x120:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+itoa(seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate %s: %v\n%s", path, err, out)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func newTestScanner(t *testing.T, tools *ffmpeg.Tools) (*Scanner, *store.DB) {
	t.Helper()
	db, err := store.OpenDB(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db, tools, nil, images.New(filepath.Join(t.TempDir(), "images"))), db
}

// runScan starts a scan and blocks until it finishes.
func runScan(t *testing.T, s *Scanner, libraryID string) Progress {
	t.Helper()

	events, unsubscribe := s.Hub().Subscribe()
	defer unsubscribe()

	if err := s.Scan(context.Background(), libraryID); err != nil {
		t.Fatalf("start scan: %v", err)
	}

	deadline := time.After(120 * time.Second)
	for {
		select {
		case p := <-events:
			if p.LibraryID == libraryID && p.Finished {
				return p
			}
		case <-deadline:
			t.Fatal("scan did not finish within the timeout")
		}
	}
}

func addLibrary(t *testing.T, db *store.DB, name string, typ models.LibraryType, path string) models.Library {
	t.Helper()
	lib := models.Library{
		ID:        "lib-" + name,
		Name:      name,
		Type:      typ,
		Paths:     []string{path},
		CreatedAt: time.Now(),
	}
	if err := db.Libraries.Put(lib); err != nil {
		t.Fatal(err)
	}
	return lib
}

func TestScanMovieLibrary(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	makeVideo(t, tools, filepath.Join(root, "The Matrix (1999)", "The Matrix (1999).mp4"), 2)
	makeVideo(t, tools, filepath.Join(root, "Inception.2010.1080p.BluRay.x264.mp4"), 2)
	// A sample file that must be ignored rather than becoming a library entry.
	makeVideo(t, tools, filepath.Join(root, "Inception.2010.sample.mp4"), 1)

	lib := addLibrary(t, db, "Films", models.LibraryMovie, root)
	prog := runScan(t, s, lib.ID)

	if prog.Phase != PhaseDone {
		t.Fatalf("scan phase = %s, error = %s", prog.Phase, prog.Error)
	}
	if db.Movies.Len() != 2 {
		for _, m := range db.Movies.All() {
			t.Logf("found: %q (%d) from %s", m.Title, m.Year, m.Media.Path)
		}
		t.Fatalf("found %d movies, want 2 (the sample file should be skipped)", db.Movies.Len())
	}

	byTitle := map[string]models.Movie{}
	for _, m := range db.Movies.All() {
		byTitle[m.Title] = m
	}

	matrix, ok := byTitle["The Matrix"]
	if !ok {
		t.Fatalf("The Matrix was not found; got %v", keysOf(byTitle))
	}
	if matrix.Year != 1999 {
		t.Errorf("year = %d, want 1999", matrix.Year)
	}
	if !matrix.Media.Probed {
		t.Errorf("media was not probed: %s", matrix.Media.ProbeError)
	}
	if matrix.Media.DurationSec < 1 || matrix.Media.DurationSec > 5 {
		t.Errorf("duration = %.2f, want roughly 2s", matrix.Media.DurationSec)
	}
	if v := matrix.Media.VideoStream(); v == nil {
		t.Error("no video stream detected")
	} else if v.Width != 160 || v.Height != 120 {
		t.Errorf("resolution = %dx%d, want 160x120", v.Width, v.Height)
	}
	if len(matrix.Media.AudioStreams()) != 1 {
		t.Errorf("found %d audio streams, want 1", len(matrix.Media.AudioStreams()))
	}
	if !matrix.Media.Available {
		t.Error("media should be marked available")
	}
	if _, ok := byTitle["Inception"]; !ok {
		t.Errorf("Inception was not found; got %v", keysOf(byTitle))
	}
}

// TestRescanIsIncremental is the property that keeps rescans fast: unchanged files
// must not be handed to ffprobe again.
func TestRescanIsIncremental(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	makeVideo(t, tools, filepath.Join(root, "Arrival (2016).mp4"), 2)
	makeVideo(t, tools, filepath.Join(root, "Dune (2021).mp4"), 2)

	lib := addLibrary(t, db, "Films", models.LibraryMovie, root)

	first := runScan(t, s, lib.ID)
	if first.Added != 2 {
		t.Fatalf("first scan added %d, want 2", first.Added)
	}

	idsBefore := map[string]bool{}
	for _, m := range db.Movies.All() {
		idsBefore[m.ID] = true
	}

	second := runScan(t, s, lib.ID)
	if second.Total != 0 {
		t.Errorf("rescan probed %d files, want 0 (nothing changed)", second.Total)
	}
	if second.Added != 0 {
		t.Errorf("rescan added %d items, want 0", second.Added)
	}
	if db.Movies.Len() != 2 {
		t.Errorf("rescan left %d movies, want 2", db.Movies.Len())
	}

	// IDs must be stable, otherwise watch history would be orphaned on every scan.
	for _, m := range db.Movies.All() {
		if !idsBefore[m.ID] {
			t.Errorf("movie ID changed across rescan: %s", m.ID)
		}
	}
}

// TestVanishedFileIsMarkedUnavailable covers the unplugged-drive case: the record
// must survive so that watch history is not silently destroyed.
func TestVanishedFileIsMarkedUnavailable(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	keep := filepath.Join(root, "Arrival (2016).mp4")
	remove := filepath.Join(root, "Dune (2021).mp4")
	makeVideo(t, tools, keep, 2)
	makeVideo(t, tools, remove, 2)

	lib := addLibrary(t, db, "Films", models.LibraryMovie, root)
	runScan(t, s, lib.ID)

	if err := os.Remove(remove); err != nil {
		t.Fatal(err)
	}

	prog := runScan(t, s, lib.ID)
	if prog.Removed != 1 {
		t.Errorf("removed = %d, want 1", prog.Removed)
	}
	if db.Movies.Len() != 2 {
		t.Errorf("record count = %d, want 2 (the record must be kept, not deleted)", db.Movies.Len())
	}

	for _, m := range db.Movies.All() {
		wantAvailable := m.Title == "Arrival"
		if m.Media.Available != wantAvailable {
			t.Errorf("%q available = %v, want %v", m.Title, m.Media.Available, wantAvailable)
		}
	}
}

func TestScanShowLibrary(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	makeVideo(t, tools, filepath.Join(root, "Breaking Bad", "Season 01", "Breaking Bad - S01E01 - Pilot.mp4"), 2)
	makeVideo(t, tools, filepath.Join(root, "Breaking Bad", "Season 01", "Breaking Bad - S01E02 - Cat in the Bag.mp4"), 2)
	makeVideo(t, tools, filepath.Join(root, "Breaking Bad", "Season 02", "Breaking Bad - S02E01 - Seven Thirty-Seven.mp4"), 2)
	makeVideo(t, tools, filepath.Join(root, "Firefly", "Firefly.S01E01.Serenity.mp4"), 2)

	lib := addLibrary(t, db, "Shows", models.LibraryShow, root)
	prog := runScan(t, s, lib.ID)

	if prog.Phase != PhaseDone {
		t.Fatalf("scan phase = %s, error = %s", prog.Phase, prog.Error)
	}
	if db.Shows.Len() != 2 {
		t.Fatalf("found %d shows, want 2", db.Shows.Len())
	}

	byTitle := map[string]models.Show{}
	for _, sh := range db.Shows.All() {
		byTitle[sh.Title] = sh
	}

	bb, ok := byTitle["Breaking Bad"]
	if !ok {
		t.Fatalf("Breaking Bad not found; got %v", showTitles(byTitle))
	}
	if len(bb.Seasons) != 2 {
		t.Fatalf("Breaking Bad has %d seasons, want 2", len(bb.Seasons))
	}
	if bb.EpisodeCount() != 3 {
		t.Errorf("Breaking Bad has %d episodes, want 3", bb.EpisodeCount())
	}

	// Seasons must be ordered, since the UI renders them in stored order.
	if bb.Seasons[0].Number != 1 || bb.Seasons[1].Number != 2 {
		t.Errorf("seasons out of order: %d then %d", bb.Seasons[0].Number, bb.Seasons[1].Number)
	}

	s1 := bb.Seasons[0]
	if len(s1.Episodes) != 2 {
		t.Fatalf("season 1 has %d episodes, want 2", len(s1.Episodes))
	}
	if s1.Episodes[0].Episode != 1 || s1.Episodes[0].Title != "Pilot" {
		t.Errorf("first episode = %d %q, want 1 \"Pilot\"", s1.Episodes[0].Episode, s1.Episodes[0].Title)
	}
	if !s1.Episodes[0].Media.Probed {
		t.Errorf("episode media was not probed: %s", s1.Episodes[0].Media.ProbeError)
	}
}

// TestNextEpisodeResolution exercises the ordering that drives autoplay.
func TestNextEpisodeResolution(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	for _, name := range []string{
		"Season 01/Show - S01E01.mp4",
		"Season 01/Show - S01E02.mp4",
		"Season 02/Show - S02E01.mp4",
	} {
		makeVideo(t, tools, filepath.Join(root, "Show", filepath.FromSlash(name)), 1)
	}

	lib := addLibrary(t, db, "Shows", models.LibraryShow, root)
	runScan(t, s, lib.ID)

	show, err := db.Shows.Get(StableID("sh", filepath.Join(root, "Show")))
	if err != nil {
		t.Fatalf("show not found: %v", err)
	}

	ordered := show.EpisodesInOrder()
	if len(ordered) != 3 {
		t.Fatalf("got %d episodes, want 3", len(ordered))
	}

	// S01E01 -> S01E02 -> S02E01 -> nothing
	next, ok := show.NextEpisode(ordered[0].ID)
	if !ok || next.Season != 1 || next.Episode != 2 {
		t.Errorf("after S01E01 got S%02dE%02d (ok=%v), want S01E02", next.Season, next.Episode, ok)
	}
	next, ok = show.NextEpisode(ordered[1].ID)
	if !ok || next.Season != 2 || next.Episode != 1 {
		t.Errorf("after S01E02 got S%02dE%02d (ok=%v), want S02E01", next.Season, next.Episode, ok)
	}
	if _, ok := show.NextEpisode(ordered[2].ID); ok {
		t.Error("the last episode should have no next episode")
	}
}

func TestResolvePlayableFindsMoviesAndEpisodes(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	movieRoot := t.TempDir()
	makeVideo(t, tools, filepath.Join(movieRoot, "Arrival (2016).mp4"), 1)
	movieLib := addLibrary(t, db, "Films", models.LibraryMovie, movieRoot)
	runScan(t, s, movieLib.ID)

	showRoot := t.TempDir()
	makeVideo(t, tools, filepath.Join(showRoot, "Show", "Season 01", "Show - S01E01.mp4"), 1)
	showLib := addLibrary(t, db, "Shows", models.LibraryShow, showRoot)
	runScan(t, s, showLib.ID)

	movie := db.Movies.All()[0]
	playable, err := db.ResolvePlayable(movie.ID)
	if err != nil {
		t.Fatalf("resolve movie: %v", err)
	}
	if playable.Kind != models.PlayableMovie || playable.Media.Path != movie.Media.Path {
		t.Errorf("resolved movie incorrectly: %+v", playable)
	}

	show := db.Shows.All()[0]
	ep := show.EpisodesInOrder()[0]
	playable, err = db.ResolvePlayable(ep.ID)
	if err != nil {
		t.Fatalf("resolve episode: %v", err)
	}
	if playable.Kind != models.PlayableEpisode || playable.ParentID != show.ID {
		t.Errorf("resolved episode incorrectly: %+v", playable)
	}

	if _, err := db.ResolvePlayable("nonexistent"); err == nil {
		t.Error("expected an error resolving an unknown ID")
	}
}

func TestScanRejectsConcurrentRuns(t *testing.T) {
	tools := testTools(t)
	s, db := newTestScanner(t, tools)

	root := t.TempDir()
	for i := 0; i < 6; i++ {
		makeVideo(t, tools, filepath.Join(root, "Movie "+itoa(i)+" (2020).mp4"), 1)
	}
	lib := addLibrary(t, db, "Films", models.LibraryMovie, root)

	events, unsubscribe := s.Hub().Subscribe()
	defer unsubscribe()

	if err := s.Scan(context.Background(), lib.ID); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	// The second request must be refused while the first is in flight.
	if err := s.Scan(context.Background(), lib.ID); err == nil {
		t.Error("expected the second concurrent scan to be rejected")
	}

	deadline := time.After(120 * time.Second)
	for {
		select {
		case p := <-events:
			if p.Finished {
				return
			}
		case <-deadline:
			t.Fatal("scan did not finish")
		}
	}
}

func keysOf(m map[string]models.Movie) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func showTitles(m map[string]models.Show) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
