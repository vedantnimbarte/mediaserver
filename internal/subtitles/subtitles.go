// Package subtitles discovers, extracts and converts subtitle tracks.
//
// Everything the player receives is WebVTT, because that is the only format a
// <track> element understands. Sidecar .srt/.ass files are converted on the fly, and
// text tracks embedded in the container are pulled out with ffmpeg and cached.
//
// Image-based tracks (PGS, VOBSUB) cannot become text at all; those are reported to
// the client as burn-in only, which restarts the transcode with the subtitles
// rendered into the picture.
package subtitles

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"kino/internal/ffmpeg"
	"kino/internal/models"
)

// Source describes where a subtitle track came from.
type Source string

const (
	// SourceSidecar is a separate file next to the media.
	SourceSidecar Source = "sidecar"
	// SourceEmbedded is a text track inside the container.
	SourceEmbedded Source = "embedded"
)

// Track is one selectable subtitle option.
type Track struct {
	// ID is stable for a given media file and identifies the track in URLs.
	ID       string `json:"id"`
	Label    string `json:"label"`
	Language string `json:"language,omitempty"`
	Code     string `json:"code,omitempty"`
	Source   Source `json:"source"`
	Default  bool   `json:"default,omitempty"`
	Forced   bool   `json:"forced,omitempty"`

	// Path is set for sidecar tracks.
	Path string `json:"-"`
	// StreamIndex is the -map 0:s:N ordinal for embedded tracks.
	StreamIndex int `json:"-"`

	// BurnInOnly marks image-based tracks that must be rendered into the video.
	BurnInOnly bool `json:"burnInOnly,omitempty"`
}

// imageSubtitleCodecs cannot be converted to text; they are bitmaps.
var imageSubtitleCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true,
	"pgssub":            true,
	"dvd_subtitle":      true,
	"dvdsub":            true,
	"xsub":              true,
	"dvb_subtitle":      true,
}

// sidecarExts are the subtitle file formats we look for beside the media.
var sidecarExts = map[string]bool{
	".srt": true, ".vtt": true, ".ass": true, ".ssa": true, ".sub": true,
}

// Service finds and converts subtitles, caching converted results on disk.
type Service struct {
	tools    *ffmpeg.Tools
	cacheDir string
}

// NewService builds a subtitle service. tools may be nil, in which case embedded
// track extraction is unavailable but sidecars still work.
func NewService(tools *ffmpeg.Tools, cacheDir string) *Service {
	return &Service{tools: tools, cacheDir: cacheDir}
}

// Tracks lists every subtitle available for a media file: sidecars found on disk
// plus whatever is embedded in the container.
func (s *Service) Tracks(media models.MediaInfo) []Track {
	var tracks []Track
	tracks = append(tracks, s.sidecarTracks(media.Path)...)
	tracks = append(tracks, embeddedTracks(media)...)

	// Stable, useful ordering: forced first (they carry translated signage the viewer
	// usually wants), then default, then by language name.
	sort.SliceStable(tracks, func(i, j int) bool {
		if tracks[i].Forced != tracks[j].Forced {
			return tracks[i].Forced
		}
		if tracks[i].Default != tracks[j].Default {
			return tracks[i].Default
		}
		return tracks[i].Label < tracks[j].Label
	})

	return tracks
}

// sidecarTracks finds subtitle files beside the media and in a Subs/ subfolder.
func (s *Service) sidecarTracks(mediaPath string) []Track {
	dir := filepath.Dir(mediaPath)
	stem := strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath))

	searchDirs := []string{
		dir,
		filepath.Join(dir, "Subs"),
		filepath.Join(dir, "Subtitles"),
		filepath.Join(dir, stem), // a folder named after the film, a common rip layout
	}

	var tracks []Track
	seen := make(map[string]bool)

	for _, searchDir := range searchDirs {
		entries, err := os.ReadDir(searchDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if !sidecarExts[ext] {
				continue
			}

			subStem := strings.TrimSuffix(name, filepath.Ext(name))

			// In the media's own folder the file must be named after it, or unrelated
			// subtitles from a neighbouring film would be offered. Inside a dedicated
			// Subs folder every file belongs to this media.
			dedicated := searchDir != dir
			if !dedicated && !strings.HasPrefix(strings.ToLower(subStem), strings.ToLower(stem)) {
				continue
			}

			path := filepath.Join(searchDir, name)
			if seen[strings.ToLower(path)] {
				continue
			}
			seen[strings.ToLower(path)] = true

			code, forced := parseSidecarSuffix(subStem, stem)
			label := models.LanguageName(code)
			if label == "" {
				label = subStem
				if dedicated {
					label = strings.TrimSuffix(name, filepath.Ext(name))
				}
			}
			if forced {
				label += " (Forced)"
			}

			tracks = append(tracks, Track{
				ID:       "sc_" + shortHash(path),
				Label:    label,
				Language: models.LanguageName(code),
				Code:     code,
				Source:   SourceSidecar,
				Forced:   forced,
				Path:     path,
			})
		}
	}

	return tracks
}

