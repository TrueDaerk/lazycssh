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
	"unicode/utf8"
)

// DefaultCapacity is the number of lines kept per session unless configured
// otherwise.
const DefaultCapacity = 10_000

// maxLineLength bounds a single line. A remote process emitting megabytes with
// no newline - a binary file catted by accident - would otherwise grow one
// string without limit. Past this the line is committed as if a newline had
// arrived.
const maxLineLength = 64 << 10

// ClearMark is stored as its own line where the remote cleared the screen -
// an erase-display sequence or an alternate-screen switch. The scrollback is
// preserved, not wiped: the marker only records where the visible area
// restarted, and the UI decides what to draw for it. The NULs keep it from
// colliding with anything a host could print as text.
const ClearMark = "\x00cleared\x00"

// maxEscapeLength bounds an escape sequence being assembled. A sequence that
// long is not one this package interprets; it is flushed into the line and
// left to the render-time sanitizer.
const maxEscapeLength = 64

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

	pending []byte // the line being assembled, not yet terminated
	esc     []byte // an escape sequence being assembled, nil outside one
	sawCR   bool   // a '\r' was seen and its meaning is not decided yet

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
// Input is split into lines on "\n", with a preceding "\r" removed, and a
// minimal line discipline is applied to the line being assembled — enough for
// a remote readline to redraw its line, no more (full emulation is a separate
// idea; the cursor is always assumed at the end of the line):
//
//   - a bare "\r" discards the line assembled so far, which is how progress
//     bars and spinners redraw themselves; keeping every frame would fill the
//     scrollback with intermediate states of a single line,
//   - a backspace removes the last rune of the line, which is how readline
//     backs over a recalled command before writing the next one,
//   - "ESC[K" (erase right of the cursor) is consumed silently — the cursor
//     is at the end, there is nothing to its right — and its "1K"/"2K" forms
//     (erase left / erase all) discard the line.
//
// Every other escape sequence lands in the line untouched, for the render-time
// sanitizer to keep (colours) or strip (the rest).
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, c := range p {
		if b.esc != nil && b.consumeEscapeLocked(c) {
			continue
		}

		if b.sawCR {
			b.sawCR = false
			if c == '\n' {
				b.commitLocked()
				continue
			}
			// A bare carriage return: the remote is redrawing this line.
			b.pending = b.pending[:0]
		}

		switch c {
		case '\r':
			b.sawCR = true
		case '\n':
			b.commitLocked()
		case 0x1b:
			b.esc = append(b.esc[:0], c)
		case '\b':
			if n := len(b.pending); n > 0 {
				_, size := utf8.DecodeLastRune(b.pending)
				b.pending = b.pending[:n-size]
			}
		default:
			b.pending = append(b.pending, c)
			if len(b.pending) >= maxLineLength {
				b.commitLocked()
			}
		}
	}

	b.written += len(p)
	return len(p), nil
}

// consumeEscapeLocked feeds one byte to the escape sequence being assembled
// and reports whether the byte was consumed. Sequences survive Write
// boundaries because the partial sequence lives on the Buffer.
//
// Only "CSI ... K" is acted on; everything else is flushed into the pending
// line verbatim once it is complete (or turns out not to be a CSI sequence),
// for the render-time sanitizer to deal with. A line break inside a sequence
// aborts it: the remote never finished it, and holding the bytes back would
// hide real output.
func (b *Buffer) consumeEscapeLocked(c byte) bool {
	if c == '\r' || c == '\n' {
		b.flushEscapeLocked()
		return false
	}

	b.esc = append(b.esc, c)

	if len(b.esc) == 2 && c != '[' {
		// Not a CSI sequence. OSC and friends are not interpreted here;
		// hand the bytes to the line and stop collecting.
		b.flushEscapeLocked()
		return true
	}

	if len(b.esc) >= 3 && c >= 0x40 && c <= 0x7e {
		// The final byte of a CSI sequence.
		params := string(b.esc[2 : len(b.esc)-1])
		switch c {
		case 'K':
			switch params {
			case "", "0":
				// Erase right of the cursor: the cursor is at the end of
				// the line, so there is nothing to erase.
			default:
				// Erase left of the cursor, or the whole line: either way
				// the visible line is gone.
				b.pending = b.pending[:0]
			}
			b.esc = nil
			return true
		case 'J':
			// Erase display. "" and "0" erase below the cursor, which is at
			// the end in this model, so there is nothing to erase. "1" wipes
			// everything above the current line, "2" the whole screen, "3"
			// the screen and the emulator's scrollback - "clear" sends "2"
			// (and often "3"), so this is how a cleared pane happens. The
			// stored history is kept in every case; only the marker is
			// planted. Honouring "3" by wiping the ring is a deliberate
			// non-goal: on a fleet tool, history is worth more than strict
			// emulation.
			switch params {
			case "", "0":
			case "1":
				b.markClearedLocked(false)
				b.esc = nil
				return true
			default:
				b.markClearedLocked(true)
				b.esc = nil
				return true
			}
			b.esc = nil
			return true
		case 'h', 'l':
			// Alternate-screen switches. Entering (h) is a full-screen
			// program taking over - its first frame expects an empty screen.
			// Leaving (l) discards that program's frame; the primary screen
			// cannot be restored without emulation, so it clears too rather
			// than showing the alternate screen's last state as if it were
			// scrollback.
			switch params {
			case "?1049", "?1047", "?47":
				b.markClearedLocked(true)
				b.esc = nil
				return true
			}
		}
		b.flushEscapeLocked()
		return true
	}

	if len(b.esc) >= maxEscapeLength {
		b.flushEscapeLocked()
	}
	return true
}

// flushEscapeLocked gives up interpreting the collected escape bytes and
// appends them to the pending line as ordinary output.
func (b *Buffer) flushEscapeLocked() {
	b.pending = append(b.pending, b.esc...)
	b.esc = nil
	if len(b.pending) >= maxLineLength {
		b.commitLocked()
	}
}

// markClearedLocked records a clear-screen at the current position. With
// dropPending the line being assembled is erased too (whole-screen erase);
// without it the marker lands before the line (erase-above keeps the cursor's
// line). Consecutive markers collapse: a program clearing every frame must
// not fill the ring with markers instead of output.
func (b *Buffer) markClearedLocked(dropPending bool) {
	if dropPending {
		b.pending = b.pending[:0]
	}
	if b.count > 0 && b.lines[(b.start+b.count-1)%cap(b.lines)] == ClearMark {
		return
	}
	b.appendLocked(ClearMark)
}

// commitLocked moves the pending line into the ring. The caller holds the lock.
func (b *Buffer) commitLocked() {
	b.appendLocked(string(b.pending))
	b.pending = b.pending[:0]
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

	partial := string(b.pending)
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
	b.esc = nil
	b.pending = b.pending[:0]
}
