package transcode

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"kino/internal/ffmpeg"
	"kino/internal/models"
)

// ErrNoTools is returned when a transcode is requested but ffmpeg is not installed.
var ErrNoTools = errors.New("ffmpeg is required for transcoding")

// ErrSessionNotFound is returned for an unknown or already-expired session ID.
var ErrSessionNotFound = errors.New("playback session not found")

// Manager owns every live transcode session.
type Manager struct {
	tools   *ffmpeg.Tools
	encoder ffmpeg.Encoder
	caps    ffmpeg.Capabilities

	rootDir     string
	segmentSec  int
	idleTimeout time.Duration

	// Advanced tuning from config.
	encoderPreset    string
	bitrateOverrides map[string]int
	audioChannels    int

	// limiter bounds concurrent encoder processes across all sessions.
	limiter chan struct{}

	mu       sync.RWMutex
	sessions map[string]*Session

	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// Options configures a Manager.
type Options struct {
	Tools          *ffmpeg.Tools
	RootDir        string
	SegmentSeconds int
	IdleSeconds    int
	MaxConcurrent  int
	HWAccel        string

	EncoderPreset    string
	BitrateOverrides map[string]int
	AudioChannels    int
}

// NewManager builds a transcode manager and starts its garbage collector.
func NewManager(opts Options) *Manager {
	if opts.SegmentSeconds < 2 {
		opts.SegmentSeconds = 4
	}
	if opts.IdleSeconds < 10 {
		opts.IdleSeconds = 60
	}
	if opts.MaxConcurrent < 1 {
		opts.MaxConcurrent = 2
	}
	if opts.AudioChannels < 1 || opts.AudioChannels > 8 {
		opts.AudioChannels = 2
	}

	m := &Manager{
		tools:            opts.Tools,
		rootDir:          opts.RootDir,
		segmentSec:       opts.SegmentSeconds,
		idleTimeout:      time.Duration(opts.IdleSeconds) * time.Second,
		encoderPreset:    opts.EncoderPreset,
		bitrateOverrides: opts.BitrateOverrides,
		audioChannels:    opts.AudioChannels,
		limiter:          make(chan struct{}, opts.MaxConcurrent),
		sessions:         make(map[string]*Session),
		stop:             make(chan struct{}),
	}

	if opts.Tools != nil {
		m.caps = opts.Tools.DetectEncoders(opts.HWAccel)
		m.encoder = m.caps.Selected
		log.Printf("transcode: using %s (%d concurrent transcodes max)", m.encoder.Label, opts.MaxConcurrent)
	}

	// A crash leaves segment directories behind. Clearing them at startup stops the
	// temp folder growing without bound across restarts.
	m.cleanRoot()

	m.wg.Add(1)
	go m.gcLoop()

	return m
}

func (m *Manager) cleanRoot() {
	if m.rootDir == "" {
		return
	}
	if err := os.RemoveAll(m.rootDir); err != nil && !os.IsNotExist(err) {
		log.Printf("transcode: could not clear %s: %v", m.rootDir, err)
	}
	if err := os.MkdirAll(m.rootDir, 0o755); err != nil {
		log.Printf("transcode: could not create %s: %v", m.rootDir, err)
	}
}

// Capabilities reports which encoders are usable and which one is selected.
func (m *Manager) Capabilities() ffmpeg.Capabilities { return m.caps }

// Encoder returns the encoder in use.
func (m *Manager) Encoder() ffmpeg.Encoder { return m.encoder }

// SegmentSeconds is the configured segment length.
func (m *Manager) SegmentSeconds() int { return m.segmentSec }

// StartOptions describes the stream a client wants.
type StartOptions struct {
	UserID  string
	MediaID string
	Media   models.MediaInfo
	// AudioIndex selects the audio track; -1 means "use the container default".
	AudioIndex int
	// BurnSubtitleIndex burns an embedded subtitle track into the video; -1 for none.
	BurnSubtitleIndex int
}

// Start creates a session and returns it. Nothing is encoded until the client
// requests its first segment.
func (m *Manager) Start(opts StartOptions) (*Session, error) {
	if m.tools == nil {
		return nil, ErrNoTools
	}
	if !opts.Media.Probed {
		return nil, errors.New("this file has not been analysed yet; rescan the library")
	}
	if opts.Media.DurationSec <= 0 {
		return nil, errors.New("this file has no readable duration, so it cannot be streamed")
	}

	audioIndex := opts.AudioIndex
	if audioIndex < 0 {
		audioIndex = opts.Media.DefaultAudioIndex()
	}

	width, height := opts.Media.Resolution()
	ladder := ApplyOverrides(BuildLadder(width, height, opts.Media.BitRate), m.bitrateOverrides)

	id := uuid.NewString()
	dir := filepath.Join(m.rootDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create session directory: %w", err)
	}

	session := &Session{
		ID:            id,
		UserID:        opts.UserID,
		MediaID:       opts.MediaID,
		MediaPath:     opts.Media.Path,
		DurationSec:   opts.Media.DurationSec,
		SegmentSec:    m.segmentSec,
		Ladder:        ladder,
		AudioIndex:    audioIndex,
		SubtitleIndex: opts.BurnSubtitleIndex,
		Dir:           dir,
		tools:         m.tools,
		encoder:       m.encoder,
		preset:        m.encoderPreset,
		audioChannels: m.audioChannels,
		limiter:       m.limiter,
		workers:       make(map[string]*worker),
		lastAccess:    time.Now(),
		CreatedAt:     time.Now(),
	}

	m.mu.Lock()
	m.sessions[id] = session
	m.mu.Unlock()

	log.Printf("transcode: session %s started for %s (%dx%d, %d renditions, audio track %d)",
		id, filepath.Base(opts.Media.Path), width, height, len(ladder), audioIndex)

	return session, nil
}

// Get looks up a live session.
func (m *Manager) Get(id string) (*Session, error) {
	m.mu.RLock()
	session, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

// Stop ends a session and frees its resources.
func (m *Manager) Stop(id string) bool {
	m.mu.Lock()
	session, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()

	if !ok {
		return false
	}
	session.Close()
	return true
}

// StopForUser ends every session belonging to a user, used on logout and when the
// account is deleted.
func (m *Manager) StopForUser(userID string) int {
	m.mu.Lock()
	var doomed []*Session
	for id, s := range m.sessions {
		if s.UserID == userID {
			doomed = append(doomed, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range doomed {
		s.Close()
	}
	return len(doomed)
}

// Active returns a snapshot of running sessions, for the admin dashboard.
func (m *Manager) Active() []Info {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]Info, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Info())
	}
	return out
}

// gcLoop reaps sessions nobody is watching any more.
//
// This is the safety net that matters most: a browser tab closed mid-film sends no
// "stop" request, so without this the encoder would keep running and filling the disk
// until the server restarted.
func (m *Manager) gcLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			m.reapIdle()
		}
	}
}

func (m *Manager) reapIdle() {
	m.mu.Lock()
	var doomed []*Session
	for id, s := range m.sessions {
		if s.IdleFor() > m.idleTimeout {
			doomed = append(doomed, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for _, s := range doomed {
		log.Printf("transcode: reaping idle session %s (no requests for %s)",
			s.ID, s.IdleFor().Round(time.Second))
		s.Close()
	}
}

// Close shuts down the manager, killing every encoder and clearing the scratch space.
func (m *Manager) Close() {
	m.stopOnce.Do(func() { close(m.stop) })
	m.wg.Wait()

	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}

	if m.rootDir != "" {
		_ = os.RemoveAll(m.rootDir)
	}
}
