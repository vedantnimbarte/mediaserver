package transcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"kino/internal/ffmpeg"
	"kino/internal/models"
)

func TestBuildLadderNeverUpscales(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		wantNames     []string
	}{
		{"4K source", 3840, 2160, []string{"2160p", "1080p", "720p", "480p", "360p"}},
		{"1080p source", 1920, 1080, []string{"1080p", "720p", "480p", "360p"}},
		{"720p source", 1280, 720, []string{"720p", "480p", "360p"}},
		{"480p source", 854, 480, []string{"480p", "360p"}},
		{"360p source", 640, 360, []string{"360p"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ladder := BuildLadder(tc.width, tc.height, 0)
			if len(ladder) != len(tc.wantNames) {
				t.Fatalf("got %d renditions %v, want %d %v",
					len(ladder), names(ladder), len(tc.wantNames), tc.wantNames)
			}
			for i, want := range tc.wantNames {
				if ladder[i].Name != want {
					t.Errorf("rendition %d = %q, want %q", i, ladder[i].Name, want)
				}
				if ladder[i].Height > tc.height {
					t.Errorf("rendition %s upscales the source (%d > %d)", ladder[i].Name, ladder[i].Height, tc.height)
				}
			}
		})
	}
}

// TestBuildLadderTinySource covers the case where the source is smaller than every
// standard tier; it still needs one rung or it could not be played at all.
func TestBuildLadderTinySource(t *testing.T) {
	ladder := BuildLadder(320, 240, 0)
	if len(ladder) != 1 {
		t.Fatalf("got %d renditions %v, want 1", len(ladder), names(ladder))
	}
	if ladder[0].Height != 240 || ladder[0].Width != 320 {
		t.Errorf("got %dx%d, want 320x240", ladder[0].Width, ladder[0].Height)
	}
}

func TestBuildLadderClampsToSourceBitrate(t *testing.T) {
	// A 1080p file that is only 1.5 Mbps should never be re-encoded at 8 Mbps.
	ladder := BuildLadder(1920, 1080, 1_500_000)
	if ladder[0].Name != "1080p" {
		t.Fatalf("first rendition = %s, want 1080p", ladder[0].Name)
	}
	if ladder[0].VideoBitrate != 1_500_000 {
		t.Errorf("1080p bitrate = %d, want it clamped to the source's 1500000", ladder[0].VideoBitrate)
	}
}

// TestBuildLadderStaysAdaptive is the property that makes ABR worth having: the rungs
// must actually differ in bitrate. A naive "clamp everything to the source bitrate"
// rule collapses a low-bitrate source's ladder into several identical rungs, leaving
// the player nothing to switch between when bandwidth drops.
func TestBuildLadderStaysAdaptive(t *testing.T) {
	cases := []struct {
		name          string
		width, height int
		bitrate       int64
	}{
		{"low-bitrate 720p", 1280, 720, 400_000},
		{"typical 1080p", 1920, 1080, 5_000_000},
		{"high-bitrate remux", 1920, 1080, 40_000_000},
		{"unknown bitrate", 1920, 1080, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ladder := BuildLadder(tc.width, tc.height, tc.bitrate)
			if len(ladder) < 2 {
				t.Skip("only one rendition; nothing to compare")
			}
			for i := 1; i < len(ladder); i++ {
				prev, cur := ladder[i-1], ladder[i]
				if cur.VideoBitrate >= prev.VideoBitrate {
					t.Errorf("%s (%d bps) is not below %s (%d bps); the ladder offers no real choice",
						cur.Name, cur.VideoBitrate, prev.Name, prev.VideoBitrate)
				}
				if cur.VideoBitrate < minVideoBitrate {
					t.Errorf("%s is %d bps, below the %d floor", cur.Name, cur.VideoBitrate, minVideoBitrate)
				}
			}
		})
	}
}

// TestBuildLadderRespectsTierCeiling stops a huge remux being re-encoded at its
// original bitrate, which would defeat the point of transcoding for a phone.
func TestBuildLadderRespectsTierCeiling(t *testing.T) {
	ladder := BuildLadder(1920, 1080, 40_000_000)
	if ladder[0].VideoBitrate != 8_000_000 {
		t.Errorf("1080p bitrate = %d, want the 8000000 tier ceiling", ladder[0].VideoBitrate)
	}
}

