package transcode

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"kino/internal/ffmpeg"
)

var (
	// ErrSegmentTimeout means the encoder did not produce the requested segment in
	// time; the client should retry or drop to a lower rendition.
	ErrSegmentTimeout = errors.New("timed out waiting for the segment to be encoded")
	// ErrSessionClosed is returned once a session has been torn down.
	ErrSessionClosed = errors.New("this playback session has ended")
)

const (
	// segmentWaitTimeout bounds how long a segment request blocks. Long enough for a
	// slow CPU to encode a few seconds of 1080p, short enough that a wedged ffmpeg
	// surfaces as an error rather than a hung player.
	segmentWaitTimeout = 60 * time.Second
	// segmentPollInterval is how often we check for the segment file to appear.
	segmentPollInterval = 40 * time.Millisecond
	// lookaheadLimit is how many segments ahead of the encoder's current output a
	// request may be before we treat it as a seek and restart the encoder there.
	// Waiting for the encoder to catch up beyond this would take longer than
	// restarting it.
	lookaheadLimit = 12
)

// Session is one client's transcode of one media file with one audio/subtitle choice.
type Session struct {
	ID      string
	UserID  string
	MediaID string

	MediaPath   string
	DurationSec float64
	SegmentSec  int
	Ladder      []Rendition

	// AudioIndex is the N in ffmpeg's -map 0:a:N.
	AudioIndex int
	// SubtitleIndex is the embedded subtitle track to burn in, or -1 for none.
	// Burn-in is only used for image-based subtitles that cannot be converted to WebVTT.
	SubtitleIndex int

	Dir string

	tools   *ffmpeg.Tools
	encoder ffmpeg.Encoder
	// preset overrides the encoder's speed/quality preset; empty uses its default.
	preset string
	// audioChannels is the output channel count, normally 2 for browsers.
	audioChannels int
	limiter       chan struct{}

	mu         sync.Mutex
	workers    map[string]*worker
	closed     bool
	lastAccess time.Time
	CreatedAt  time.Time
}

// worker is a running ffmpeg process producing one variant from a start segment.
type worker struct {
	rendition Rendition
	startSeg  int

	cancel context.CancelFunc
	done   chan struct{}

	mu       sync.Mutex
	produced int // highest segment index observed on disk, -1 before the first
	failed   error
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastAccess = time.Now()
	s.mu.Unlock()
}

// IdleFor reports how long since the last segment or playlist request.
func (s *Session) IdleFor() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.lastAccess)
}

// MasterPlaylist renders the master playlist for this session.
func (s *Session) MasterPlaylist() string {
	s.touch()
	return MasterPlaylist(s.Ladder, func(r Rendition) string {
		return r.Name + "/index.m3u8"
	})
}

// VariantPlaylist renders the segment list for one rendition.
func (s *Session) VariantPlaylist(name string) (string, bool) {
	if _, ok := FindRendition(s.Ladder, name); !ok {
		return "", false
	}
	s.touch()
	return VariantPlaylist(s.DurationSec, s.SegmentSec, func(n int) string {
		return fmt.Sprintf("seg%d.ts", n)
	}), true
}

// SegmentPath returns the on-disk path for a segment, encoding it first if needed.
//
// This is the hot path: it is called once per segment per playing client. The common
// case is a cache hit on a segment the running encoder already produced.
func (s *Session) SegmentPath(ctx context.Context, variant string, n int) (string, error) {
	s.touch()

	rendition, ok := FindRendition(s.Ladder, variant)
	if !ok {
		return "", fmt.Errorf("unknown rendition %q", variant)
	}
	if n < 0 || n >= SegmentCount(s.DurationSec, s.SegmentSec) {
		return "", fmt.Errorf("segment %d is out of range", n)
	}

	path := s.segmentFile(variant, n)

	// Fast path: already encoded.
	if fileReady(path) {
		return path, nil
	}

	if err := s.ensureWorker(variant, rendition, n); err != nil {
		return "", err
	}

	return s.waitForSegment(ctx, variant, n, path)
}

// ensureWorker makes sure an encoder is running that will reach segment n soon,
// restarting it at n when the request is a seek away from where it currently is.
func (s *Session) ensureWorker(variant string, rendition Rendition, n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrSessionClosed
	}

	existing := s.workers[variant]
	if existing != nil && existing.covers(n) {
		return nil
	}

	if existing != nil {
		// The request is behind the encoder, or so far ahead that waiting would cost
		// more than starting over. Either way this is a seek: kill and restart.
		log.Printf("transcode: session %s variant %s seeking to segment %d", s.ID, variant, n)
		existing.stop()
		delete(s.workers, variant)
	}

	w, err := s.startWorker(variant, rendition, n)
	if err != nil {
		return err
	}
	s.workers[variant] = w
	return nil
}

