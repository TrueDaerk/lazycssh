// Package scrollback stores the recent output of one SSH session in a bounded
// ring buffer.
//
// The rule this package exists to enforce: a chatty host must never stall the
// UI. Writes are lock-protected but never block on a reader, never allocate
// without bound, and never fail. When the buffer is full the oldest lines are
// dropped and counted.
package scrollback

import (
	"strings"
	"sync"
)

// DefaultCapacity is the number of lines kept per session unless configured
// otherwise.
const DefaultCapacity = 10_000

// maxLineLength bounds a single line. A remote process emitting megabytes with
// no newline - a binary file catted by accident - would otherwise grow one
// string without limit. Past this the line is committed as if a newline had
// arrived.
const maxLineLength = 64 << 10

// Buffer is a bounded, line-oriented ring buffer. The zero value is not usable;
// construct one with [New].
//
// A Buffer is safe for concurrent use: one writer goroutine per session and the
// UI reading snapshots.
type Buffer struct {
	mu sync.Mutex

	lines []string // ring storage, len == capacity once full
	start int      // index of the oldest line
	count int      // number of lines currently stored

	pending strings.Builder // the line being assembled, not yet terminated
	sawCR   bool            // a '\r' was seen and its meaning is not decided yet

	dropped int
	written int
}

// New returns a Buffer keeping at most capacity lines. A capacity below one is
// raised to [DefaultCapacity], so a zero value from an unset config field does
// not silently discard all output.
func New(capacity int) *Buffer {
	if capacity < 1 {
		capacity = DefaultCapacity
	}
	return &Buffer{lines: make([]string, 0, capacity)}
}

// Capacity returns the maximum number of lines retained.
func (b *Buffer) Capacity() int { return cap(b.lines) }

// Write appends raw session output. It implements [io.Writer] and always
// reports success: there is no failure mode a session reader goroutine could
// usefully react to, and blocking it would stall the host it is reading from.
//
// Input is split into lines on "\n", with a preceding "\r" removed. A "\r" not
// followed by "\n" discards the line assembled so far, which is how progress
// bars and spinners redraw themselves; keeping every frame would fill the
// scrollback with intermediate states of a single line.
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, c := range p {
		if b.sawCR {
			b.sawCR = false
			if c == '\n' {
				b.commitLocked()
				continue
			}
			// A bare carriage return: the remote is redrawing this line.
			b.pending.Reset()
		}

		switch c {
		case '\r':
			b.sawCR = true
		case '\n':
			b.commitLocked()
		default:
			b.pending.WriteByte(c)
			if b.pending.Len() >= maxLineLength {
				b.commitLocked()
			}
		}
	}

	b.written += len(p)
	return len(p), nil
}

// commitLocked moves the pending line into the ring. The caller holds the lock.
func (b *Buffer) commitLocked() {
	b.appendLocked(b.pending.String())
	b.pending.Reset()
}

// appendLocked stores one line, evicting the oldest when full.
func (b *Buffer) appendLocked(line string) {
	capacity := cap(b.lines)

	if b.count < capacity {
		b.lines = append(b.lines, line)
		b.count++
		return
	}

	b.lines[b.start] = line
	b.start = (b.start + 1) % capacity
	b.dropped++
}

// Lines returns a snapshot of the retained lines, oldest first. A line still
// being assembled - a shell prompt with no trailing newline, for example - is
// included as the last element, because the UI must show a prompt the moment it
// arrives rather than when the next line completes.
//
// The result is a fresh slice; the caller may keep or modify it.
func (b *Buffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()

	partial := b.pending.String()
	out := make([]string, 0, b.count+1)

	capacity := cap(b.lines)
	for i := 0; i < b.count; i++ {
		out = append(out, b.lines[(b.start+i)%capacity])
	}
	if partial != "" {
		out = append(out, partial)
	}
	return out
}

// String renders the buffer as it would appear on screen.
func (b *Buffer) String() string {
	return strings.Join(b.Lines(), "\n")
}

// Len returns the number of complete lines retained, excluding any line still
// being assembled.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.count
}

// Dropped returns how many lines have been evicted to stay within capacity. The
// UI shows this so that truncated scrollback is visible rather than silent.
func (b *Buffer) Dropped() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// Written returns the total number of bytes ever written, including output that
// has since been dropped.
func (b *Buffer) Written() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.written
}

// Reset clears the buffer and its counters.
func (b *Buffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lines = b.lines[:0]
	b.start = 0
	b.count = 0
	b.dropped = 0
	b.written = 0
	b.sawCR = false
	b.pending.Reset()
}
