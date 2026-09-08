package ffmpeg

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Encoder describes an H.264 encoder this machine can actually use.
type Encoder struct {
	// Name is the ffmpeg encoder name, e.g. "h264_nvenc".
	Name string
	// Kind is the short accelerator identifier used in config: nvenc, qsv, amf,
	// videotoolbox or cpu.
	Kind string
	// Label is what the settings UI shows.
	Label string
	// Hardware is false only for libx264.
	Hardware bool
	// HWAccelArgs are the decoder-side flags that let the GPU handle decoding too.
	HWAccelArgs []string
}

// cpuEncoder is the always-available fallback.
var cpuEncoder = Encoder{
	Name:     "libx264",
	Kind:     "cpu",
	Label:    "CPU (libx264)",
	Hardware: false,
}

// candidates are probed in preference order. NVENC first because it is both the most
// common discrete GPU and the fastest of these; QSV next as it is present on most
// Intel desktop chips; AMF last because its ffmpeg support is the least reliable.
var candidates = []Encoder{
	{
		Name: "h264_nvenc", Kind: "nvenc", Label: "NVIDIA NVENC", Hardware: true,
		HWAccelArgs: []string{"-hwaccel", "cuda"},
	},
	{
		Name: "h264_qsv", Kind: "qsv", Label: "Intel Quick Sync", Hardware: true,
		HWAccelArgs: []string{"-hwaccel", "qsv"},
	},
	{
		Name: "h264_amf", Kind: "amf", Label: "AMD AMF", Hardware: true,
		HWAccelArgs: []string{"-hwaccel", "d3d11va"},
	},
	{
		Name: "h264_videotoolbox", Kind: "videotoolbox", Label: "Apple VideoToolbox", Hardware: true,
		HWAccelArgs: []string{"-hwaccel", "videotoolbox"},
	},
}

// smokeTestTimeout bounds each trial encode. A broken driver can make ffmpeg hang
// rather than fail, and that must not stall startup.
const smokeTestTimeout = 20 * time.Second

// Capabilities is the result of probing this machine's encoders.
type Capabilities struct {
	// Available lists every encoder that passed the trial encode, best first.
	Available []Encoder
	// Selected is the encoder that will actually be used.
	Selected Encoder
}

var (
	capsOnce sync.Once
	capsVal  Capabilities
)

// DetectEncoders works out which H.264 encoders this machine can really use.
//
// Being listed by `ffmpeg -encoders` is not enough: ffmpeg ships with NVENC support
// compiled in regardless of whether an NVIDIA card is present, and a stale driver
// makes the encoder fail only at the moment you try to use it. So each candidate gets
// a one-frame trial encode, and only the ones that survive are offered.
//
// preference is the configured override: "auto" probes everything and picks the best,
// anything else forces that specific accelerator (falling back to CPU if it fails).
func (t *Tools) DetectEncoders(preference string) Capabilities {
	capsOnce.Do(func() { capsVal = t.detectEncoders() })

	caps := capsVal
	caps.Selected = cpuEncoder

	switch strings.ToLower(strings.TrimSpace(preference)) {
	case "", "auto":
		if len(caps.Available) > 0 {
			caps.Selected = caps.Available[0]
		}
	case "cpu", "none", "software":
		caps.Selected = cpuEncoder
	default:
		for _, e := range caps.Available {
			if e.Kind == preference {
				caps.Selected = e
				return caps
			}
		}
		log.Printf("ffmpeg: requested encoder %q is not usable on this machine, falling back to CPU", preference)
	}
	return caps
}

func (t *Tools) detectEncoders() Capabilities {
	listed := t.listedEncoders()

	// A real encoded clip to decode during the second stage of the test. Synthetic
	// lavfi input produces software frames, which would never exercise the hardware
	// decode path and so would never catch a broken one.
	sample, cleanup := t.makeSampleClip()
	defer cleanup()

	var caps Capabilities
	for _, c := range candidates {
		if !listed[c.Name] {
			continue
		}

		// Stage 1: can this encoder produce a frame at all?
		if !t.smokeTest(c.Name) {
			log.Printf("ffmpeg: %s is compiled in but not usable on this machine", c.Name)
			continue
		}

		// Stage 2: does hardware decoding work alongside it? Pairing a hardware
		// decoder with a hardware encoder requires both to land on the same adapter,
		// and ffmpeg's default choice frequently does not. When this fails the encoder
		// is still perfectly good on its own, so drop only the decode flags.
		if len(c.HWAccelArgs) > 0 {
			if sample == "" || !t.hwDecodeTest(c, sample) {
				log.Printf("ffmpeg: %s works, but hardware decoding does not; using it for encoding only", c.Label)
				c.HWAccelArgs = nil
			}
		}

		log.Printf("ffmpeg: %s is available", c.Label)
		caps.Available = append(caps.Available, c)
	}
	return caps
}

