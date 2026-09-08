package metadata

import (
	"context"
	"sync"
	"time"
)

// rateLimiter is a simple token bucket: at most n operations per interval.
//
// TMDb throttles aggressively, and a scan of a large library would otherwise burst
// straight into a 429 and stall.
type rateLimiter struct {
	mu       sync.Mutex
	tokens   int
	capacity int
	interval time.Duration
	last     time.Time
}

func newRateLimiter(perInterval int, interval time.Duration) *rateLimiter {
	if perInterval < 1 {
		perInterval = 1
	}
	return &rateLimiter{
		tokens:   perInterval,
		capacity: perInterval,
		interval: interval,
		last:     time.Now(),
	}
}

// wait blocks until a token is available or the context is cancelled.
func (r *rateLimiter) wait(ctx context.Context) error {
	for {
		r.mu.Lock()

		// Refill proportionally to elapsed time.
		now := time.Now()
		elapsed := now.Sub(r.last)
		if elapsed >= r.interval {
			r.tokens = r.capacity
			r.last = now
		}

		if r.tokens > 0 {
			r.tokens--
			r.mu.Unlock()
			return nil
		}

		sleep := r.interval - elapsed
		r.mu.Unlock()

		if sleep <= 0 {
			sleep = time.Millisecond
		}
		timer := time.NewTimer(sleep)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}
