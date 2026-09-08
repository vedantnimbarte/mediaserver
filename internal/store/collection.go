// Package store provides a small in-memory-with-JSON-durability persistence layer.
//
// Every collection is fully loaded into memory at boot and mirrored back to a single
// JSON file on disk. Writes are debounced (so a scan that touches 2000 items does not
// perform 2000 file rewrites) and atomic (temp file + fsync + rename), so the on-disk
// state is always either the previous version or the new one, never a truncated blend.
//
// This design is deliberately sized for libraries in the low thousands of items. If a
// collection outgrows that, the Collection API is the seam at which to swap in sharded
// or log-structured storage without touching any caller.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ErrNotFound is returned by Get when no item carries the requested ID.
var ErrNotFound = errors.New("not found")

const (
	debounceWindow = 500 * time.Millisecond
	maxFlushDelay  = 30 * time.Second
)

// Entity is the contract every stored record satisfies.
type Entity interface {
	EntityID() string
}

// Collection is a thread-safe, JSON-backed set of entities keyed by ID.
type Collection[T Entity] struct {
	name string
	path string

	mu    sync.RWMutex
	items map[string]T

	dirty  atomic.Bool
	notify chan struct{}
	stop   chan struct{}
	closed atomic.Bool
	wg     sync.WaitGroup

	// flushErr records the most recent flush failure so callers can surface it.
	flushErr atomic.Pointer[error]
}

// Open loads (or creates) the collection stored at <dir>/<name>.json and starts its
// background flusher. Close must be called to stop the flusher and persist pending writes.
//
// A file that fails to parse is preserved as <name>.json.corrupt-<n> and recovery is
// attempted from <name>.json.bak before giving up and starting empty.
func Open[T Entity](dir, name string) (*Collection[T], error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create store dir %s: %w", dir, err)
	}

	c := &Collection[T]{
		name:   name,
		path:   filepath.Join(dir, name+".json"),
		items:  make(map[string]T),
		notify: make(chan struct{}, 1),
		stop:   make(chan struct{}),
	}

	if err := c.load(); err != nil {
		return nil, err
	}

	c.wg.Add(1)
	go c.flushLoop()
	return c, nil
}

func (c *Collection[T]) load() error {
	items, err := readList[T](c.path)
	if err == nil {
		c.ingest(items)
		return nil
	}
	if os.IsNotExist(err) {
		return nil // brand new collection
	}

	// The primary file exists but is unreadable or malformed. Quarantine it and try the backup.
	quarantine := c.quarantinePath()
	if renameErr := os.Rename(c.path, quarantine); renameErr != nil {
		return fmt.Errorf("quarantine corrupt %s: %w (original error: %v)", c.path, renameErr, err)
	}
	fmt.Fprintf(os.Stderr, "store: %s is corrupt (%v); moved to %s\n", c.path, err, filepath.Base(quarantine))

	backup := c.path + ".bak"
	if items, bakErr := readList[T](backup); bakErr == nil {
		c.ingest(items)
		fmt.Fprintf(os.Stderr, "store: recovered %d %s records from %s\n", len(items), c.name, filepath.Base(backup))
		c.dirty.Store(true) // rewrite the primary from the recovered data
		return nil
	}

	fmt.Fprintf(os.Stderr, "store: no usable backup for %s; starting empty\n", c.name)
	return nil
}

func readList[T Entity](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("parse json: %w", err)
	}
	return items, nil
}

func (c *Collection[T]) ingest(items []T) {
	for _, it := range items {
		id := it.EntityID()
		if id == "" {
			continue // a record with no ID is unaddressable; drop it rather than clobber ""
		}
		c.items[id] = it
	}
}

func (c *Collection[T]) quarantinePath() string {
	for i := 1; ; i++ {
		p := fmt.Sprintf("%s.corrupt-%d", c.path, i)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
	}
}

// ---- reads ----

// Get returns the item with the given ID.
func (c *Collection[T]) Get(id string) (T, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	it, ok := c.items[id]
	if !ok {
		var zero T
		return zero, ErrNotFound
	}
	return it, nil
}

// Has reports whether an item with the given ID exists.
func (c *Collection[T]) Has(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.items[id]
	return ok
}

// All returns every item. Order is unspecified; sort at the call site.
func (c *Collection[T]) All() []T {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]T, 0, len(c.items))
	for _, it := range c.items {
		out = append(out, it)
	}
	return out
}

// Filter returns every item for which pred reports true.
func (c *Collection[T]) Filter(pred func(T) bool) []T {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []T
	for _, it := range c.items {
		if pred(it) {
			out = append(out, it)
		}
	}
	return out
}

// Find returns the first item matching pred. Iteration order is random, so only use
// this when at most one item can match.
func (c *Collection[T]) Find(pred func(T) bool) (T, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, it := range c.items {
		if pred(it) {
			return it, true
		}
	}
	var zero T
	return zero, false
}

