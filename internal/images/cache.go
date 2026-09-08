// Package images stores artwork on disk and serves resized copies.
//
// Everything is cached locally on first use, so after a library has been scanned once
// the server never needs the network again to render its UI.
package images

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
)

// ErrNotFound is returned for an unknown cache key.
var ErrNotFound = errors.New("image not found")

// maxImageBytes caps a single download. Posters are well under a megabyte; anything
// far larger is a misconfigured URL rather than artwork.
const maxImageBytes = 20 << 20

// Cache stores original images and their resized variants under a single directory.
type Cache struct {
	dir    string
	client *http.Client

	// inflight collapses concurrent requests for the same image into one download,
	// which matters during a scan where many episodes share one show poster.
	mu       sync.Mutex
	inflight map[string]chan struct{}
}

// New creates an image cache rooted at dir.
func New(dir string) *Cache {
	return &Cache{
		dir:      dir,
		client:   &http.Client{Timeout: 30 * time.Second},
		inflight: make(map[string]chan struct{}),
	}
}

// Dir returns the cache root.
func (c *Cache) Dir() string { return c.dir }

// Key derives the cache key for a source URL.
func Key(source string) string {
	sum := sha1.Sum([]byte(source))
	return hex.EncodeToString(sum[:12])
}

// originalPath is where the full-size image for a key lives.
func (c *Cache) originalPath(key string) string {
	return filepath.Join(c.dir, key[:2], key+".img")
}

// variantPath is where a resized copy lives.
func (c *Cache) variantPath(key string, width int) string {
	return filepath.Join(c.dir, key[:2], fmt.Sprintf("%s_w%d.jpg", key, width))
}

// Has reports whether the original for a key is already cached.
func (c *Cache) Has(key string) bool {
	if !validKey(key) {
		return false
	}
	info, err := os.Stat(c.originalPath(key))
	return err == nil && info.Size() > 0
}

// Download fetches an image and stores it, returning its cache key. A second call for
// the same URL is a no-op that returns the existing key.
func (c *Cache) Download(ctx context.Context, url string) (string, error) {
	if url == "" {
		return "", errors.New("empty image url")
	}
	key := Key(url)

	if c.Has(key) {
		return key, nil
	}

	// Collapse concurrent downloads of the same URL.
	c.mu.Lock()
	if wait, busy := c.inflight[key]; busy {
		c.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		if c.Has(key) {
			return key, nil
		}
		return "", fmt.Errorf("download of %s failed in another request", url)
	}
	done := make(chan struct{})
	c.inflight[key] = done
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.inflight, key)
		c.mu.Unlock()
		close(done)
	}()

	if err := c.fetch(ctx, url, key); err != nil {
		return "", err
	}
	return key, nil
}

func (c *Cache) fetch(ctx context.Context, url, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build image request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download image: server returned %s", resp.Status)
	}

	dest := c.originalPath(key)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create image cache dir: %w", err)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}

	written, err := io.Copy(f, io.LimitReader(resp.Body, maxImageBytes))
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("write image: %w", err)
	}
	if written == 0 {
		os.Remove(tmp)
		return errors.New("the image was empty")
	}

	// Rename into place so a partially downloaded file is never visible as cached.
	return os.Rename(tmp, dest)
}

// Store saves raw image bytes (used for cover art extracted from music files) and
// returns the cache key.
func (c *Cache) Store(data []byte, sourceHint string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("no image data")
	}
	// Key on the content so identical album art shared across tracks is stored once.
	sum := sha1.Sum(data)
	key := hex.EncodeToString(sum[:12])

	if c.Has(key) {
		return key, nil
	}

	dest := c.originalPath(key)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", fmt.Errorf("create image cache dir: %w", err)
	}

	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("write image: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return key, nil
}

// StoreFile copies an image already on disk into the cache.
func (c *Cache) StoreFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return c.Store(data, path)
}

// Open returns a readable path for the image at the given width.
//
// width of 0 means the original. Resized variants are generated on first request and
// cached, so a poster grid pays the resize cost once rather than on every page load.
func (c *Cache) Open(key string, width int) (string, error) {
	if !validKey(key) {
		return "", ErrNotFound
	}

	original := c.originalPath(key)
	if info, err := os.Stat(original); err != nil || info.Size() == 0 {
		return "", ErrNotFound
	}

	if width <= 0 {
		return original, nil
	}
	width = snapWidth(width)

	variant := c.variantPath(key, width)
	if info, err := os.Stat(variant); err == nil && info.Size() > 0 {
		return variant, nil
	}

	if err := c.resize(original, variant, width); err != nil {
		// A resize failure should not blank the UI; fall back to the original.
		return original, nil
	}
	return variant, nil
}

func (c *Cache) resize(src, dest string, width int) error {
	img, err := imaging.Open(src, imaging.AutoOrientation(true))
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}

	// Never upscale: enlarging a small poster just wastes bytes and looks soft.
	if img.Bounds().Dx() <= width {
		return errors.New("source is already smaller than the requested width")
	}

	resized := imaging.Resize(img, width, 0, imaging.Lanczos)

	// Encode explicitly rather than through imaging.Save: Save infers the format from
	// the filename extension, and the temp file's ".tmp" suffix is not a known image
	// format, so every resize would fail and silently fall back to the original.
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if err := imaging.Encode(f, resized, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("encode resized image: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dest)
}

// allowedWidths are the sizes the server will generate. Restricting the set stops a
// caller from filling the disk by requesting every width from 1 to 4000.
var allowedWidths = []int{92, 154, 185, 342, 500, 780, 1280}

// snapWidth rounds a requested width up to the nearest allowed size.
func snapWidth(w int) int {
	for _, allowed := range allowedWidths {
		if w <= allowed {
			return allowed
		}
	}
	return allowedWidths[len(allowedWidths)-1]
}

// validKey guards against path traversal through the key parameter.
func validKey(key string) bool {
	if len(key) < 8 || len(key) > 64 {
		return false
	}
	for _, r := range key {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

// Prune deletes cached images whose keys are not in the keep set, reclaiming space
// after items are removed from the library.
func (c *Cache) Prune(keep map[string]bool) (int, error) {
	var removed int

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	for _, shard := range entries {
		if !shard.IsDir() {
			continue
		}
		shardPath := filepath.Join(c.dir, shard.Name())
		files, err := os.ReadDir(shardPath)
		if err != nil {
			continue
		}
		for _, file := range files {
			name := file.Name()
			key, _, _ := strings.Cut(name, "_")
			key = strings.TrimSuffix(key, filepath.Ext(key))
			if keep[key] {
				continue
			}
			if err := os.Remove(filepath.Join(shardPath, name)); err == nil {
				removed++
			}
		}
	}

	return removed, nil
}
