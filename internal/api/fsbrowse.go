package api

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"

	"kino/internal/ffmpeg"
)

func marshalJSON(v any) ([]byte, error) { return json.Marshal(v) }

func urlQueryEscape(s string) string { return url.QueryEscape(s) }

func ffmpegInstallHint() string { return ffmpeg.InstallHint() }

// listRoots returns the top-level starting points for the folder picker: drive
// letters on Windows, and the filesystem root plus the user's home elsewhere.
func listRoots() []dirEntry {
	var out []dirEntry

	if runtime.GOOS == "windows" {
		for c := 'A'; c <= 'Z'; c++ {
			drive := string(c) + `:\`
			// Stat is the cheapest way to tell a mounted drive from an empty bay; an
			// unmounted letter simply errors.
			if _, err := os.Stat(drive); err == nil {
				out = append(out, dirEntry{Name: drive, Path: drive, IsDir: true})
			}
		}
	} else {
		out = append(out, dirEntry{Name: "/", Path: "/", IsDir: true})
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out, dirEntry{Name: "Home (" + filepath.Base(home) + ")", Path: home, IsDir: true})
	}

	return out
}