// covers reports whether this worker will produce segment n without restarting.
func (w *worker) covers(n int) bool {
	if n < w.startSeg {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed != nil {
		return false
	}
	// Before the first segment lands, only the start segment is covered.
	if w.produced < 0 {
		return n <= w.startSeg+lookaheadLimit
	}
	return n <= w.produced+lookaheadLimit
}

func (w *worker) stop() {
	w.cancel()
	// Wait briefly for the process to actually die so its files stop changing under
	// the replacement worker that is about to write to the same directory.
	select {
	case <-w.done:
	case <-time.After(5 * time.Second):
		log.Printf("transcode: worker did not exit promptly after cancel")
	}
}

// startWorker launches ffmpeg to produce a variant starting at segment n.
//
// Caller must hold s.mu.
func (s *Session) startWorker(variant string, rendition Rendition, startSeg int) (*worker, error) {
	dir := filepath.Join(s.Dir, variant)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create segment dir: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := &worker{
		rendition: rendition,
		startSeg:  startSeg,
		cancel:    cancel,
		done:      make(chan struct{}),
		produced:  -1,
	}

	startTime := float64(startSeg) * float64(s.SegmentSec)
	args := s.buildArgs(rendition, dir, startSeg, startTime)

	go func() {
		defer close(w.done)
		defer cancel()

		// Bound how many encoders run at once across the whole server, so five
		// simultaneous viewers cannot bring the machine to its knees.
		select {
		case s.limiter <- struct{}{}:
			defer func() { <-s.limiter }()
		case <-ctx.Done():
			return
		}

		if err := s.runFFmpeg(ctx, args, w, dir); err != nil && ctx.Err() == nil {
			w.mu.Lock()
			w.failed = err
			w.mu.Unlock()
			log.Printf("transcode: session %s variant %s: %v", s.ID, variant, err)
		}
	}()

	return w, nil
}

// buildArgs assembles the ffmpeg command line for one variant.
func (s *Session) buildArgs(r Rendition, dir string, startSeg int, startTime float64) []string {
	var args []string

	args = append(args, "-hide_banner", "-loglevel", "warning", "-nostdin")

	// Hardware-accelerated decoding, when the selected encoder supports it.
	if len(s.encoder.HWAccelArgs) > 0 && s.SubtitleIndex < 0 {
		// Subtitle burn-in needs frames in system memory, so hardware decode is
		// skipped in that case rather than paying for a download/upload round trip.
		args = append(args, s.encoder.HWAccelArgs...)
	}

	// Input seeking (-ss before -i) jumps by keyframe and is orders of magnitude
	// faster than decoding from the start to reach the seek point.
	if startTime > 0 {
		args = append(args, "-ss", fmt.Sprintf("%.3f", startTime))
	}
	args = append(args, "-i", s.MediaPath)

	// Explicit stream selection: without -map, ffmpeg picks by its own heuristics and
	// the user's audio-track choice would be silently ignored.
	args = append(args, "-map", "0:v:0")
	if s.AudioIndex >= 0 {
		args = append(args, "-map", fmt.Sprintf("0:a:%d", s.AudioIndex))
	}
	args = append(args, "-sn", "-dn") // never pass subtitle or data streams into HLS

	// Video filters: burn-in first (it operates on the source resolution), then scale.
	var filters []string
	if s.SubtitleIndex >= 0 {
		filters = append(filters, subtitleBurnFilter(s.MediaPath, s.SubtitleIndex))
	}
	if r.Height > 0 {
		filters = append(filters, ffmpeg.ScaleFilter(s.encoder, r.Width, r.Height))
	}
	if len(filters) > 0 {
		args = append(args, "-vf", joinFilters(filters))
	}

	args = append(args, "-c:v", s.encoder.Name)
	args = append(args, ffmpeg.EncoderOptions(s.encoder, r.VideoBitrate, r.MaxBitrate(), r.BufSize())...)
	// A configured preset is appended last so it wins over the encoder default; ffmpeg
	// takes the final occurrence of a repeated option.
	if s.preset != "" {
		args = append(args, "-preset", s.preset)
	}

	// Force a keyframe exactly on every segment boundary. Without this ffmpeg cuts
	// segments at whatever keyframes the source happens to have, the segment
	// durations stop matching the playlist, and seeking lands in the wrong place.
	args = append(args,
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", s.SegmentSec),
		"-pix_fmt", "yuv420p",
	)

	// Downmix to stereo AAC: browsers cannot decode AC3, DTS or TrueHD, and a 5.1 AAC
	// track plays back with missing channels on most laptops anyway.
	channels := s.audioChannels
	if channels < 1 {
		channels = 2
	}
	args = append(args,
		"-c:a", "aac",
		"-b:a", fmt.Sprintf("%dk", r.AudioBitrate/1000),
		"-ac", fmt.Sprintf("%d", channels),
		"-ar", "48000",
	)

	// Shift output timestamps to the absolute position in the film. Without this every
	// seek would restart the timeline at zero and the player's clock would jump.
	if startTime > 0 {
		args = append(args, "-output_ts_offset", fmt.Sprintf("%.3f", startTime))
	}

	args = append(args,
		"-f", "hls",
		"-hls_time", fmt.Sprintf("%d", s.SegmentSec),
		"-hls_playlist_type", "vod",
		"-hls_segment_type", "mpegts",
		"-hls_list_size", "0",
		// temp_file makes ffmpeg write each segment to a .tmp and rename it into
		// place. That rename is atomic, so the mere existence of segN.ts proves the
		// segment is complete and we never serve a half-written file.
		"-hls_flags", "temp_file+independent_segments",
		"-start_number", fmt.Sprintf("%d", startSeg),
		"-hls_segment_filename", filepath.Join(dir, "seg%d.ts"),
		filepath.Join(dir, "worker.m3u8"),
	)

	return args
}

func joinFilters(filters []string) string {
	out := filters[0]
	for _, f := range filters[1:] {
		out += "," + f
	}
	return out
}

// subtitleBurnFilter renders an embedded subtitle track into the picture. This is
// only used for image-based formats (PGS, VOBSUB) which cannot become WebVTT.
func subtitleBurnFilter(mediaPath string, index int) string {
	return fmt.Sprintf("subtitles=%s:si=%d", escapeFilterPath(mediaPath), index)
}

// escapeFilterPath quotes a path for use inside an ffmpeg filter argument.
//
// ffmpeg's filter parser treats backslashes, colons, commas and brackets as syntax,
// which makes every Windows path (C:\Media\...) a parse error unless escaped.
func escapeFilterPath(p string) string {
	var b []rune
	for _, r := range p {
		switch r {
		case '\\', ':', '\'', '[', ']', ',', ';':
			b = append(b, '\\', r)
		default:
			b = append(b, r)
		}
	}
	return "'" + string(b) + "'"
}

// segmentFile is the on-disk path of a segment.
func (s *Session) segmentFile(variant string, n int) string {
	return filepath.Join(s.Dir, variant, fmt.Sprintf("seg%d.ts", n))
}

// waitForSegment blocks until the segment appears, the encoder fails, or we time out.
func (s *Session) waitForSegment(ctx context.Context, variant string, n int, path string) (string, error) {
	deadline := time.NewTimer(segmentWaitTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(segmentPollInterval)
	defer ticker.Stop()

	for {
		if fileReady(path) {
			s.noteProduced(variant, n)
			return path, nil
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()

		case <-deadline.C:
			return "", ErrSegmentTimeout

		case <-ticker.C:
			s.mu.Lock()
			w := s.workers[variant]
			closed := s.closed
			s.mu.Unlock()

			if closed {
				return "", ErrSessionClosed
			}
			if w == nil {
				return "", errors.New("the encoder stopped unexpectedly")
			}

			w.mu.Lock()
			failed := w.failed
			w.mu.Unlock()
			if failed != nil {
				return "", fmt.Errorf("encoding failed: %w", failed)
			}

			// The encoder may have exited cleanly having already written the file.
			select {
			case <-w.done:
				if fileReady(path) {
					s.noteProduced(variant, n)
					return path, nil
				}
				return "", errors.New("the encoder exited before producing this segment")
			default:
			}
		}
	}
}

func (s *Session) noteProduced(variant string, n int) {
	s.mu.Lock()
	w := s.workers[variant]
	s.mu.Unlock()
	if w == nil {
		return
	}
	w.mu.Lock()
	if n > w.produced {
		w.produced = n
	}
	w.mu.Unlock()
}

// fileReady reports whether a segment exists and is non-empty. Thanks to the
// temp_file flag, a visible file is always a complete one.
func fileReady(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// Close tears down the session: every encoder is killed and the scratch directory
// removed.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	workers := make([]*worker, 0, len(s.workers))
	for _, w := range s.workers {
		workers = append(workers, w)
	}
	s.workers = nil
	dir := s.Dir
	s.mu.Unlock()

	for _, w := range workers {
		w.stop()
	}

	if dir != "" {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("transcode: could not remove %s: %v", dir, err)
		}
	}
	log.Printf("transcode: session %s closed", s.ID)
}

// Info is the client-facing description of a session.
type Info struct {
	ID          string      `json:"id"`
	MediaID     string      `json:"mediaId"`
	DurationSec float64     `json:"durationSec"`
	SegmentSec  int         `json:"segmentSec"`
	Ladder      []Rendition `json:"ladder"`
	AudioIndex  int         `json:"audioIndex"`
	Encoder     string      `json:"encoder"`
	Hardware    bool        `json:"hardware"`
	MasterURL   string      `json:"masterUrl"`
}

// Info describes this session for the client.
func (s *Session) Info() Info {
	return Info{
		ID:          s.ID,
		MediaID:     s.MediaID,
		DurationSec: s.DurationSec,
		SegmentSec:  s.SegmentSec,
		Ladder:      s.Ladder,
		AudioIndex:  s.AudioIndex,
		Encoder:     s.encoder.Label,
		Hardware:    s.encoder.Hardware,
		MasterURL:   "/hls/" + s.ID + "/master.m3u8",
	}
}
