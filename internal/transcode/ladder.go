// Package transcode implements on-demand adaptive-bitrate HLS.
//
// The design is segment-pull rather than stream-push: Go generates the playlists up
// front from the known duration, and an ffmpeg worker is started only when a client
// actually requests a segment. That is what makes seeking work — a seek to the middle
// of a two-hour film restarts the encoder at that point instead of waiting for it to
// grind through everything before it — and it means the variants nobody watches cost
// nothing at all.
package transcode

import (
	"fmt"
	"strings"
)

// Rendition is one rung of the adaptive ladder.
type Rendition struct {
	Name         string `json:"name"` // "1080p", used in URLs
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	VideoBitrate int    `json:"videoBitrate"` // bits per second
	AudioBitrate int    `json:"audioBitrate"`
}

// MaxBitrate is the ceiling the encoder is allowed to peak to, and the value
// advertised to the player for bandwidth estimation.
func (r Rendition) MaxBitrate() int { return r.VideoBitrate * 3 / 2 }

// BufSize is the rate-control buffer, conventionally twice the target bitrate.
func (r Rendition) BufSize() int { return r.VideoBitrate * 2 }

// TotalBitrate is video plus audio, which is what BANDWIDTH in the master playlist
// is meant to describe.
func (r Rendition) TotalBitrate() int { return r.VideoBitrate + r.AudioBitrate }

// tiers is the standard ladder, richest first.
var tiers = []Rendition{
	{Name: "2160p", Height: 2160, VideoBitrate: 16_000_000, AudioBitrate: 192_000},
	{Name: "1080p", Height: 1080, VideoBitrate: 8_000_000, AudioBitrate: 192_000},
	{Name: "720p", Height: 720, VideoBitrate: 4_000_000, AudioBitrate: 128_000},
	{Name: "480p", Height: 480, VideoBitrate: 2_000_000, AudioBitrate: 128_000},
	{Name: "360p", Height: 360, VideoBitrate: 1_000_000, AudioBitrate: 96_000},
}

// minVideoBitrate is the floor for any rendition. Below this even a small frame
// looks like a mess of blocking artefacts, so there is no point going lower.
const minVideoBitrate = 200_000

// BuildLadder derives the renditions to offer for a given source.
//
// Three rules shape the result:
//
//   - Renditions above the source resolution are dropped. Upscaling burns CPU to
//     produce a bigger stream that looks no better.
//   - No rendition exceeds its standard tier budget, which is what stops a 40 Mbps
//     remux from being re-encoded at 40 Mbps.
//   - No rendition exceeds what the source bitrate justifies *at that resolution*,
//     scaled by pixel count. Clamping every rung to the raw source bitrate instead
//     would give a low-bitrate file a ladder whose rungs all share one bitrate,
//     leaving the player nothing to adapt between.
func BuildLadder(srcWidth, srcHeight int, srcBitrate int64) []Rendition {
	if srcHeight <= 0 || srcWidth <= 0 {
		// Audio-only, or a probe that could not read the dimensions. One safe rung.
		return []Rendition{{Name: "audio", Width: 0, Height: 0, VideoBitrate: 0, AudioBitrate: 192_000}}
	}

	aspect := float64(srcWidth) / float64(srcHeight)
	srcPixels := float64(srcWidth) * float64(srcHeight)

	var ladder []Rendition
	for _, tier := range tiers {
		if tier.Height > srcHeight {
			continue // never upscale
		}

		r := tier
		r.Height = evenInt(tier.Height)
		r.Width = evenInt(int(float64(tier.Height)*aspect + 0.5))

		if srcBitrate > 0 {
			// Bitrate need scales with pixel count, so a rendition at a quarter of the
			// source's pixels needs roughly a quarter of its bitrate.
			pixelRatio := (float64(r.Width) * float64(r.Height)) / srcPixels
			if pixelRatio > 1 {
				pixelRatio = 1
			}
			budget := int(float64(srcBitrate) * pixelRatio)
			if budget < minVideoBitrate {
				budget = minVideoBitrate
			}
			if budget < r.VideoBitrate {
				r.VideoBitrate = budget
			}
		}

		// A rung that saves no bitrate over the one above it is dead weight: the
		// player would never gain anything by switching down to it, and encoding it
		// would cost a whole extra ffmpeg process. This happens when several low
		// tiers all bottom out at minVideoBitrate.
		if len(ladder) > 0 && r.VideoBitrate >= ladder[len(ladder)-1].VideoBitrate {
			continue
		}

		ladder = append(ladder, r)
	}

	// A source smaller than the lowest tier still needs one rung to play at all.
	if len(ladder) == 0 {
		bitrate := 1_000_000
		if srcBitrate > 0 && srcBitrate < int64(bitrate) {
			bitrate = int(srcBitrate)
		}
		ladder = append(ladder, Rendition{
			Name:         fmt.Sprintf("%dp", evenInt(srcHeight)),
			Width:        evenInt(srcWidth),
			Height:       evenInt(srcHeight),
			VideoBitrate: bitrate,
			AudioBitrate: 128_000,
		})
	}

	return ladder
}

// evenInt rounds up to the nearest even number. H.264 with 4:2:0 chroma cannot encode
// odd dimensions, and ffmpeg fails outright rather than rounding for you.
func evenInt(n int) int {
	if n < 2 {
		return 2
	}
	if n%2 != 0 {
		return n + 1
	}
	return n
}

// ApplyOverrides replaces ladder bitrates with configured values.
//
// The overrides are applied after the ladder is derived, so a user tuning one rung
// does not have to restate the whole ladder, and the no-upscale rule still holds.
func ApplyOverrides(ladder []Rendition, overrides map[string]int) []Rendition {
	if len(overrides) == 0 {
		return ladder
	}
	out := make([]Rendition, len(ladder))
	copy(out, ladder)

	for i := range out {
		if bitrate, ok := overrides[out[i].Name]; ok && bitrate >= minVideoBitrate {
			out[i].VideoBitrate = bitrate
		}
	}
	return out
}

// FindRendition looks up a rendition by name.
func FindRendition(ladder []Rendition, name string) (Rendition, bool) {
	for _, r := range ladder {
		if r.Name == name {
			return r, true
		}
	}
	return Rendition{}, false
}

// codecsAttribute builds the RFC 6381 codec string for the master playlist.
//
// Players use this to decide, before downloading anything, whether they can decode a
// variant. Getting it wrong means either a silently skipped variant or a black screen.
func codecsAttribute(r Rendition) string {
	// avc1.<profile><constraints><level>. We always encode High profile, and the
	// level follows from the resolution.
	var level string
	switch {
	case r.Height >= 2160:
		level = "33" // 5.1
	case r.Height >= 1080:
		level = "28" // 4.0
	case r.Height >= 720:
		level = "1f" // 3.1
	default:
		level = "1e" // 3.0
	}

	codecs := []string{"avc1.6400" + level}
	if r.AudioBitrate > 0 {
		codecs = append(codecs, "mp4a.40.2") // AAC-LC
	}
	return strings.Join(codecs, ",")
}