func TestLadderDimensionsAreEven(t *testing.T) {
	// 1919x1079 is deliberately odd on both axes; H.264 4:2:0 cannot encode that.
	for _, r := range BuildLadder(1919, 1079, 0) {
		if r.Width%2 != 0 || r.Height%2 != 0 {
			t.Errorf("rendition %s is %dx%d; both dimensions must be even", r.Name, r.Width, r.Height)
		}
	}
}

func TestSegmentCountAndDuration(t *testing.T) {
	// 25 seconds at 4s per segment is 6 full segments plus a 1s tail.
	if got := SegmentCount(25, 4); got != 7 {
		t.Errorf("SegmentCount(25, 4) = %d, want 7", got)
	}
	if got := SegmentCount(24, 4); got != 6 {
		t.Errorf("SegmentCount(24, 4) = %d, want 6", got)
	}
	if got := SegmentDuration(0, 25, 4); got != 4 {
		t.Errorf("first segment = %.2f, want 4", got)
	}
	if got := SegmentDuration(6, 25, 4); got != 1 {
		t.Errorf("last segment = %.2f, want 1", got)
	}
}

func TestVariantPlaylistIsCompleteVOD(t *testing.T) {
	playlist := VariantPlaylist(25, 4, func(n int) string { return "seg" + strconv.Itoa(n) + ".ts" })

	for _, want := range []string{
		"#EXTM3U",
		"#EXT-X-PLAYLIST-TYPE:VOD",
		"#EXT-X-TARGETDURATION:4",
		"#EXT-X-ENDLIST",
		"seg0.ts",
		"seg6.ts",
	} {
		if !strings.Contains(playlist, want) {
			t.Errorf("playlist is missing %q:\n%s", want, playlist)
		}
	}

	// Every segment must be listed up front; that is what makes seeking work.
	if n := strings.Count(playlist, "#EXTINF:"); n != 7 {
		t.Errorf("playlist has %d EXTINF entries, want 7", n)
	}
}

func TestMasterPlaylistAdvertisesEveryVariant(t *testing.T) {
	ladder := BuildLadder(1920, 1080, 0)
	master := MasterPlaylist(ladder, func(r Rendition) string { return r.Name + "/index.m3u8" })

	if n := strings.Count(master, "#EXT-X-STREAM-INF:"); n != len(ladder) {
		t.Errorf("master lists %d variants, want %d", n, len(ladder))
	}
	for _, r := range ladder {
		if !strings.Contains(master, r.Name+"/index.m3u8") {
			t.Errorf("master is missing the %s variant:\n%s", r.Name, master)
		}
	}
	if !strings.Contains(master, "RESOLUTION=1920x1080") {
		t.Errorf("master is missing the 1080p resolution attribute:\n%s", master)
	}
	if !strings.Contains(master, `CODECS="avc1.640028,mp4a.40.2"`) {
		t.Errorf("master has the wrong codec attribute:\n%s", master)
	}
}

// ---- integration: real encoding ----

func testTools(t *testing.T) *ffmpeg.Tools {
	t.Helper()
	tools, err := ffmpeg.Locate("", "")
	if err != nil {
		t.Skipf("ffmpeg is not installed: %v", err)
	}
	return tools
}

// makeSource writes a real MKV that a browser could not play directly, so the
// transcode path is genuinely exercised.
func makeSource(t *testing.T, tools *ffmpeg.Tools, path string, seconds int) models.MediaInfo {
	t.Helper()

	cmd := exec.Command(tools.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration="+strconv.Itoa(seconds)+":size=640x480:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(seconds),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "50",
		"-c:a", "ac3", // AC3 cannot be played by browsers, forcing a transcode
		"-shortest",
		path,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate source: %v\n%s", err, out)
	}

	info, err := tools.Probe(context.Background(), path)
	if err != nil {
		t.Fatalf("probe source: %v", err)
	}
	return info
}

func newTestManager(t *testing.T, tools *ffmpeg.Tools) *Manager {
	t.Helper()
	m := NewManager(Options{
		Tools:          tools,
		RootDir:        filepath.Join(t.TempDir(), "transcode"),
		SegmentSeconds: 4,
		IdleSeconds:    60,
		MaxConcurrent:  2,
		// Force CPU: hardware encoders vary by machine and the point of these tests
		// is the segment pipeline, not the encoder.
		HWAccel: "cpu",
	})
	t.Cleanup(m.Close)
	return m
}

