package images

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"

	"github.com/disintegration/imaging"
)

// sampleJPEG builds a real JPEG with enough detail that resizing measurably shrinks it.
func sampleJPEG(t *testing.T, w, h int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8((x * 7) % 256),
				G: uint8((y * 13) % 256),
				B: uint8((x*y)%256),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestStoreAndOpenOriginal(t *testing.T) {
	cache := New(t.TempDir())
	data := sampleJPEG(t, 800, 600)

	key, err := cache.Store(data, "test.jpg")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !cache.Has(key) {
		t.Fatal("cache reports the image is missing after storing it")
	}

	path, err := cache.Open(key, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stored, data) {
		t.Error("the stored original does not match what was written")
	}
}

func TestStoreIsContentAddressed(t *testing.T) {
	cache := New(t.TempDir())
	data := sampleJPEG(t, 200, 200)

	// Identical bytes must collapse to one entry: an album's twelve tracks all carry
	// the same embedded cover art, and storing it twelve times would be wasteful.
	first, err := cache.Store(data, "a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.Store(data, "b.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("identical content produced different keys: %s vs %s", first, second)
	}
}

// TestResizeActuallyResizes is the regression guard for a silent failure: when the
// resize errored, Open fell back to the original, so callers got a full-size image
// while believing they had asked for a thumbnail.
func TestResizeActuallyResizes(t *testing.T) {
	cache := New(t.TempDir())
	original := sampleJPEG(t, 1200, 900)

	key, err := cache.Store(original, "big.jpg")
	if err != nil {
		t.Fatal(err)
	}

	originalPath, err := cache.Open(key, 0)
	if err != nil {
		t.Fatal(err)
	}
	originalInfo, _ := os.Stat(originalPath)

	thumbPath, err := cache.Open(key, 185)
	if err != nil {
		t.Fatalf("open resized: %v", err)
	}

	if thumbPath == originalPath {
		t.Fatal("Open returned the original instead of a resized variant")
	}

	img, err := imaging.Open(thumbPath)
	if err != nil {
		t.Fatalf("decode the resized image: %v", err)
	}
	if got := img.Bounds().Dx(); got != 185 {
		t.Errorf("resized width = %d, want 185", got)
	}
	// Aspect ratio must be preserved: 1200x900 is 4:3, so 185 wide is 139 high.
	if got := img.Bounds().Dy(); got < 135 || got > 143 {
		t.Errorf("resized height = %d, want about 139 (aspect ratio preserved)", got)
	}

	thumbInfo, _ := os.Stat(thumbPath)
	if thumbInfo.Size() >= originalInfo.Size() {
		t.Errorf("thumbnail is %d bytes, not smaller than the %d byte original",
			thumbInfo.Size(), originalInfo.Size())
	}
}

func TestResizedVariantIsCached(t *testing.T) {
	cache := New(t.TempDir())
	key, err := cache.Store(sampleJPEG(t, 800, 600), "x.jpg")
	if err != nil {
		t.Fatal(err)
	}

	first, err := cache.Open(key, 342)
	if err != nil {
		t.Fatal(err)
	}
	firstInfo, _ := os.Stat(first)

	second, err := cache.Open(key, 342)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, _ := os.Stat(second)

	if first != second {
		t.Errorf("second request produced a different path: %s vs %s", first, second)
	}
	if !firstInfo.ModTime().Equal(secondInfo.ModTime()) {
		t.Error("the variant was regenerated instead of served from cache")
	}
}

// TestNeverUpscales: enlarging a small poster costs bytes and looks worse, so the
// original is served instead.
func TestNeverUpscales(t *testing.T) {
	cache := New(t.TempDir())
	key, err := cache.Store(sampleJPEG(t, 100, 100), "small.jpg")
	if err != nil {
		t.Fatal(err)
	}

	originalPath, _ := cache.Open(key, 0)
	path, err := cache.Open(key, 780)
	if err != nil {
		t.Fatal(err)
	}
	if path != originalPath {
		t.Error("a request larger than the source should return the original, not an upscale")
	}
}

func TestWidthsAreSnapped(t *testing.T) {
	// Arbitrary widths must snap to the allowed set, or a caller could fill the disk
	// by requesting every width from 1 to 4000.
	cases := map[int]int{1: 92, 92: 92, 100: 154, 200: 342, 900: 1280, 9000: 1280}
	for in, want := range cases {
		if got := snapWidth(in); got != want {
			t.Errorf("snapWidth(%d) = %d, want %d", in, got, want)
		}
	}
}

// TestInvalidKeysRejected guards the image endpoint against path traversal.
func TestInvalidKeysRejected(t *testing.T) {
	cache := New(t.TempDir())
	for _, key := range []string{
		"../../etc/passwd",
		"..\\..\\windows\\system32",
		"short",
		"NOTHEXADECIMAL!!",
		"",
	} {
		if _, err := cache.Open(key, 0); err != ErrNotFound {
			t.Errorf("Open(%q) = %v, want ErrNotFound", key, err)
		}
		if cache.Has(key) {
			t.Errorf("Has(%q) should be false", key)
		}
	}
}

func TestOpenMissingKey(t *testing.T) {
	cache := New(t.TempDir())
	if _, err := cache.Open("abcdef0123456789abcd", 0); err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestStoreRejectsEmptyData(t *testing.T) {
	cache := New(t.TempDir())
	if _, err := cache.Store(nil, "empty"); err == nil {
		t.Error("storing empty data should fail")
	}
}
