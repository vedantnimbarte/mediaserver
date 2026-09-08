// Package ffmpeg wraps the external ffmpeg and ffprobe binaries: locating them,
// probing media, and detecting which hardware encoders actually work on this machine.
package ffmpeg

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrNotInstalled is returned when neither binary can be found.
var ErrNotInstalled = errors.New("ffmpeg is not installed or could not be found")

// Tools holds the resolved paths to the two binaries we shell out to.
type Tools struct {
	FFmpeg  string
	FFprobe string
	Version string
}

// Locate resolves the ffmpeg and ffprobe binaries.
//
// Explicit config paths win, then PATH, then a short list of the places these builds
// conventionally land on each platform. The fallback list matters on Windows, where
// people commonly unzip a portable build rather than run an installer.
func Locate(ffmpegPath, ffprobePath string) (*Tools, error) {
	t := &Tools{}

	t.FFmpeg = resolveBinary(ffmpegPath, "ffmpeg")
	if t.FFmpeg == "" {
		return nil, fmt.Errorf("%w: could not find ffmpeg", ErrNotInstalled)
	}

	t.FFprobe = resolveBinary(ffprobePath, "ffprobe")
	if t.FFprobe == "" {
		// ffprobe almost always ships beside ffmpeg; look there before giving up.
		candidate := filepath.Join(filepath.Dir(t.FFmpeg), exeName("ffprobe"))
		if isExecutable(candidate) {
			t.FFprobe = candidate
		} else {
			return nil, fmt.Errorf("%w: found ffmpeg at %s but no ffprobe beside it", ErrNotInstalled, t.FFmpeg)
		}
	}

	t.Version = probeVersion(t.FFmpeg)
	return t, nil
}

// resolveBinary tries an explicit path, then PATH, then the conventional locations.
func resolveBinary(explicit, name string) string {
	if explicit != "" {
		// An explicit setting may point at either the binary or its containing folder.
		if isExecutable(explicit) {
			return explicit
		}
		if inDir := filepath.Join(explicit, exeName(name)); isExecutable(inDir) {
			return inDir
		}
		// Fall through: a stale config setting should not permanently break startup.
	}

	if p, err := exec.LookPath(name); err == nil {
		return p
	}

	for _, dir := range commonDirs() {
		if candidate := filepath.Join(dir, exeName(name)); isExecutable(candidate) {
			return candidate
		}
	}
	return ""
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS != "windows" {
		// Require at least one execute bit; a data file named "ffmpeg" is not a tool.
		if info.Mode()&0o111 == 0 {
			return false
		}
	}
	return true
}

// commonDirs lists the folders portable and packaged builds typically end up in.
func commonDirs() []string {
	home, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "windows":
		var dirs []string
		if home != "" {
			dirs = append(dirs,
				filepath.Join(home, "tools", "ffmpeg", "bin"),
				filepath.Join(home, "ffmpeg", "bin"),
				filepath.Join(home, "scoop", "shims"),
			)
		}
		dirs = append(dirs,
			`C:\ffmpeg\bin`,
			`C:\Program Files\ffmpeg\bin`,
			`C:\ProgramData\chocolatey\bin`,
		)
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Microsoft", "WinGet", "Links"))
		}
		return dirs

	case "darwin":
		return []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}

	default:
		return []string{"/usr/bin", "/usr/local/bin", "/snap/bin", "/var/lib/flatpak/exports/bin"}
	}
}

// probeVersion extracts the version banner, used only for display and diagnostics.
func probeVersion(ffmpegPath string) string {
	out, err := exec.Command(ffmpegPath, "-hide_banner", "-version").Output()
	if err != nil {
		return "unknown"
	}
	line, _, _ := strings.Cut(string(out), "\n")
	line = strings.TrimSpace(line)
	// The banner reads "ffmpeg version 7.1 Copyright (c) ...".
	if after, ok := strings.CutPrefix(line, "ffmpeg version "); ok {
		if v, _, found := strings.Cut(after, " "); found {
			return v
		}
		return after
	}
	return line
}

// InstallHint returns a platform-appropriate message telling the user how to install
// ffmpeg, shown when Locate fails.
func InstallHint() string {
	switch runtime.GOOS {
	case "windows":
		return "Install ffmpeg with `winget install Gyan.FFmpeg`, or download a build from " +
			"https://www.gyan.dev/ffmpeg/builds/ and set its bin folder in Settings."
	case "darwin":
		return "Install ffmpeg with `brew install ffmpeg`."
	default:
		return "Install ffmpeg with your package manager, for example `sudo apt install ffmpeg`."
	}
}
