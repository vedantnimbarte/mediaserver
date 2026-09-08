package scanner

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// videoExts are the container extensions treated as playable video.
var videoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true,
	".wmv": true, ".flv": true, ".webm": true, ".mpg": true, ".mpeg": true,
	".m2ts": true, ".ts": true, ".mts": true, ".vob": true, ".ogv": true,
	".divx": true, ".rmvb": true, ".asf": true, ".3gp": true, ".mxf": true,
}

// audioExts are the container extensions treated as playable audio.
var audioExts = map[string]bool{
	".mp3": true, ".flac": true, ".m4a": true, ".aac": true, ".ogg": true,
	".oga": true, ".opus": true, ".wav": true, ".wma": true, ".alac": true,
	".aiff": true, ".aif": true, ".ape": true, ".wv": true, ".mpc": true,
	".dsf": true, ".m4b": true,
}

// imageExts are the extensions treated as photos.
var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".bmp": true, ".tif": true, ".tiff": true, ".heic": true, ".heif": true,
	".avif": true,
}

// subtitleExts are recognized sidecar subtitle formats.
var subtitleExts = map[string]bool{
	".srt": true, ".vtt": true, ".ass": true, ".ssa": true, ".sub": true, ".sbv": true,
}

// skipDirs are directories that never contain library content and are expensive or
// pointless to walk into.
var skipDirs = map[string]bool{
	"@eadir":            true, // Synology thumbnail sidecars
	".@__thumb":         true,
	"#recycle":          true,
	"$recycle.bin":      true,
	"system volume information": true,
	".ds_store":         true,
	"lost+found":        true,
	".git":              true,
	".svn":              true,
	"extras":            true, // trailers and featurettes, not the feature itself
	"featurettes":       true,
	"behind the scenes": true,
	"deleted scenes":    true,
	"trailers":          true,
	"sample":            true,
	"samples":           true,
	"subs":              true, // handled by the subtitle sidecar lookup, not the walker
	"subtitles":         true,
}

// junkNamePatterns mark files that sit inside a library but are not the feature:
// sample clips, trailers and extras.
var junkNamePatterns = []string{
	"sample", "trailer", "-trailer", ".trailer",
	"featurette", "behindthescenes", "deleted",
}

// IsVideoFile reports whether a path looks like playable video.
func IsVideoFile(path string) bool { return videoExts[strings.ToLower(filepath.Ext(path))] }

// IsAudioFile reports whether a path looks like playable audio.
func IsAudioFile(path string) bool { return audioExts[strings.ToLower(filepath.Ext(path))] }

// IsImageFile reports whether a path looks like a photo.
func IsImageFile(path string) bool { return imageExts[strings.ToLower(filepath.Ext(path))] }

// IsSubtitleFile reports whether a path is a sidecar subtitle.
func IsSubtitleFile(path string) bool { return subtitleExts[strings.ToLower(filepath.Ext(path))] }

// SkipDir reports whether a directory should not be descended into.
func SkipDir(name string) bool {
	lower := strings.ToLower(name)
	if skipDirs[lower] {
		return true
	}
	// Hidden directories, except the current directory marker.
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

// IsJunkFile reports whether a media file is a sample, trailer or extra rather than
// the main feature. Small "sample" files in particular would otherwise show up as
// duplicate library entries.
func IsJunkFile(path string) bool {
	name := strings.ToLower(stemOf(path))
	compact := strings.NewReplacer(" ", "", "_", "", ".", "", "-", "").Replace(name)

	for _, pattern := range junkNamePatterns {
		clean := strings.NewReplacer(" ", "", "_", "", ".", "", "-", "").Replace(pattern)
		if compact == clean || strings.HasSuffix(compact, clean) || strings.HasPrefix(compact, clean) {
			return true
		}
	}
	return false
}

// MinFeatureBytes is the size below which a video file is assumed to be a sample
// rather than a feature. Real content is essentially never this small.
const MinFeatureBytes = 50 << 20 // 50 MiB

// StableID derives a deterministic ID for an item from its path.
//
// Deriving the ID from the path rather than generating a random one means a rescan
// reuses the same IDs, so watch history, resume points and shared links survive.
// The trade-off is that moving a file loses its history, which is the rarer event.
func StableID(prefix, path string) string {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	sum := sha1.Sum([]byte(normalized))
	return prefix + "_" + hex.EncodeToString(sum[:12])
}
