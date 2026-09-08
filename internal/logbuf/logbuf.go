// Package logbuf keeps the most recent log lines in memory so the Settings screen can
// show them without the user having to find a terminal or a log file.
package logbuf

import (
	"io"
	"strings"
	"sync"
)

// DefaultCapacity is how many lines are retained. A few hundred covers the useful
// window (startup, the last scan, recent playback) without holding meaningful memory.
const DefaultCapacity = 500

// Buffer is a fixed-size ring of log lines that also forwards everything it receives
// to an underlying writer, so the console output is unchanged.
type Buffer struct {
	mu    sync.RWMutex
	lines []string
	next  int
	full  bool

	out io.Writer
	// partial accumulates bytes until a newline arrives, because a single log call can
	// reach Write in several pieces.
	partial strings.Builder
}

// New creates a buffer that mirrors writes to out.
func New(out io.Writer, capacity int) *Buffer {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Buffer{
		lines: make([]string, capacity),
		out:   out,
	}
}

// Write satisfies io.Writer so the buffer can be installed with log.SetOutput.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.partial.Write(p)
	text := b.partial.String()

	// Keep any trailing fragment for the next call; only complete lines are stored.
	if idx := strings.LastIndexByte(text, '\n'); idx >= 0 {
		complete := text[:idx]
		remainder := text[idx+1:]

		b.partial.Reset()
		b.partial.WriteString(remainder)

		for _, line := range strings.Split(complete, "\n") {
			if line == "" {
				continue
			}
			b.lines[b.next] = line
			b.next = (b.next + 1) % len(b.lines)
			if b.next == 0 {
				b.full = true
			}
		}
	}
	b.mu.Unlock()

	if b.out != nil {
		return b.out.Write(p)
	}
	return len(p), nil
}

// Lines returns the retained lines, oldest first. A limit of 0 returns everything.
func (b *Buffer) Lines(limit int) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var out []string
	if b.full {
		out = append(out, b.lines[b.next:]...)
		out = append(out, b.lines[:b.next]...)
	} else {
		out = append(out, b.lines[:b.next]...)
	}

	// Drop any empty slots left by a partially filled ring.
	trimmed := out[:0]
	for _, line := range out {
		if line != "" {
			trimmed = append(trimmed, line)
		}
	}
	out = trimmed

	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// Clear discards everything retained so far.
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.lines {
		b.lines[i] = ""
	}
	b.next = 0
	b.full = false
	b.partial.Reset()
}