// TestEncodeFirstSegment is the core happy path: ask for segment 0 and get a real,
// playable transport stream back.
func TestEncodeFirstSegment(t *testing.T) {
	tools := testTools(t)
	m := newTestManager(t, tools)

	src := filepath.Join(t.TempDir(), "source.mkv")
	info := makeSource(t, tools, src, 20)

	session, err := m.Start(StartOptions{
		UserID: "u1", MediaID: "m1", Media: info, AudioIndex: -1, BurnSubtitleIndex: -1,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	variant := session.Ladder[0].Name

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	path, err := session.SegmentPath(ctx, variant, 0)
	if err != nil {
		t.Fatalf("encode segment 0: %v", err)
	}

	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("segment file: %v", err)
	}
	if stat.Size() == 0 {
		t.Fatal("segment file is empty")
	}

	// The segment must be a real decodable stream, not just bytes on disk.
	segInfo, err := tools.Probe(ctx, path)
	if err != nil {
		t.Fatalf("probe segment: %v", err)
	}
	if segInfo.VideoStream() == nil {
		t.Error("segment has no video stream")
	}
	if len(segInfo.AudioStreams()) == 0 {
		t.Error("segment has no audio stream")
	}
	if a := segInfo.AudioStreams(); len(a) > 0 && a[0].Codec != "aac" {
		t.Errorf("segment audio codec = %s, want aac (AC3 must be converted)", a[0].Codec)
	}
	if segInfo.DurationSec < 3 || segInfo.DurationSec > 5 {
		t.Errorf("segment duration = %.2fs, want roughly 4s", segInfo.DurationSec)
	}
}

// TestSeekStartsEncoderAtRequestedSegment is the property that separates this design
// from a naive sequential transcoder: requesting a segment deep into the file must
// not require encoding everything before it.
func TestSeekStartsEncoderAtRequestedSegment(t *testing.T) {
	tools := testTools(t)
	m := newTestManager(t, tools)

	src := filepath.Join(t.TempDir(), "source.mkv")
	info := makeSource(t, tools, src, 40) // 10 segments at 4s

	session, err := m.Start(StartOptions{
		UserID: "u1", MediaID: "m1", Media: info, AudioIndex: -1, BurnSubtitleIndex: -1,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	variant := session.Ladder[0].Name

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Jump straight to segment 8 without ever asking for 0..7.
	const seekTo = 8
	path, err := session.SegmentPath(ctx, variant, seekTo)
	if err != nil {
		t.Fatalf("encode segment %d after seek: %v", seekTo, err)
	}
	if !fileReady(path) {
		t.Fatalf("segment %d was not produced", seekTo)
	}

	// The earlier segments must NOT have been encoded; that is the whole point.
	for n := 0; n < seekTo; n++ {
		if fileReady(session.segmentFile(variant, n)) {
			t.Errorf("segment %d was encoded even though playback seeked past it", n)
		}
	}

	// Seeking backwards must also work, restarting the encoder at the new point.
	if _, err := session.SegmentPath(ctx, variant, 1); err != nil {
		t.Fatalf("encode segment 1 after seeking backwards: %v", err)
	}
	if !fileReady(session.segmentFile(variant, 1)) {
		t.Error("segment 1 was not produced after seeking backwards")
	}
}

// TestSequentialSegmentsReuseOneEncoder verifies that normal playback does not
// restart ffmpeg for every segment.
func TestSequentialSegmentsReuseOneEncoder(t *testing.T) {
	tools := testTools(t)
	m := newTestManager(t, tools)

	src := filepath.Join(t.TempDir(), "source.mkv")
	info := makeSource(t, tools, src, 20)

	session, err := m.Start(StartOptions{
		UserID: "u1", MediaID: "m1", Media: info, AudioIndex: -1, BurnSubtitleIndex: -1,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	variant := session.Ladder[0].Name

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	for n := 0; n < 3; n++ {
		if _, err := session.SegmentPath(ctx, variant, n); err != nil {
			t.Fatalf("segment %d: %v", n, err)
		}
	}

	session.mu.Lock()
	w := session.workers[variant]
	session.mu.Unlock()
	if w == nil {
		t.Fatal("no worker is running")
	}
	if w.startSeg != 0 {
		t.Errorf("worker restarted during sequential playback (startSeg = %d, want 0)", w.startSeg)
	}
}

func TestSegmentOutOfRangeIsRejected(t *testing.T) {
	tools := testTools(t)
	m := newTestManager(t, tools)

	src := filepath.Join(t.TempDir(), "source.mkv")
	info := makeSource(t, tools, src, 8) // 2 segments

	session, err := m.Start(StartOptions{
		UserID: "u1", MediaID: "m1", Media: info, AudioIndex: -1, BurnSubtitleIndex: -1,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	variant := session.Ladder[0].Name

	if _, err := session.SegmentPath(context.Background(), variant, 99); err == nil {
		t.Error("expected an error for a segment past the end of the file")
	}
	if _, err := session.SegmentPath(context.Background(), "nonexistent", 0); err == nil {
		t.Error("expected an error for an unknown rendition")
	}
}

// TestSessionCleanupRemovesEverything is the leak guard: closing a session must kill
// the encoder and delete its segments, or a few hours of browsing would fill the disk.
func TestSessionCleanupRemovesEverything(t *testing.T) {
	tools := testTools(t)
	m := newTestManager(t, tools)

	src := filepath.Join(t.TempDir(), "source.mkv")
	info := makeSource(t, tools, src, 20)

	session, err := m.Start(StartOptions{
		UserID: "u1", MediaID: "m1", Media: info, AudioIndex: -1, BurnSubtitleIndex: -1,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	dir := session.Dir
	variant := session.Ladder[0].Name

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := session.SegmentPath(ctx, variant, 0); err != nil {
		t.Fatalf("encode segment: %v", err)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("session directory should exist: %v", err)
	}

	if !m.Stop(session.ID) {
		t.Fatal("Stop reported no such session")
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("session directory still exists after Stop: %v", err)
	}
	if _, err := m.Get(session.ID); err == nil {
		t.Error("session is still resolvable after Stop")
	}
	// Requests against a dead session must fail cleanly rather than hang.
	if _, err := session.SegmentPath(context.Background(), variant, 5); err == nil {
		t.Error("expected an error requesting a segment from a closed session")
	}
}

func TestIdleSessionIsReaped(t *testing.T) {
	tools := testTools(t)

	m := NewManager(Options{
		Tools:          tools,
		RootDir:        filepath.Join(t.TempDir(), "transcode"),
		SegmentSeconds: 4,
		IdleSeconds:    10, // clamped to the 10s minimum by NewManager
		MaxConcurrent:  1,
		HWAccel:        "cpu",
	})
	t.Cleanup(m.Close)

	src := filepath.Join(t.TempDir(), "source.mkv")
	info := makeSource(t, tools, src, 8)

	session, err := m.Start(StartOptions{
		UserID: "u1", MediaID: "m1", Media: info, AudioIndex: -1, BurnSubtitleIndex: -1,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}

	// Backdate the session's last access so the collector sees it as abandoned.
	session.mu.Lock()
	session.lastAccess = time.Now().Add(-time.Hour)
	session.mu.Unlock()

	m.reapIdle()

	if _, err := m.Get(session.ID); err == nil {
		t.Error("an idle session should have been reaped")
	}
	if _, err := os.Stat(session.Dir); !os.IsNotExist(err) {
		t.Error("a reaped session should have had its directory removed")
	}
}

func TestAudioTrackSelection(t *testing.T) {
	tools := testTools(t)
	m := newTestManager(t, tools)

	// Two audio tracks: the second is a distinct tone so selection is observable.
	src := filepath.Join(t.TempDir(), "multi.mkv")
	cmd := exec.Command(tools.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=8",
		"-f", "lavfi", "-i", "sine=frequency=880:duration=8",
		"-map", "0:v", "-map", "1:a", "-map", "2:a",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest",
		"-metadata:s:a:0", "language=eng",
		"-metadata:s:a:1", "language=fra",
		src,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate multi-audio source: %v\n%s", err, out)
	}

	info, err := tools.Probe(context.Background(), src)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(info.AudioStreams()) != 2 {
		t.Fatalf("source has %d audio tracks, want 2", len(info.AudioStreams()))
	}
	if info.AudioStreams()[1].Language != "fra" {
		t.Errorf("second track language = %q, want fra", info.AudioStreams()[1].Language)
	}

	// Select the second track and confirm it encodes.
	session, err := m.Start(StartOptions{
		UserID: "u1", MediaID: "m1", Media: info, AudioIndex: 1, BurnSubtitleIndex: -1,
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	if session.AudioIndex != 1 {
		t.Errorf("session audio index = %d, want 1", session.AudioIndex)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := session.SegmentPath(ctx, session.Ladder[0].Name, 0); err != nil {
		t.Fatalf("encode with the second audio track: %v", err)
	}
}

func names(ladder []Rendition) []string {
	out := make([]string, 0, len(ladder))
	for _, r := range ladder {
		out = append(out, r.Name)
	}
	return out
}
