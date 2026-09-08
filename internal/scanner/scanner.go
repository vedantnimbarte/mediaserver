package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"kino/internal/ffmpeg"
	"kino/internal/images"
	"kino/internal/models"
	"kino/internal/store"
)

// ErrAlreadyScanning is returned when a scan is requested for a library that is
// already being scanned.
var ErrAlreadyScanning = errors.New("this library is already being scanned")

// ErrNoProbe is returned when a scan needs ffprobe but it could not be found.
var ErrNoProbe = errors.New("ffprobe is required to scan media libraries")

// Phase describes what a scan is currently doing, for the progress UI.
type Phase string

const (
	PhaseWalking  Phase = "walking"  // enumerating files on disk
	PhaseProbing  Phase = "probing"  // running ffprobe / reading tags
	PhaseMetadata Phase = "metadata" // fetching from the metadata provider
	PhaseCleaning Phase = "cleaning" // marking vanished files unavailable
	PhaseDone     Phase = "done"
	PhaseFailed   Phase = "failed"
)

// Progress is a snapshot of a running or finished scan.
type Progress struct {
	LibraryID   string    `json:"libraryId"`
	LibraryName string    `json:"libraryName"`
	Phase       Phase     `json:"phase"`
	Total       int       `json:"total"`
	Done        int       `json:"done"`
	Added       int       `json:"added"`
	Updated     int       `json:"updated"`
	Removed     int       `json:"removed"`
	Current     string    `json:"current,omitempty"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt,omitempty"`
	Finished    bool      `json:"finished"`
}

// MetadataEnricher fills in provider metadata for freshly scanned items. It is an
// interface so the scanner does not depend on the TMDb client directly, which keeps
// scanning testable without network access.
type MetadataEnricher interface {
	EnrichMovie(ctx context.Context, movie *models.Movie) error
	EnrichShow(ctx context.Context, show *models.Show) error
	Enabled() bool
}

// Scanner walks library folders and turns what it finds into stored records.
type Scanner struct {
	db     *store.DB
	tools  *ffmpeg.Tools
	meta   MetadataEnricher
	images *images.Cache

	// workers bounds concurrent ffprobe invocations. Probing is IO- and process-bound,
	// so oversubscribing the CPU here just thrashes.
	workers int

	mu      sync.Mutex
	running map[string]context.CancelFunc
	latest  map[string]*Progress

	hub *ProgressHub
}

// New builds a scanner. tools may be nil, in which case scans that need ffprobe fail
// with a clear error rather than panicking. imageCache may also be nil, which only
// disables extracting embedded album art.
func New(db *store.DB, tools *ffmpeg.Tools, meta MetadataEnricher, imageCache *images.Cache) *Scanner {
	workers := runtime.NumCPU() / 2
	if workers < 2 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}

	return &Scanner{
		db:      db,
		tools:   tools,
		meta:    meta,
		images:  imageCache,
		workers: workers,
		running: make(map[string]context.CancelFunc),
		latest:  make(map[string]*Progress),
		hub:     NewProgressHub(),
	}
}

// Hub exposes the progress broadcaster so the API can stream it to clients.
func (s *Scanner) Hub() *ProgressHub { return s.hub }

// SetTools swaps in newly located ffmpeg binaries, used when the user fixes the path
// in Settings without restarting the server.
func (s *Scanner) SetTools(t *ffmpeg.Tools) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = t
}

// Tools returns the current ffmpeg binaries, or nil if none were found.
func (s *Scanner) Tools() *ffmpeg.Tools {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tools
}

// SetMetadata swaps in a new enricher, used when the user saves a metadata API key
// so the next scan picks it up without a restart.
func (s *Scanner) SetMetadata(m MetadataEnricher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta = m
}

// metadataService returns the enricher under the lock.
func (s *Scanner) metadataService() MetadataEnricher {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta
}

// IsScanning reports whether a scan is in flight for the given library.
func (s *Scanner) IsScanning(libraryID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.running[libraryID]
	return ok
}

// Progress returns the most recent progress snapshot for every library.
func (s *Scanner) Progress() []Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Progress, 0, len(s.latest))
	for _, p := range s.latest {
		out = append(out, *p)
	}
	return out
}

// Cancel stops an in-flight scan.
func (s *Scanner) Cancel(libraryID string) bool {
	s.mu.Lock()
	cancel, ok := s.running[libraryID]
	s.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}

// CancelAll stops every in-flight scan, used during shutdown.
func (s *Scanner) CancelAll() {
	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.running))
	for _, c := range s.running {
		cancels = append(cancels, c)
	}
	s.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// Scan runs a scan for one library. It returns as soon as the scan has started;
