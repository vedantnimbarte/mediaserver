package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type item struct {
	ID    string `json:"id"`
	Value int    `json:"value"`
}

func (i item) EntityID() string { return i.ID }

func openTemp(t *testing.T) (*Collection[item], string) {
	t.Helper()
	dir := t.TempDir()
	c, err := Open[item](dir, "items")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, dir
}

func TestPutGetDelete(t *testing.T) {
	c, _ := openTemp(t)

	if err := c.Put(item{ID: "a", Value: 1}); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := c.Get("a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Value != 1 {
		t.Fatalf("got value %d, want 1", got.Value)
	}

	if _, err := c.Get("missing"); err != ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	c.Delete("a")
	if c.Has("a") {
		t.Fatal("item still present after delete")
	}
	if c.Len() != 0 {
		t.Fatalf("len is %d, want 0", c.Len())
	}
}

func TestPutRejectsEmptyID(t *testing.T) {
	c, _ := openTemp(t)
	if err := c.Put(item{Value: 5}); err == nil {
		t.Fatal("expected an error putting an entity with an empty ID")
	}
}

func TestUpdateAndUpsert(t *testing.T) {
	c, _ := openTemp(t)
	_ = c.Put(item{ID: "a", Value: 1})

	if _, err := c.Update("a", func(i *item) { i.Value = 42 }); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := c.Get("a")
	if got.Value != 42 {
		t.Fatalf("got %d, want 42", got.Value)
	}

	if _, err := c.Update("nope", func(i *item) {}); err != ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	// Upsert creates when absent.
	if _, err := c.Upsert("b", func(i *item) { i.ID = "b"; i.Value = 7 }); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got, _ := c.Get("b"); got.Value != 7 {
		t.Fatalf("got %d, want 7", got.Value)
	}

	// An upsert that writes the wrong ID must be rejected, not silently mis-keyed.
	if _, err := c.Upsert("c", func(i *item) { i.ID = "wrong" }); err == nil {
		t.Fatal("expected an error when the upsert callback sets a mismatched ID")
	}
	if c.Has("c") {
		t.Fatal("rejected upsert should not have stored anything")
	}
}

func TestDeleteWhereAndFilter(t *testing.T) {
	c, _ := openTemp(t)
	for i := 0; i < 10; i++ {
		_ = c.Put(item{ID: fmt.Sprintf("id-%d", i), Value: i})
	}

	even := c.Filter(func(i item) bool { return i.Value%2 == 0 })
	if len(even) != 5 {
		t.Fatalf("filter returned %d, want 5", len(even))
	}

	removed := c.DeleteWhere(func(i item) bool { return i.Value < 3 })
	if removed != 3 {
		t.Fatalf("removed %d, want 3", removed)
	}
	if c.Len() != 7 {
		t.Fatalf("len is %d, want 7", c.Len())
	}
}