// makeSampleClip encodes a short H.264 file for the hardware-decode test. It returns
// an empty path if the clip could not be produced, in which case that test is skipped.
func (t *Tools) makeSampleClip() (string, func()) {
	f, err := os.CreateTemp("", "mediaserver-probe-*.mp4")
	if err != nil {
		return "", func() {}
	}
	path := f.Name()
	f.Close()

	cleanup := func() { os.Remove(path) }

	ctx, cancel := context.WithTimeout(context.Background(), smokeTestTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, t.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=1:size=640x480:rate=15",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		path,
	)
	hideWindow(cmd)

	if err := cmd.Run(); err != nil {
		cleanup()
		return "", func() {}
	}
	return path, cleanup
}

// hwDecodeTest runs the full pipeline the transcoder actually uses: hardware decode,
// a software scale filter, then hardware encode.
func (t *Tools) hwDecodeTest(enc Encoder, sample string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), smokeTestTimeout)
	defer cancel()

	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, enc.HWAccelArgs...)
	args = append(args,
		"-i", sample,
		"-vf", ScaleFilter(enc, 320, 240),
		"-frames:v", "1",
		"-c:v", enc.Name,
		"-f", "null", "-",
	)

	cmd := exec.CommandContext(ctx, t.FFmpeg, args...)
	hideWindow(cmd)

	return cmd.Run() == nil && ctx.Err() == nil
}

// listedEncoders parses `ffmpeg -encoders` into a set of names.
func (t *Tools) listedEncoders() map[string]bool {
	out := make(map[string]bool)

	cmd := exec.Command(t.FFmpeg, "-hide_banner", "-loglevel", "error", "-encoders")
	hideWindow(cmd)

	data, err := cmd.Output()
	if err != nil {
		log.Printf("ffmpeg: could not list encoders: %v", err)
		return out
	}

	for _, line := range strings.Split(string(data), "\n") {
		// Lines look like " V....D h264_nvenc  NVIDIA NVENC H.264 encoder".
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out[fields[1]] = true
	}
	return out
}

// smokeTest encodes a single synthetic frame to /dev/null with the given encoder.
func (t *Tools) smokeTest(encoder string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), smokeTestTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, t.FFmpeg,
		"-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=320x240:d=0.1:r=10",
		"-frames:v", "1",
		"-c:v", encoder,
		"-f", "null", "-",
	)
	hideWindow(cmd)

	err := cmd.Run()
	return err == nil && ctx.Err() == nil
}

// EncoderOptions returns the encoder-specific quality flags for a target bitrate.
//
// Each hardware encoder spells its rate control differently, and getting this wrong
// is the difference between a smooth stream and one that stutters or looks terrible.
func EncoderOptions(enc Encoder, bitrateBps, maxrateBps, bufsizeBps int) []string {
	kbps := func(bps int) string { return itoa(bps/1000) + "k" }

	common := []string{
		"-b:v", kbps(bitrateBps),
		"-maxrate", kbps(maxrateBps),
		"-bufsize", kbps(bufsizeBps),
	}

	switch enc.Kind {
	case "nvenc":
		return append([]string{
			"-preset", "p4", // balanced quality/speed on the modern NVENC preset scale
			"-tune", "hq",
			"-rc", "vbr",
			"-rc-lookahead", "20",
		}, common...)

	case "qsv":
		return append([]string{
			"-preset", "veryfast",
			"-look_ahead", "0",
		}, common...)

	case "amf":
		return append([]string{
			"-quality", "balanced",
			"-rc", "vbr_peak",
		}, common...)

	case "videotoolbox":
		return append([]string{
			"-realtime", "0",
		}, common...)

	default: // libx264
		return append([]string{
			// veryfast is the sweet spot for live transcoding: slower presets cannot
			// keep ahead of playback on a CPU that is also serving the rest of the app.
			"-preset", "veryfast",
			"-profile:v", "high",
			"-level", "4.1",
			"-sc_threshold", "0", // no extra keyframes: segments must stay aligned
		}, common...)
	}
}

// ScaleFilter builds the video filter that resizes to the target height while
// preserving aspect ratio and keeping both dimensions even (H.264 requires it).
func ScaleFilter(enc Encoder, width, height int) string {
	switch enc.Kind {
	case "qsv":
		return "scale_qsv=w=" + itoa(width) + ":h=" + itoa(height)
	default:
		// force_original_aspect_ratio guards against a source with odd dimensions
		// producing a non-conforming stream.
		return "scale=" + itoa(width) + ":" + itoa(height) +
			":force_original_aspect_ratio=decrease,pad=" +
			itoa(width) + ":" + itoa(height) + ":(ow-iw)/2:(oh-ih)/2"
	}
}

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
