// Package models defines every entity persisted by the server.
package models

import (
	"path/filepath"
	"strings"
	"time"
)

// LibraryType distinguishes the four kinds of library the scanner understands.
type LibraryType string

const (
	LibraryMovie LibraryType = "movie"
	LibraryShow  LibraryType = "show"
	LibraryMusic LibraryType = "music"
	LibraryPhoto LibraryType = "photo"
)

// Valid reports whether t is one of the known library types.
func (t LibraryType) Valid() bool {
	switch t {
	case LibraryMovie, LibraryShow, LibraryMusic, LibraryPhoto:
		return true
	}
	return false
}

// Library is a named set of folders scanned as a single collection.
type Library struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Type       LibraryType `json:"type"`
	Paths      []string    `json:"paths"`
	ItemCount  int         `json:"itemCount"`
	CreatedAt  time.Time   `json:"createdAt"`
	LastScanAt time.Time   `json:"lastScanAt,omitempty"`
	Scanning   bool        `json:"scanning"`
}

func (l Library) EntityID() string { return l.ID }

// StreamKind is the type of an elementary stream inside a container.
type StreamKind string

const (
	StreamVideo    StreamKind = "video"
	StreamAudio    StreamKind = "audio"
	StreamSubtitle StreamKind = "subtitle"
)

// Stream is one elementary stream as reported by ffprobe.
type Stream struct {
	// Index is the absolute stream index within the container (ffprobe's stream index).
	Index int        `json:"index"`
	Kind  StreamKind `json:"kind"`
	// TypeIndex is the index among streams of the same kind, i.e. the N in ffmpeg's
	// `-map 0:a:N`. This is what the transcoder and subtitle extractor actually use.
	TypeIndex int    `json:"typeIndex"`
	Codec     string `json:"codec"`
	CodecLong string `json:"codecLong,omitempty"`
	Profile   string `json:"profile,omitempty"`
	Language  string `json:"language,omitempty"`
	Title     string `json:"title,omitempty"`
	Default   bool   `json:"default,omitempty"`
	Forced    bool   `json:"forced,omitempty"`
	BitRate   int64  `json:"bitRate,omitempty"`

	// Video-only
	Width     int     `json:"width,omitempty"`
	Height    int     `json:"height,omitempty"`
	FrameRate float64 `json:"frameRate,omitempty"`
	PixFmt    string  `json:"pixFmt,omitempty"`

	// Audio-only
	Channels      int    `json:"channels,omitempty"`
	ChannelLayout string `json:"channelLayout,omitempty"`
	SampleRate    int    `json:"sampleRate,omitempty"`
}

// DisplayName produces a human label for a track picker, e.g. "English (AC3 5.1)".
func (s Stream) DisplayName() string {
	var parts []string
	if s.Title != "" {
		parts = append(parts, s.Title)
	} else if lang := LanguageName(s.Language); lang != "" {
		parts = append(parts, lang)
	} else {
		parts = append(parts, "Track "+itoa(s.TypeIndex+1))
	}

	var detail []string
	if s.Codec != "" {
		detail = append(detail, strings.ToUpper(s.Codec))
	}
	if s.ChannelLayout != "" {
		detail = append(detail, s.ChannelLayout)
	} else if s.Channels > 0 {
		detail = append(detail, itoa(s.Channels)+"ch")
	}
	if s.Forced {
		detail = append(detail, "Forced")
	}
	if len(detail) > 0 {
		parts = append(parts, "("+strings.Join(detail, " ")+")")
	}
	return strings.Join(parts, " ")
}

// MediaInfo is the physical file behind a playable item, plus everything ffprobe
// told us about it. It is embedded in Movie, Episode and Track.
type MediaInfo struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`

	Container   string   `json:"container,omitempty"`
	DurationSec float64  `json:"durationSec,omitempty"`
	BitRate     int64    `json:"bitRate,omitempty"`
	Streams     []Stream `json:"streams,omitempty"`

	// Probed is false until ffprobe has successfully run against this file.
	Probed     bool   `json:"probed"`
	ProbeError string `json:"probeError,omitempty"`

	// Available is false when the file has disappeared from disk. The record is kept
	// so that watch history survives an unplugged drive.
	Available bool `json:"available"`
}

// Unchanged reports whether the file on disk matches what was recorded, which lets an
// incremental scan skip re-probing.
func (m MediaInfo) Unchanged(size int64, modTime time.Time) bool {
	return m.Probed && m.Size == size && m.ModTime.Equal(modTime)
}

// Filename returns the base name of the media file.
func (m MediaInfo) Filename() string { return filepath.Base(m.Path) }

// VideoStream returns the primary video stream, or nil for audio-only media.
// Cover-art streams (mjpeg/png attachments) are skipped.
func (m MediaInfo) VideoStream() *Stream {
	for i := range m.Streams {
		s := &m.Streams[i]
		if s.Kind != StreamVideo {
			continue
		}
		switch s.Codec {
		case "mjpeg", "png", "bmp", "gif":
			continue // embedded cover art, not a real video track
		}
		return s
	}
	return nil
}

// StreamsOfKind returns every stream of the requested kind, in container order.
func (m MediaInfo) StreamsOfKind(kind StreamKind) []Stream {
	var out []Stream
	for _, s := range m.Streams {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

// AudioStreams returns all audio tracks.
func (m MediaInfo) AudioStreams() []Stream { return m.StreamsOfKind(StreamAudio) }

// SubtitleStreams returns all embedded subtitle tracks.
func (m MediaInfo) SubtitleStreams() []Stream { return m.StreamsOfKind(StreamSubtitle) }

// DefaultAudioIndex picks the audio track to start with: the one flagged default,
// otherwise the first. Returns -1 when there is no audio at all.
func (m MediaInfo) DefaultAudioIndex() int {
	audio := m.AudioStreams()
	if len(audio) == 0 {
		return -1
	}
	for _, s := range audio {
		if s.Default {
			return s.TypeIndex
		}
	}
	return audio[0].TypeIndex
}

// Resolution returns the video dimensions, or zeroes for audio-only media.
func (m MediaInfo) Resolution() (int, int) {
	if v := m.VideoStream(); v != nil {
		return v.Width, v.Height
	}
	return 0, 0
}

// CastMember is one credited performer.
type CastMember struct {
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
	ProfileID string `json:"profileId,omitempty"` // image cache key
	Order     int    `json:"order"`
}

// MetadataStatus tracks how far the metadata pipeline got with an item.
type MetadataStatus string

const (
	MetaPending MetadataStatus = "pending" // not yet looked up
	MetaMatched MetadataStatus = "matched" // enriched from TMDb
	MetaNoMatch MetadataStatus = "nomatch" // looked up, nothing found
	MetaError   MetadataStatus = "error"   // lookup failed (network, quota, bad key)
	MetaLocal   MetadataStatus = "local"   // filename + ffprobe only, no provider configured
)

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
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
