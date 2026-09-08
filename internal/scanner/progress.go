package scanner

import "sync"

// ProgressHub fans scan progress out to any number of SSE subscribers.
//
// Subscribers get a buffered channel and are never blocked on: a client that stops
// reading (a closed laptop lid, a stalled connection) drops updates instead of
// wedging the scan that is producing them.
type ProgressHub struct {
	mu     sync.RWMutex
	nextID int
	subs   map[int]chan Progress
}

// NewProgressHub creates an empty hub.
func NewProgressHub() *ProgressHub {
	return &ProgressHub{subs: make(map[int]chan Progress)}
}

// Subscribe registers a listener and returns its channel plus an unsubscribe func.
func (h *ProgressHub) Subscribe() (<-chan Progress, func()) {
	ch := make(chan Progress, 32)

	h.mu.Lock()
	id := h.nextID
	h.nextID++
	h.subs[id] = ch
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		if existing, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(existing)
		}
		h.mu.Unlock()
	}
}

// Broadcast delivers an update to every subscriber, skipping any whose buffer is full.
func (h *ProgressHub) Broadcast(p Progress) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subs {
		select {
		case ch <- p:
		default: // slow consumer: drop this update rather than stall the scanner
		}
	}
}

// SubscriberCount reports how many listeners are attached, for diagnostics.
func (h *ProgressHub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subs)
}
