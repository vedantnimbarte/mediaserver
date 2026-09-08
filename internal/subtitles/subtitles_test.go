package subtitles

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"kino/internal/ffmpeg"
	"kino/internal/models"
)

const sampleSRT = `1
00:00:01,000 --> 00:00:04,000
Hello from the sidecar.

2
00:00:05,000 --> 00:00:08,000
Second line of dialogue.
`

func testTools(t *testing.T) *ffmpeg.Tools {
	t.Helper()
	tools, err := ffmpeg.Locate("", "")
	if err != nil {
		t.Skipf("ffmpeg is not installed: %v", err)
	}
	return tools
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSidecarDiscovery(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "The Matrix (1999).mkv")
	writeFile(t, media, "not really a video")

	// Named after the media, with language and forced markers.
	writeFile(t, filepath.Join(dir, "The Matrix (1999).en.srt"), sampleSRT)
	writeFile(t, filepath.Join(dir, "The Matrix (1999).fr.srt"), sampleSRT)
	writeFile(t, filepath.Join(dir, "The Matrix (1999).en.forced.srt"), sampleSRT)
	// A neighbouring film's subtitles must not be picked up.
	writeFile(t, filepath.Join(dir, "Inception (2010).en.srt"), sampleSRT)
	// A dedicated Subs folder: everything inside belongs to this media.
	writeFile(t, filepath.Join(dir, "Subs", "German.srt"), sampleSRT)

	svc := NewService(nil, t.TempDir())
	tracks := svc.Tracks(models.MediaInfo{Path: media})

	var labels []string
	for _, tr := range tracks {
		labels = append(labels, tr.Label)
		if strings.Contains(tr.Path, "Inception") {
			t.Errorf("picked up a neighbouring film's subtitles: %s", tr.Path)
		}
	}

	if len(tracks) != 4 {
		t.Fatalf("found %d tracks %v, want 4", len(tracks), labels)
	}

	var sawEnglish, sawFrench, sawForced, sawSubsFolder bool
	for _, tr := range tracks {
		switch {
		case tr.Code == "en" && tr.Forced:
			sawForced = true
			if !strings.Contains(tr.Label, "Forced") {
				t.Errorf("forced track is not labelled as such: %q", tr.Label)
			}
		case tr.Code == "en":
			sawEnglish = true
			if tr.Language != "English" {
				t.Errorf("language = %q, want English", tr.Language)
			}
		case tr.Code == "fr":
			sawFrench = true
		case strings.Contains(tr.Path, "Subs"):
			sawSubsFolder = true
		}
	}
	if !sawEnglish || !sawFrench || !sawForced || !sawSubsFolder {
		t.Errorf("missing tracks: en=%v fr=%v forced=%v subsFolder=%v (found %v)",
			sawEnglish, sawFrench, sawForced, sawSubsFolder, labels)
	}
}

// TestForcedTracksSortFirst matters because forced subtitles carry translated signage
// the viewer almost always wants, so they belong at the top of the menu.
func TestForcedTracksSortFirst(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	writeFile(t, media, "x")
	writeFile(t, filepath.Join(dir, "Movie.zu.srt"), sampleSRT)
	writeFile(t, filepath.Join(dir, "Movie.en.forced.srt"), sampleSRT)

	tracks := NewService(nil, t.TempDir()).Tracks(models.MediaInfo{Path: media})
	if len(tracks) < 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	if !tracks[0].Forced {
		t.Errorf("first track is %q; a forced track should sort first", tracks[0].Label)
	}
}