// parseSidecarSuffix pulls the language and forced flag out of a subtitle filename
// such as "Movie.en.forced.srt".
func parseSidecarSuffix(subStem, mediaStem string) (code string, forced bool) {
	suffix := subStem
	if len(subStem) > len(mediaStem) && strings.EqualFold(subStem[:len(mediaStem)], mediaStem) {
		suffix = subStem[len(mediaStem):]
	}

	for _, part := range strings.FieldsFunc(suffix, func(r rune) bool {
		return r == '.' || r == '_' || r == '-' || r == ' '
	}) {
		lower := strings.ToLower(part)
		switch {
		case lower == "forced":
			forced = true
		case lower == "sdh" || lower == "cc" || lower == "hi":
			// Hearing-impaired marker; not a language.
		case code == "" && models.IsLanguageCode(lower):
			code = lower
		}
	}
	return code, forced
}

// embeddedTracks converts the container's subtitle streams into selectable tracks.
func embeddedTracks(media models.MediaInfo) []Track {
	var tracks []Track
	for _, stream := range media.SubtitleStreams() {
		isImage := imageSubtitleCodecs[stream.Codec]

		label := stream.DisplayName()
		if isImage {
			label += " [burn-in]"
		}

		tracks = append(tracks, Track{
			ID:          fmt.Sprintf("em_%d", stream.TypeIndex),
			Label:       label,
			Language:    models.LanguageName(stream.Language),
			Code:        stream.Language,
			Source:      SourceEmbedded,
			Default:     stream.Default,
			Forced:      stream.Forced,
			StreamIndex: stream.TypeIndex,
			BurnInOnly:  isImage,
		})
	}
	return tracks
}

// FindTrack looks up a track by ID for a given media file.
func (s *Service) FindTrack(media models.MediaInfo, id string) (Track, bool) {
	for _, t := range s.Tracks(media) {
		if t.ID == id {
			return t, true
		}
	}
	return Track{}, false
}

// WebVTT returns the path to a WebVTT rendering of the track, converting and caching
// it on first request.
func (s *Service) WebVTT(ctx context.Context, media models.MediaInfo, track Track) (string, error) {
	if track.BurnInOnly {
		return "", fmt.Errorf("this subtitle track is image-based and must be burned into the video")
	}

	// A sidecar that is already WebVTT can be served directly.
	if track.Source == SourceSidecar && strings.EqualFold(filepath.Ext(track.Path), ".vtt") {
		return track.Path, nil
	}

	if err := os.MkdirAll(s.cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create subtitle cache: %w", err)
	}

	// Key the cache on the media path, the track and the file's modification time, so
	// replacing a subtitle file invalidates the cached conversion.
	cacheKey := shortHash(media.Path + "|" + track.ID + "|" + sourceStamp(track))
	cached := filepath.Join(s.cacheDir, cacheKey+".vtt")
	if info, err := os.Stat(cached); err == nil && info.Size() > 0 {
		return cached, nil
	}

	if s.tools == nil {
		return "", fmt.Errorf("ffmpeg is required to convert subtitles")
	}

	if err := s.convert(ctx, media, track, cached); err != nil {
		os.Remove(cached)
		return "", err
	}
	return cached, nil
}

// sourceStamp captures the mtime of a sidecar so edits bust the cache.
func sourceStamp(track Track) string {
	if track.Source != SourceSidecar {
		return ""
	}
	info, err := os.Stat(track.Path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())
}

// convert runs ffmpeg to produce WebVTT.
func (s *Service) convert(ctx context.Context, media models.MediaInfo, track Track, dest string) error {
	// Write to a temp file and rename, so a concurrent request never reads a
	// half-written subtitle file.
	tmp := dest + ".tmp"

	var args []string
	switch track.Source {
	case SourceSidecar:
		args = []string{
			"-hide_banner", "-loglevel", "error", "-y",
			// Assume UTF-8 but do not fail on stray bytes; subtitle files in the wild
			// are frequently mislabelled or mixed-encoding.
			"-sub_charenc_mode", "ignore",
			"-i", track.Path,
			"-map", "0:s:0",
			"-f", "webvtt",
			tmp,
		}
	case SourceEmbedded:
		args = []string{
			"-hide_banner", "-loglevel", "error", "-y",
			"-i", media.Path,
			"-map", fmt.Sprintf("0:s:%d", track.StreamIndex),
			"-f", "webvtt",
			tmp,
		}
	default:
		return fmt.Errorf("unknown subtitle source %q", track.Source)
	}

	cmd := execCommand(ctx, s.tools.FFmpeg, args...)
	ffmpeg.PrepareCommand(cmd)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("convert subtitles: %s", detail)
	}

	if info, err := os.Stat(tmp); err != nil || info.Size() == 0 {
		os.Remove(tmp)
		return fmt.Errorf("the subtitle track converted to an empty file")
	}

	return os.Rename(tmp, dest)
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(strings.ToLower(s)))
	return hex.EncodeToString(sum[:10])
}