// progress is reported through the hub.
func (s *Scanner) Scan(ctx context.Context, libraryID string) error {
	lib, err := s.db.Libraries.Get(libraryID)
	if err != nil {
		return fmt.Errorf("library %s: %w", libraryID, err)
	}

	if s.tools == nil && lib.Type != models.LibraryPhoto {
		return ErrNoProbe
	}

	s.mu.Lock()
	if _, busy := s.running[libraryID]; busy {
		s.mu.Unlock()
		return ErrAlreadyScanning
	}
	// Detach from the request context: a scan must outlive the HTTP call that began it.
	scanCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.running[libraryID] = cancel
	s.mu.Unlock()

	_, _ = s.db.Libraries.Update(libraryID, func(l *models.Library) { l.Scanning = true })

	// release frees the scan slot. It runs before the terminal progress event is
	// published, because clients react to "finished" by immediately offering (or
	// triggering) another scan, and that request must not be rejected as a duplicate.
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			cancel()
			s.mu.Lock()
			delete(s.running, libraryID)
			s.mu.Unlock()
			_, _ = s.db.Libraries.Update(libraryID, func(l *models.Library) {
				l.Scanning = false
				l.LastScanAt = time.Now()
			})
		})
	}

	go func() {
		prog := &Progress{
			LibraryID:   lib.ID,
			LibraryName: lib.Name,
			Phase:       PhaseWalking,
			StartedAt:   time.Now(),
		}

		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("scanner: panic scanning %s: %v", lib.Name, rec)
				release()
				prog.Phase = PhaseFailed
				prog.Error = fmt.Sprintf("Internal error: %v", rec)
				prog.Finished = true
				prog.FinishedAt = time.Now()
				s.publish(prog)
			}
		}()

		s.publish(prog)
		err := s.dispatch(scanCtx, lib, prog)

		release()
		s.finish(lib, prog, err)
	}()

	return nil
}

// dispatch runs the scan appropriate to the library type.
func (s *Scanner) dispatch(ctx context.Context, lib models.Library, prog *Progress) error {
	switch lib.Type {
	case models.LibraryMovie:
		return s.scanMovies(ctx, lib, prog)
	case models.LibraryShow:
		return s.scanShows(ctx, lib, prog)
	case models.LibraryMusic:
		return s.scanMusic(ctx, lib, prog)
	case models.LibraryPhoto:
		return s.scanPhotos(ctx, lib, prog)
	default:
		return fmt.Errorf("unknown library type %q", lib.Type)
	}
}

// finish records the terminal state and publishes the final progress event.
func (s *Scanner) finish(lib models.Library, prog *Progress, err error) {
	switch {
	case err == nil:
		prog.Phase = PhaseDone
		prog.Current = ""
	case errors.Is(err, context.Canceled):
		log.Printf("scanner: scan of %s cancelled", lib.Name)
		prog.Phase = PhaseFailed
		prog.Error = "Scan cancelled."
	default:
		log.Printf("scanner: scan of %s failed: %v", lib.Name, err)
		prog.Phase = PhaseFailed
		prog.Error = err.Error()
	}

	prog.Finished = true
	prog.FinishedAt = time.Now()
	s.publish(prog)

	log.Printf("scanner: %s finished in %s (%d added, %d updated, %d removed)",
		lib.Name, time.Since(prog.StartedAt).Round(time.Second),
		prog.Added, prog.Updated, prog.Removed)
}

// publish records the latest progress and broadcasts it to SSE subscribers.
func (s *Scanner) publish(p *Progress) {
	s.mu.Lock()
	snapshot := *p
	s.latest[p.LibraryID] = &snapshot
	s.mu.Unlock()
	s.hub.Broadcast(snapshot)
}

// ---- shared walking ----

// walkFiles enumerates every file under the library's paths that passes accept.
//
// Unreadable directories are logged and skipped rather than aborting the walk: one
// permission-denied folder should not stop a whole library from being scanned.
func walkFiles(ctx context.Context, paths []string, accept func(string) bool) ([]string, error) {
	var out []string
	seen := make(map[string]bool)

	for _, root := range paths {
		if err := ctx.Err(); err != nil {
			return out, err
		}

		info, err := os.Stat(root)
		if err != nil {
			log.Printf("scanner: skipping %s: %v", root, err)
			continue
		}
		if !info.IsDir() {
			if accept(root) {
				out = append(out, root)
			}
			continue
		}

		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if err != nil {
				log.Printf("scanner: skipping %s: %v", path, err)
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if path != root && SkipDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if !accept(path) {
				return nil
			}
			// The same file can be reachable through two configured paths; de-duplicate
			// so it does not produce two library entries.
			key := normalizePathKey(path)
			if seen[key] {
				return nil
			}
			seen[key] = true
			out = append(out, path)
			return nil
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("scanner: walking %s: %v", root, err)
		}
		if errors.Is(err, context.Canceled) {
			return out, err
		}
	}

	return out, nil
}

func normalizePathKey(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	return filepath.Clean(abs)
}

// probeAll runs ffprobe across files with a bounded worker pool, reporting progress
// as results land. Results come back in the same order as the input.
func (s *Scanner) probeAll(ctx context.Context, files []string, prog *Progress) []models.MediaInfo {
	results := make([]models.MediaInfo, len(files))

	type job struct {
		idx  int
		path string
	}
	jobs := make(chan job)

	var (
		wg       sync.WaitGroup
		progMu   sync.Mutex
		lastEmit time.Time
	)

	for w := 0; w < s.workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					return
				}
				info, err := s.tools.Probe(ctx, j.path)
				if err != nil {
					log.Printf("scanner: probe %s: %v", filepath.Base(j.path), err)
				}
				results[j.idx] = info

				progMu.Lock()
				prog.Done++
				prog.Current = filepath.Base(j.path)
				// Emitting on every file would flood SSE clients on a large library;
				// 10 updates a second is smooth enough for a progress bar.
				if time.Since(lastEmit) > 100*time.Millisecond || prog.Done == prog.Total {
					lastEmit = time.Now()
					s.publish(prog)
				}
				progMu.Unlock()
			}
		}()
	}

	for i, path := range files {
		select {
		case jobs <- job{idx: i, path: path}:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return results
		}
	}
	close(jobs)
	wg.Wait()

	return results
}
