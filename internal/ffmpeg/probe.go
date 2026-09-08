package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"kino/internal/models"
)

// probeTimeout bounds a single ffprobe run. A damaged file can otherwise make ffprobe
// sit and spin, which would stall a whole library scan.
const probeTimeout = 30 * time.Second

// rawProbe mirrors the subset of `ffprobe -print_format json` that we consume.
type rawProbe struct {
	Format struct {
		Filename       string            `json:"filename"`
		FormatName     string            `json:"format_name"`
		FormatLongName string            `json:"format_long_name"`
		Duration       string            `json:"duration"`
		Size           string            `json:"size"`
		BitRate        string            `json:"bit_rate"`
		Tags           map[string]string `json:"tags"`
	} `json:"format"`
	Streams []rawStream `json:"streams"`
}

type rawStream struct {
	Index         int               `json:"index"`
	CodecName     string            `json:"codec_name"`
	CodecLongName string            `json:"codec_long_name"`
	CodecType     string            `json:"codec_type"`
	Profile       string            `json:"profile"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	PixFmt        string            `json:"pix_fmt"`
	RFrameRate    string            `json:"r_frame_rate"`
	AvgFrameRate  string            `json:"avg_frame_rate"`
	Channels      int               `json:"channels"`
	ChannelLayout string            `json:"channel_layout"`
	SampleRate    string            `json:"sample_rate"`
	BitRate       string            `json:"bit_rate"`
	Duration      string            `json:"duration"`
	Disposition   map[string]int    `json:"disposition"`
	Tags          map[string]string `json:"tags"`
}

// Probe runs ffprobe against a file and returns a populated MediaInfo.
//
// A probe failure is recorded on the returned MediaInfo rather than returned as a hard
// error for most callers: one unreadable file should downgrade to "unplayable" in the
// UI, not abort the scan around it.
func (t *Tools) Probe(ctx context.Context, path string) (models.MediaInfo, error) {
	info := models.MediaInfo{Path: path}

	stat, err := os.Stat(path)
	if err != nil {
		info.ProbeError = "file is not accessible"
		return info, fmt.Errorf("stat %s: %w", path, err)
	}
	info.Size = stat.Size()
	info.ModTime = stat.ModTime()
	info.Available = true

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, t.FFprobe,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	hideWindow(cmd)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			msg = "ffprobe timed out"
		}
		if msg == "" {
			msg = err.Error()
		}
		info.ProbeError = msg
		return info, fmt.Errorf("ffprobe %s: %s", path, msg)
	}

	var raw rawProbe
	if err := json.Unmarshal(out, &raw); err != nil {
		info.ProbeError = "ffprobe returned unreadable output"
		return info, fmt.Errorf("parse ffprobe output for %s: %w", path, err)
	}

	info.Container = raw.Format.FormatName
	info.DurationSec = parseFloat(raw.Format.Duration)
	info.BitRate = parseInt(raw.Format.BitRate)
	info.Streams = convertStreams(raw.Streams)

	// Some containers (notably MPEG-TS and a few malformed MKVs) omit the format-level
	// duration. Fall back to the longest stream duration before giving up, otherwise
	// seeking and the HLS playlist length would both be wrong.
	if info.DurationSec == 0 {
		for _, s := range raw.Streams {
			if d := parseFloat(s.Duration); d > info.DurationSec {
				info.DurationSec = d
			}
		}
	}

	info.Probed = true
	info.ProbeError = ""
	return info, nil
}

func convertStreams(raw []rawStream) []models.Stream {
	var out []models.Stream
	// ffmpeg's -map 0:<kind>:<n> syntax indexes within a kind, so we have to compute
	// that ordinal ourselves; the container-wide Index is not what -map wants.
	counters := map[models.StreamKind]int{}

	for _, rs := range raw {
		kind := streamKind(rs.CodecType)
		if kind == "" {
			continue // attachments, data streams: nothing we can play or extract
		}

		s := models.Stream{
			Index:     rs.Index,
			Kind:      kind,
			TypeIndex: counters[kind],
			Codec:     rs.CodecName,
			CodecLong: rs.CodecLongName,
			Profile:   rs.Profile,
			BitRate:   parseInt(rs.BitRate),
			Language:  strings.TrimSpace(rs.Tags["language"]),
			Title:     strings.TrimSpace(rs.Tags["title"]),
			Default:   rs.Disposition["default"] == 1,
			Forced:    rs.Disposition["forced"] == 1,
		}
		counters[kind]++

		switch kind {
		case models.StreamVideo:
			s.Width = rs.Width
			s.Height = rs.Height
			s.PixFmt = rs.PixFmt
			s.FrameRate = parseFrameRate(rs.AvgFrameRate)
			if s.FrameRate == 0 {
				s.FrameRate = parseFrameRate(rs.RFrameRate)
			}
		case models.StreamAudio:
			s.Channels = rs.Channels
			s.ChannelLayout = rs.ChannelLayout
			s.SampleRate = int(parseInt(rs.SampleRate))
		}

		out = append(out, s)
	}
	return out
}

func streamKind(codecType string) models.StreamKind {
	switch codecType {
	case "video":
		return models.StreamVideo
	case "audio":
		return models.StreamAudio
	case "subtitle":
		return models.StreamSubtitle
	}
	return ""
}

func parseFloat(s string) float64 {
	if s == "" || s == "N/A" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}

func parseInt(s string) int64 {
	if s == "" || s == "N/A" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// parseFrameRate converts ffprobe's "30000/1001" rational form to a float.
func parseFrameRate(s string) float64 {
	num, den, found := strings.Cut(s, "/")
	if !found {
		return parseFloat(s)
	}
	n, d := parseFloat(num), parseFloat(den)
	if d == 0 {
		return 0
	}
	return n / d
}