// TestFlushIsAtomicAndReloads is the core durability guarantee: what Close writes,
// a fresh Open must read back identically.
func TestFlushIsAtomicAndReloads(t *testing.T) {
	dir := t.TempDir()

	c, err := Open[item](dir, "items")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 100; i++ {
		_ = c.Put(item{ID: fmt.Sprintf("id-%03d", i), Value: i})
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// No temp file may survive a clean shutdown.
	if _, err := os.Stat(filepath.Join(dir, "items.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("items.json.tmp still exists after close")
	}

	reopened, err := Open[item](dir, "items")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	if reopened.Len() != 100 {
		t.Fatalf("reloaded %d items, want 100", reopened.Len())
	}
	got, err := reopened.Get("id-042")
	if err != nil {
		t.Fatalf("get after reload: %v", err)
	}
	if got.Value != 42 {
		t.Fatalf("got %d, want 42", got.Value)
	}
}

func TestFlushIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	c, _ := Open[item](dir, "items")
	for i := 0; i < 20; i++ {
		_ = c.Put(item{ID: fmt.Sprintf("id-%02d", i), Value: i})
	}
	_ = c.Close()

	first, _ := os.ReadFile(filepath.Join(dir, "items.json"))

	// Rewriting the same data must produce byte-identical output, so the file is
	// diffable and backups do not churn.
	c2, _ := Open[item](dir, "items")
	_ = c2.Put(item{ID: "id-00", Value: 0}) // no-op change, but marks dirty
	_ = c2.Close()

	second, _ := os.ReadFile(filepath.Join(dir, "items.json"))
	if string(first) != string(second) {
		t.Fatal("flush output is not deterministic across runs")
	}
}

// TestCorruptFileRecoversFromBackup covers the failure mode that would otherwise
// silently lose a library: a truncated or malformed primary file.
func TestCorruptFileRecoversFromBackup(t *testing.T) {
	dir := t.TempDir()

	c, _ := Open[item](dir, "items")
	_ = c.Put(item{ID: "a", Value: 1})
	_ = c.Flush()
	// A second flush rotates the good copy into items.json.bak.
	_ = c.Put(item{ID: "b", Value: 2})
	_ = c.Close()

	primary := filepath.Join(dir, "items.json")
	if _, err := os.Stat(primary + ".bak"); err != nil {
		t.Fatalf("expected a .bak to exist: %v", err)
	}

	if err := os.WriteFile(primary, []byte(`[{"id": "a", "value":`), 0o644); err != nil {
		t.Fatalf("corrupt primary: %v", err)
	}

	recovered, err := Open[item](dir, "items")
	if err != nil {
		t.Fatalf("open after corruption: %v", err)
	}
	defer recovered.Close()

	if recovered.Len() == 0 {
		t.Fatal("expected data to be recovered from the backup")
	}
	if !recovered.Has("a") {
		t.Fatal("recovered collection is missing item a")
	}

	matches, _ := filepath.Glob(primary + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("expected the corrupt file to be quarantined, found %d", len(matches))
	}
}

func TestCorruptWithNoBackupStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "items.json")
	if err := os.WriteFile(primary, []byte(`not json at all`), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := Open[item](dir, "items")
	if err != nil {
		t.Fatalf("open should not fail on corruption: %v", err)
	}
	defer c.Close()
	if c.Len() != 0 {
		t.Fatalf("expected an empty collection, got %d items", c.Len())
	}
}

// TestConcurrentAccess is the guarantee that makes JSON-backed storage safe under an
// HTTP server: many readers and writers, no data race, no lost writes.
func TestConcurrentAccess(t *testing.T) {
	c, _ := openTemp(t)

	const writers, readers, iterations = 8, 8, 200
	var wg sync.WaitGroup

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = c.Put(item{ID: fmt.Sprintf("w%d-i%d", w, i), Value: i})
			}
		}(w)
	}
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = c.All()
				_ = c.Len()
				_, _ = c.Get("w0-i0")
			}
		}()
	}
	wg.Wait()

	if want := writers * iterations; c.Len() != want {
		t.Fatalf("len is %d, want %d", c.Len(), want)
	}
}

func TestEmptyFileLoadsClean(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "items.json"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Open[item](dir, "items")
	if err != nil {
		t.Fatalf("open empty file: %v", err)
	}
	defer c.Close()
	if c.Len() != 0 {
		t.Fatalf("expected 0 items, got %d", c.Len())
	}
}

func TestStoredFormatIsAList(t *testing.T) {
	dir := t.TempDir()
	c, _ := Open[item](dir, "items")
	_ = c.Put(item{ID: "a", Value: 1})
	_ = c.Close()

	data, err := os.ReadFile(filepath.Join(dir, "items.json"))
	if err != nil {
		t.Fatal(err)
	}
	var list []item
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("stored file is not a JSON list: %v", err)
	}
	if len(list) != 1 || list[0].ID != "a" {
		t.Fatalf("unexpected stored content: %s", data)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	c, _ := Open[item](dir, "items")
	_ = c.Put(item{ID: "a"})
	if err := c.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("second close should be a no-op: %v", err)
	}
}
