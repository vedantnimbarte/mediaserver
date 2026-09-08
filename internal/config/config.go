// Package config loads and persists server configuration from data/config.json.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Config is the persisted server configuration. It lives at <DataDir>/config.json.
// DataDir itself is resolved at startup and never serialized.
type Config struct {
	// Server
	Host string `json:"host"`
	Port int    `json:"port"`

	// Auth. Generated on first boot; changing it invalidates every session.
	JWTSecret     string `json:"jwtSecret"`
	SetupComplete bool   `json:"setupComplete"`

	// External tools. Empty means "look it up on PATH".
	FFmpegPath  string `json:"ffmpegPath"`
	FFprobePath string `json:"ffprobePath"`

	// Metadata
	TMDbAPIKey   string `json:"tmdbApiKey"`
	MetadataLang string `json:"metadataLang"`

	// Transcoding
	HWAccel         string `json:"hwAccel"`         // auto | nvenc | qsv | amf | videotoolbox | cpu
	MaxTranscodes   int    `json:"maxTranscodes"`   // concurrent ffmpeg sessions
	SegmentSeconds  int    `json:"segmentSeconds"`  // HLS segment length
	SessionIdleSecs int    `json:"sessionIdleSecs"` // kill a session after this long with no segment request
	TranscodeTemp   string `json:"transcodeTemp"`   // empty = os.TempDir()/mediaserver-transcode

	// Advanced transcoding. These exist so a user can trade quality against CPU on
	// their own hardware without rebuilding.

	// EncoderPreset overrides the speed/quality preset. Empty uses the per-encoder
	// default, which is tuned for keeping ahead of live playback.
	EncoderPreset string `json:"encoderPreset"`
	// BitrateOverrides maps a rendition name ("1080p") to a bitrate in bits per
	// second, replacing the built-in ladder value for that rung.
	BitrateOverrides map[string]int `json:"bitrateOverrides,omitempty"`
	// BurnInSubtitlesByDefault renders subtitles into the video rather than sending
	// them as a separate track. Costs CPU but works on any client.
	BurnInSubtitlesByDefault bool `json:"burnInSubtitlesByDefault"`
	// AllowDirectPlay can be turned off to force everything through the transcoder,
	// which is the quickest way to reproduce a playback problem.
	AllowDirectPlay bool `json:"allowDirectPlay"`
	// AudioChannels caps the output channel count. Two is right for browsers.
	AudioChannels int `json:"audioChannels"`

	// Scanning
	ScanOnStart  bool `json:"scanOnStart"`
	WatchFolders bool `json:"watchFolders"`

	// DataDir is where every JSON collection and the image cache live.
	DataDir string `json:"-"`

	// mu is a pointer so that Config stays copyable (Snapshot) without tripping vet's
	// copylocks check. It is allocated by Defaults/Load and is never nil in practice.
	mu   *sync.RWMutex `json:"-"`
	path string        `json:"-"`
}

func (c *Config) rlock()   { c.mu.RLock() }
func (c *Config) runlock() { c.mu.RUnlock() }

// Defaults returns a Config with every field set to a sane starting value.
func Defaults() *Config {
	return &Config{
		mu:              &sync.RWMutex{},
		Host:            "0.0.0.0",
		Port:            8096,
		MetadataLang:    "en-US",
		HWAccel:         "auto",
		MaxTranscodes:   maxDefault(),
		SegmentSeconds:  4,
		SessionIdleSecs: 60,
		AllowDirectPlay: true,
		AudioChannels:   2,
		ScanOnStart:     false,
		WatchFolders:    true,
	}
}

func maxDefault() int {
	n := runtime.NumCPU() / 4
	if n < 1 {
		n = 1
	}
	return n
}

// Load reads config.json from dataDir, filling in defaults for missing fields and
// generating a JWT secret on first run. The file is created if it does not exist.
func Load(dataDir string) (*Config, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	cfg := Defaults()
	cfg.DataDir = dataDir
	cfg.path = filepath.Join(dataDir, "config.json")

	data, err := os.ReadFile(cfg.path)
	switch {
	case err == nil:
		// Unmarshal over the defaults so absent keys keep their default value.
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", cfg.path, err)
		}
	case os.IsNotExist(err):
		// First run.
	default:
		return nil, fmt.Errorf("read %s: %w", cfg.path, err)
	}

	// Repair anything nonsensical that may have been hand-edited.
	if cfg.Port <= 0 || cfg.Port > 65535 {
		cfg.Port = 8096
	}
	if cfg.SegmentSeconds < 2 {
		cfg.SegmentSeconds = 4
	}
	if cfg.MaxTranscodes < 1 {
		cfg.MaxTranscodes = maxDefault()
	}
	if cfg.SessionIdleSecs < 10 {
		cfg.SessionIdleSecs = 60
	}
	if cfg.MetadataLang == "" {
		cfg.MetadataLang = "en-US"
	}
	if cfg.HWAccel == "" {
		cfg.HWAccel = "auto"
	}
	if cfg.AudioChannels < 1 || cfg.AudioChannels > 8 {
		cfg.AudioChannels = 2
	}

	if cfg.JWTSecret == "" {
		secret, err := randomSecret()
		if err != nil {
			return nil, err
		}
		cfg.JWTSecret = secret
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func randomSecret() (string, error) {
	buf := make([]byte, 48)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate jwt secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Save writes the config back to disk atomically.
func (c *Config) Save() error {
	c.mu.RLock()
	data, err := json.MarshalIndent(c, "", "  ")
	path := c.path
	c.mu.RUnlock()
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// Update applies fn under the write lock and persists the result.
func (c *Config) Update(fn func(*Config)) error {
	c.mu.Lock()
	fn(c)
	c.mu.Unlock()
	return c.Save()
}

// CacheDir is where downloaded posters and generated thumbnails live.
func (c *Config) CacheDir() string { return filepath.Join(c.DataDir, "cache") }

// ImageDir is the on-disk image cache root.
func (c *Config) ImageDir() string { return filepath.Join(c.CacheDir(), "images") }

// SubtitleDir caches subtitles extracted out of media containers.
func (c *Config) SubtitleDir() string { return filepath.Join(c.CacheDir(), "subtitles") }

// TranscodeDir is the scratch space for HLS sessions. It deliberately defaults to
// the OS temp dir so a crash cannot leave gigabytes of segments in the data dir.
func (c *Config) TranscodeDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.TranscodeTemp != "" {
		return c.TranscodeTemp
	}
	return filepath.Join(os.TempDir(), "kino-transcode")
}

// Addr is the listen address for the HTTP server.
func (c *Config) Addr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

// Snapshot returns a detached copy safe to hand to templates or JSON encoders.
// The copy carries no lock and must not be mutated through Update.
func (c *Config) Snapshot() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := *c
	cp.mu = nil
	return cp
}

// DefaultDataDir resolves the data directory next to the executable, falling back
// to ./data when the executable path cannot be determined.
func DefaultDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "data"
	}
	return filepath.Join(filepath.Dir(exe), "data")
}