// Len returns the number of stored items.
func (c *Collection[T]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// ---- writes ----

// Put inserts or replaces an item and schedules a flush.
func (c *Collection[T]) Put(item T) error {
	id := item.EntityID()
	if id == "" {
		return errors.New("store: cannot put an entity with an empty ID")
	}
	c.mu.Lock()
	c.items[id] = item
	c.mu.Unlock()
	c.markDirty()
	return nil
}

// PutMany inserts or replaces several items under a single lock acquisition and a
// single flush. Prefer this in scan loops.
func (c *Collection[T]) PutMany(items []T) error {
	if len(items) == 0 {
		return nil
	}
	c.mu.Lock()
	for _, item := range items {
		id := item.EntityID()
		if id == "" {
			c.mu.Unlock()
			return errors.New("store: cannot put an entity with an empty ID")
		}
		c.items[id] = item
	}
	c.mu.Unlock()
	c.markDirty()
	return nil
}

// Update applies fn to the stored item and writes the result back. fn receives a copy,
// so it cannot mutate shared state behind the lock. Returns ErrNotFound if absent.
func (c *Collection[T]) Update(id string, fn func(*T)) (T, error) {
	c.mu.Lock()
	it, ok := c.items[id]
	if !ok {
		c.mu.Unlock()
		var zero T
		return zero, ErrNotFound
	}
	fn(&it)
	c.items[id] = it
	c.mu.Unlock()
	c.markDirty()
	return it, nil
}

// Upsert applies fn to the existing item, or to the zero value if none exists, and
// stores the result under id.
func (c *Collection[T]) Upsert(id string, fn func(*T)) (T, error) {
	c.mu.Lock()
	it := c.items[id] // zero value when absent
	fn(&it)
	if got := it.EntityID(); got != id {
		c.mu.Unlock()
		var zero T
		return zero, fmt.Errorf("store: upsert callback set ID %q but key is %q", got, id)
	}
	c.items[id] = it
	c.mu.Unlock()
	c.markDirty()
	return it, nil
}

// Delete removes an item. Deleting a missing item is not an error.
func (c *Collection[T]) Delete(id string) {
	c.mu.Lock()
	_, existed := c.items[id]
	delete(c.items, id)
	c.mu.Unlock()
	if existed {
		c.markDirty()
	}
}

// DeleteWhere removes every item matching pred and returns how many were removed.
func (c *Collection[T]) DeleteWhere(pred func(T) bool) int {
	c.mu.Lock()
	var removed int
	for id, it := range c.items {
		if pred(it) {
			delete(c.items, id)
			removed++
		}
	}
	c.mu.Unlock()
	if removed > 0 {
		c.markDirty()
	}
	return removed
}

// ---- persistence ----

func (c *Collection[T]) markDirty() {
	c.dirty.Store(true)
	select {
	case c.notify <- struct{}{}:
	default: // a flush is already pending
	}
}

// flushLoop coalesces writes: it waits for a quiet period after the last mutation
// before hitting disk, but never lets a dirty collection go unwritten for longer
// than maxFlushDelay.
func (c *Collection[T]) flushLoop() {
	defer c.wg.Done()

	debounce := time.NewTimer(time.Hour)
	if !debounce.Stop() {
		<-debounce.C
	}
	debounceArmed := false

	deadline := time.NewTicker(maxFlushDelay)
	defer deadline.Stop()

	flush := func() {
		if debounceArmed {
			if !debounce.Stop() {
				select {
				case <-debounce.C:
				default:
				}
			}
			debounceArmed = false
		}
		if err := c.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "store: flush %s: %v\n", c.name, err)
		}
	}

	for {
		select {
		case <-c.notify:
			if !debounceArmed {
				debounce.Reset(debounceWindow)
				debounceArmed = true
			}

		case <-debounce.C:
			debounceArmed = false
			if err := c.Flush(); err != nil {
				fmt.Fprintf(os.Stderr, "store: flush %s: %v\n", c.name, err)
			}

		case <-deadline.C:
			if c.dirty.Load() {
				flush()
			}

		case <-c.stop:
			flush()
			return
		}
	}
}

// Flush writes the collection to disk immediately if there are pending changes.
// It is safe to call concurrently with reads and writes.
func (c *Collection[T]) Flush() error {
	if !c.dirty.CompareAndSwap(true, false) {
		return nil
	}

	c.mu.RLock()
	list := make([]T, 0, len(c.items))
	for _, it := range c.items {
		list = append(list, it)
	}
	c.mu.RUnlock()

	// Stable ordering keeps diffs readable and makes the file content deterministic.
	sort.Slice(list, func(i, j int) bool { return list[i].EntityID() < list[j].EntityID() })

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		c.dirty.Store(true)
		c.recordErr(err)
		return fmt.Errorf("marshal %s: %w", c.name, err)
	}

	if err := c.writeAtomic(data); err != nil {
		c.dirty.Store(true) // retry on the next tick rather than losing the change
		c.recordErr(err)
		return err
	}
	c.flushErr.Store(nil)
	return nil
}

// writeAtomic replaces the collection file without ever leaving a partial file in place.
// The previous version is rotated to .bak first, so a crash between the two renames
// still leaves a complete copy of the data on disk.
func (c *Collection[T]) writeAtomic(data []byte) error {
	tmp := c.path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close %s: %w", tmp, err)
	}

	// Rotate the current file to .bak. os.Rename replaces an existing destination on
	// both Windows and Unix, so no separate remove is needed.
	if _, err := os.Stat(c.path); err == nil {
		if err := os.Rename(c.path, c.path+".bak"); err != nil {
			// Not fatal: we would rather lose the backup than fail the write.
			fmt.Fprintf(os.Stderr, "store: rotate backup for %s: %v\n", c.name, err)
		}
	}

	if err := os.Rename(tmp, c.path); err != nil {
		return fmt.Errorf("replace %s: %w", c.path, err)
	}
	return nil
}

func (c *Collection[T]) recordErr(err error) { c.flushErr.Store(&err) }

// LastFlushError returns the most recent flush failure, or nil if the last flush
// succeeded. Handlers can use this to warn that the library is not being persisted.
func (c *Collection[T]) LastFlushError() error {
	if p := c.flushErr.Load(); p != nil {
		return *p
	}
	return nil
}

// Close stops the flusher and performs a final synchronous flush.
func (c *Collection[T]) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(c.stop)
	c.wg.Wait()
	return c.LastFlushError()
}

// Name returns the collection name (its filename stem).
func (c *Collection[T]) Name() string { return c.name }

// Path returns the backing file path.
func (c *Collection[T]) Path() string { return c.path }