func TestSidecarConvertsToWebVTT(t *testing.T) {
	tools := testTools(t)

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	writeFile(t, media, "x")
	writeFile(t, filepath.Join(dir, "Movie.en.srt"), sampleSRT)

	svc := NewService(tools, t.TempDir())
	info := models.MediaInfo{Path: media}

	tracks := svc.Tracks(info)
	if len(tracks) != 1 {
		t.Fatalf("found %d tracks, want 1", len(tracks))
	}

	path, err := svc.WebVTT(context.Background(), info, tracks[0])
	if err != nil {
		t.Fatalf("convert: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "WEBVTT") {
		t.Errorf("output is not WebVTT:\n%s", content)
	}
	if !strings.Contains(content, "Hello from the sidecar.") {
		t.Errorf("dialogue is missing from the conversion:\n%s", content)
	}
	// WebVTT uses a dot before milliseconds where SRT uses a comma, and makes the
	// hour field optional, so match on the cue arrow and the decimal separator rather
	// than on a fixed timestamp width.
	if !strings.Contains(content, "-->") || !strings.Contains(content, "01.000") {
		t.Errorf("timestamps were not converted to WebVTT form:\n%s", content)
	}
	if strings.Contains(content, "01,000") {
		t.Errorf("output still contains SRT comma timestamps:\n%s", content)
	}

	// A second call must hit the cache and return the same file.
	again, err := svc.WebVTT(context.Background(), info, tracks[0])
	if err != nil {
		t.Fatalf("second convert: %v", err)
	}
	if again != path {
		t.Errorf("cache miss: got %s, want %s", again, path)
	}
}

func TestVTTSidecarIsServedDirectly(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	writeFile(t, media, "x")
	vtt := filepath.Join(dir, "Movie.en.vtt")
	writeFile(t, vtt, "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\nAlready VTT.\n")

	svc := NewService(nil, t.TempDir()) // no ffmpeg: no conversion should be needed
	info := models.MediaInfo{Path: media}

	tracks := svc.Tracks(info)
	if len(tracks) != 1 {
		t.Fatalf("found %d tracks, want 1", len(tracks))
	}

	path, err := svc.WebVTT(context.Background(), info, tracks[0])
	if err != nil {
		t.Fatalf("serve vtt: %v", err)
	}
	if path != vtt {
		t.Errorf("got %s, want the original file %s", path, vtt)
	}
}

// TestEmbeddedSubtitleExtraction runs the real pipeline: mux subtitles into an MKV,
// then pull them back out as WebVTT.
func TestEmbeddedSubtitleExtraction(t *testing.T) {
	tools := testTools(t)
	dir := t.TempDir()

	srtPath := filepath.Join(dir, "source.srt")
	writeFile(t, srtPath, sampleSRT)

	media := filepath.Join(dir, "with-subs.mkv")
	cmd := exec.Command(tools.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=10:size=320x240:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=10",
		"-i", srtPath,
		"-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-c:s", "srt",
		"-metadata:s:s:0", "language=eng",
		"-shortest",
		media,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mux subtitles: %v\n%s", err, out)
	}

	info, err := tools.Probe(context.Background(), media)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(info.SubtitleStreams()) != 1 {
		t.Fatalf("probe found %d subtitle streams, want 1", len(info.SubtitleStreams()))
	}

	svc := NewService(tools, t.TempDir())
	tracks := svc.Tracks(info)
	if len(tracks) != 1 {
		t.Fatalf("found %d tracks, want 1", len(tracks))
	}
	track := tracks[0]

	if track.Source != SourceEmbedded {
		t.Errorf("source = %q, want embedded", track.Source)
	}
	if track.Language != "English" {
		t.Errorf("language = %q, want English", track.Language)
	}
	if track.BurnInOnly {
		t.Error("a text subtitle track should not require burn-in")
	}

	path, err := svc.WebVTT(context.Background(), info, track)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Hello from the sidecar.") {
		t.Errorf("extracted subtitles are missing the dialogue:\n%s", data)
	}
}

// TestImageSubtitlesRequireBurnIn documents the one case that cannot become text.
func TestImageSubtitlesRequireBurnIn(t *testing.T) {
	info := models.MediaInfo{
		Path: filepath.Join(t.TempDir(), "movie.mkv"),
		Streams: []models.Stream{
			{Index: 2, Kind: models.StreamSubtitle, TypeIndex: 0,
				Codec: "hdmv_pgs_subtitle", Language: "eng"},
		},
	}

	svc := NewService(nil, t.TempDir())
	tracks := svc.Tracks(info)
	if len(tracks) != 1 {
		t.Fatalf("found %d tracks, want 1", len(tracks))
	}
	if !tracks[0].BurnInOnly {
		t.Error("a PGS track must be flagged as burn-in only")
	}
	if !strings.Contains(tracks[0].Label, "burn-in") {
		t.Errorf("label %q should mention burn-in", tracks[0].Label)
	}

	if _, err := svc.WebVTT(context.Background(), info, tracks[0]); err == nil {
		t.Error("converting an image subtitle track to WebVTT should fail")
	}
}

func TestNoSubtitlesIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	media := filepath.Join(dir, "bare.mkv")
	writeFile(t, media, "x")

	tracks := NewService(nil, t.TempDir()).Tracks(models.MediaInfo{Path: media})
	if len(tracks) != 0 {
		t.Errorf("found %d tracks, want none", len(tracks))
	}
}

func TestParseSidecarSuffix(t *testing.T) {
	cases := []struct {
		subStem, mediaStem string
		wantCode           string
		wantForced         bool
	}{
		{"Movie.en", "Movie", "en", false},
		{"Movie.eng.forced", "Movie", "eng", true},
		{"Movie.fr.sdh", "Movie", "fr", false},
		{"Movie", "Movie", "", false},
		// A region-tagged code still resolves to its base language, so "pt-BR" is
		// offered as Portuguese rather than being dropped.
		{"Movie.pt-BR", "Movie", "pt", false},
	}
	for _, tc := range cases {
		code, forced := parseSidecarSuffix(tc.subStem, tc.mediaStem)
		if code != tc.wantCode || forced != tc.wantForced {
			t.Errorf("parseSidecarSuffix(%q) = (%q, %v), want (%q, %v)",
				tc.subStem, code, forced, tc.wantCode, tc.wantForced)
		}
	}
}
