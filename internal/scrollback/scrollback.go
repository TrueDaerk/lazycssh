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

// maxEscapeLength bounds a CSI sequence being assembled. A sequence that long
// is not one this package interprets; it is flushed into the line and left to
// the render-time sanitizer.
const maxEscapeLength = 64

// maxStringSequence bounds an OSC / DCS style string sequence being consumed.
// These carry titles, clipboard payloads or shell-integration markers - never
// text - so an unterminated one is dropped at the bound rather than flushed
// into the line, where its payload would overwrite real output.
const maxStringSequence = 4096

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

	// The line being assembled is a logical line: on the remote terminal it
	// occupies len/width screen rows, and the cursor may sit anywhere on it.
	// That is what lets a remote readline redraw a recalled multi-row command
	// without its intermediate states being committed as output (issue #178).
	pending []byte // the line being assembled, not yet terminated
	cursor  int    // cell the next rune lands in, 0-based within pending
	cells   int    // printable cells in pending (escapes are zero width)
	width   int    // remote terminal columns, 0 when unknown

	esc      []byte // an escape sequence being assembled, nil outside one
	inString bool   // the sequence in esc is an OSC/DCS-style string sequence
	runeBuf  []byte // a UTF-8 rune split across writes

	// memo caches one cell → byte-offset mapping into pending, so a readline
	// overwriting a long recalled line does not rescan the prefix for every
	// rune. It survives cursor movement (the mapping describes pending, not
	// the cursor) and dies on any edit that shifts bytes.
	memoCell int
	memoByte int

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

// SetWidth tells the buffer how many columns the remote terminal has. The
// session calls it on connect and on every resize, in lockstep with the PTY.
// The width is what turns cursor-up and carriage-return into positions on the
// pending logical line; without it (width 0) vertical movement is ignored and
// carriage return means column zero of the whole line.
func (b *Buffer) SetWidth(cols int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cols < 0 {
		cols = 0
	}
	b.width = cols
}

// Write appends raw session output. It implements [io.Writer] and always
// reports success: there is no failure mode a session reader goroutine could
// usefully react to, and blocking it would stall the host it is reading from.
//
// Input is split into logical lines on "\n". The line being assembled carries
// a cursor, and enough of the terminal's movement repertoire is honoured for a
// remote readline to redraw a recalled command - including one that wraps over
// several screen rows - without leaving its intermediate states behind:
//
//   - printable runes overwrite the cell under the cursor and advance it,
//   - "\r" returns the cursor to column zero of its current screen row,
//   - a backspace moves the cursor one cell left (it erases nothing - that is
//     what the following overwrite or erase sequence is for),
//   - cursor movement (CUU/CUD/CUF/CUB/CHA) repositions the cursor on the
//     pending line, with rows mapped through the terminal width,
//   - "ESC[K" erases within the cursor's screen row,
//   - "\n" commits the pending line when the cursor is on its last screen row;
//     on an upper row it only moves the cursor down one row, because that is
//     readline stepping through a multi-row edit, not output.
//
// OSC and the other string sequences are consumed and dropped - they carry
// titles and shell-integration markers, never text. SGR and any unrecognized
// CSI sequence land in the line untouched, zero cells wide, for the
// render-time sanitizer to keep (colours) or strip (the rest).
func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, c := range p {
		if b.esc != nil && b.consumeEscapeLocked(c) {
			continue
		}
		if len(b.runeBuf) > 0 {
			if c >= 0x80 && c < 0xc0 { // a UTF-8 continuation byte
				b.runeBuf = append(b.runeBuf, c)
				if utf8.FullRune(b.runeBuf) || len(b.runeBuf) >= utf8.UTFMax {
					b.placeLocked(b.runeBuf)
					b.runeBuf = b.runeBuf[:0]
				}
				continue
			}
			// The rune never completed; keep its bytes as they came and let
			// the current byte take its normal path below.
			b.placeLocked(b.runeBuf)
			b.runeBuf = b.runeBuf[:0]
		}

		switch {
		case c == '\r':
			b.cursor -= b.columnLocked()
		case c == '\n':
			b.lineFeedLocked()
		case c == 0x1b:
			b.esc = append(b.esc[:0], c)
			b.inString = false
		case c == '\b':
			if b.cursor > 0 {
				b.cursor--
			}
		case c == '\t':
			// Kept as one cell; the render-time sanitizer expands it.
			b.placeLocked([]byte{c})
		case c < 0x20 || c == 0x7f:
			// BEL, SO/SI and friends: zero width on a terminal, dropped here.
		case c >= utf8.RuneSelf:
			b.runeBuf = append(b.runeBuf[:0], c)
			if utf8.FullRune(b.runeBuf) {
				b.placeLocked(b.runeBuf)
				b.runeBuf = b.runeBuf[:0]
			}
		default:
			b.placeLocked([]byte{c})
		}
	}

	b.written += len(p)
	return len(p), nil
}

// columnLocked returns the cursor's column within its current screen row.
func (b *Buffer) columnLocked() int {
	if b.width > 0 {
		return b.cursor % b.width
	}
	return b.cursor
}

// rowStartLocked returns the cell index where the cursor's screen row begins.
func (b *Buffer) rowStartLocked() int {
	return b.cursor - b.columnLocked()
}

// lineFeedLocked handles "\n": commit, or step down one screen row when the
// cursor is on an upper row of a multi-row pending line - which only happens
// when the remote moved it up there, i.e. during a readline redraw.
func (b *Buffer) lineFeedLocked() {
	if b.width > 0 {
		if cells := b.effectiveCellsLocked(); cells > 0 {
			lastRow := (cells - 1) / b.width
			if b.cursor/b.width < lastRow {
				b.cursor += b.width
				return
			}
		}
	}
	b.commitLocked()
}

// placeLocked writes one rune at the cursor cell and advances the cursor,
// overwriting whatever cell was there. Writing past the end pads with spaces,
// which is what a cursor parked beyond the text means on a terminal.
func (b *Buffer) placeLocked(r []byte) {
	if b.cursor >= b.cells {
		for b.cells < b.cursor {
			b.pending = append(b.pending, ' ')
			b.cells++
		}
		b.pending = append(b.pending, r...)
		b.cells++
		b.cursor++
	} else {
		start, end := b.cellRangeLocked(b.cursor)
		if end-start == len(r) {
			copy(b.pending[start:end], r)
			b.memoCell, b.memoByte = b.cursor+1, end
		} else {
			b.pending = splice(b.pending, start, end, r)
			b.memoCell, b.memoByte = b.cursor+1, start+len(r)
		}
		b.cursor++
	}
	if len(b.pending) >= maxLineLength {
		b.commitLocked()
	}
}

// cellRangeLocked returns the byte range of one cell's rune in pending,
// skipping escape sequences, which occupy bytes but no cells. A cell at or
// past the end returns an empty range at the end.
func (b *Buffer) cellRangeLocked(cell int) (start, end int) {
	i, seen := 0, 0
	if b.memoByte > 0 && b.memoCell <= cell && b.memoByte <= len(b.pending) {
		i, seen = b.memoByte, b.memoCell
	}
	for i < len(b.pending) {
		if b.pending[i] == 0x1b {
			i += embeddedEscapeLen(b.pending[i:])
			continue
		}
		_, size := utf8.DecodeRune(b.pending[i:])
		if seen == cell {
			return i, i + size
		}
		seen++
		i += size
	}
	return len(b.pending), len(b.pending)
}

// embeddedEscapeLen measures an escape sequence stored in pending. Only CSI
// and short non-string escapes ever land there - string sequences are dropped
// in Write - but an unterminated tail still measures to the end.
func embeddedEscapeLen(s []byte) int {
	if len(s) < 2 {
		return len(s)
	}
	if s[1] == '[' {
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	}
	for i := 1; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x2f {
			return i + 1
		}
	}
	return len(s)
}

// splice replaces p[start:end] with r.
func splice(p []byte, start, end int, r []byte) []byte {
	if len(r) == end-start {
		copy(p[start:end], r)
		return p
	}
	out := make([]byte, 0, len(p)-(end-start)+len(r))
	out = append(out, p[:start]...)
	out = append(out, r...)
	out = append(out, p[end:]...)
	return out
}

// eraseCellsLocked overwrites the cells in [from, to) with spaces. The blank
// tail this may leave is not stored away here - trailing spaces fall off at
// commit and snapshot time, where the cursor says which of them still matter.
// One pass over pending, whatever the range: erasure must stay cheap even
// against a pathological line.
func (b *Buffer) eraseCellsLocked(from, to int) {
	if from < 0 {
		from = 0
	}
	if to > b.cells {
		to = b.cells
	}
	if from >= to {
		return
	}

	out := b.pending[:0:0]
	i, cell := 0, 0
	for i < len(b.pending) {
		if b.pending[i] == 0x1b {
			n := embeddedEscapeLen(b.pending[i:])
			out = append(out, b.pending[i:i+n]...)
			i += n
			continue
		}
		_, size := utf8.DecodeRune(b.pending[i:])
		if cell >= from && cell < to {
			out = append(out, ' ')
		} else {
			out = append(out, b.pending[i:i+size]...)
		}
		cell++
		i += size
	}
	b.pending = out
	b.memoCell, b.memoByte = 0, 0
}

// effectiveCellsLocked is where the pending line's content ends: trailing
// spaces do not count unless the cursor sits among them. The distinction is
// what separates erase debris - blanked rows of an abandoned readline edit -
// from spaces the host printed and stayed behind.
func (b *Buffer) effectiveCellsLocked() int {
	trailing := 0
	for i := len(b.pending) - 1; i >= 0 && b.pending[i] == ' '; i-- {
		trailing++
	}
	end := b.cells - trailing
	if b.cursor > end {
		end = b.cursor
	}
	if end > b.cells {
		end = b.cells
	}
	return end
}

// consumeEscapeLocked feeds one byte to the escape sequence being assembled
// and reports whether the byte was consumed. Sequences survive Write
// boundaries because the partial sequence lives on the Buffer.
//
// Cursor movement and erasure within the pending line are acted on; string
// sequences (OSC, DCS and friends) are consumed and dropped; everything else
// is flushed into the pending line verbatim once it is complete, zero cells
// wide, for the render-time sanitizer to deal with. A line break inside a
// non-string sequence aborts it: the remote never finished it, and holding
// the bytes back would hide real output.
func (b *Buffer) consumeEscapeLocked(c byte) bool {
	if b.inString {
		// Consume until BEL or ST (ESC \), then drop the whole sequence.
		if c == '\a' || (c == '\\' && b.esc[len(b.esc)-1] == 0x1b) {
			b.esc = nil
			return true
		}
		b.esc = append(b.esc, c)
		if len(b.esc) >= maxStringSequence {
			b.esc = nil
		}
		return true
	}

	if c == '\r' || c == '\n' {
		b.flushEscapeLocked()
		return false
	}

	b.esc = append(b.esc, c)

	if len(b.esc) == 2 {
		switch c {
		case '[':
			return true
		case ']', 'P', 'X', '^', '_':
			b.inString = true
			return true
		default:
			// A short escape (charset selection, keypad modes): not
			// interpreted; hand the bytes to the line and stop collecting.
			b.flushEscapeLocked()
			return true
		}
	}

	if len(b.esc) >= 3 && c >= 0x40 && c <= 0x7e {
		// The final byte of a CSI sequence.
		params := string(b.esc[2 : len(b.esc)-1])
		switch c {
		case 'A', 'B': // cursor up / down: ± n screen rows
			n, ok := escapeParam(params)
			if !ok {
				break
			}
			if b.width > 0 {
				if c == 'A' {
					if next := b.cursor - n*b.width; next >= 0 {
						b.cursor = next
					} else {
						b.cursor = b.columnLocked()
					}
				} else {
					b.cursor += n * b.width
				}
			}
			b.esc = nil
			return true
		case 'C': // cursor forward, clamped to the row's last column
			n, ok := escapeParam(params)
			if !ok {
				break
			}
			b.cursor += n
			if b.width > 0 {
				// rowStart is computed before the move above, so recompute
				// against the original row.
				rowStart := (b.cursor - n) - (b.cursor-n)%b.width
				if b.cursor > rowStart+b.width-1 {
					b.cursor = rowStart + b.width - 1
				}
			}
			b.esc = nil
			return true
		case 'D': // cursor back, clamped to the row's first column
			n, ok := escapeParam(params)
			if !ok {
				break
			}
			rowStart := b.rowStartLocked()
			b.cursor -= n
			if b.cursor < rowStart {
				b.cursor = rowStart
			}
			b.esc = nil
			return true
		case 'G': // cursor to absolute column within the row
			n, ok := escapeParam(params)
			if !ok {
				break
			}
			rowStart := b.rowStartLocked()
			b.cursor = rowStart + n - 1
			if b.width > 0 && b.cursor > rowStart+b.width-1 {
				b.cursor = rowStart + b.width - 1
			}
			b.esc = nil
			return true
		case 'K':
			rowStart := b.rowStartLocked()
			rowEnd := b.cells
			if b.width > 0 {
				rowEnd = rowStart + b.width
			}
			switch params {
			case "", "0":
				b.eraseCellsLocked(b.cursor, rowEnd)
			case "1":
				b.eraseCellsLocked(rowStart, b.cursor+1)
			case "2":
				b.eraseCellsLocked(rowStart, rowEnd)
			default:
				b.flushEscapeLocked()
				return true
			}
			b.esc = nil
			return true
		case 'J':
			// Erase display. "" and "0" erase below the cursor: the pending
			// line past the cursor is gone, committed lines above it stay.
			// "1" wipes everything above the current line, "2" the whole
			// screen, "3" the screen and the emulator's scrollback - "clear"
			// sends "2" (and often "3"), so this is how a cleared pane
			// happens. The stored history is kept in every case; only the
			// marker is planted. Honouring "3" by wiping the ring is a
			// deliberate non-goal: on a fleet tool, history is worth more
			// than strict emulation.
			switch params {
			case "", "0":
				start, _ := b.cellRangeLocked(b.cursor)
				b.pending = b.pending[:start]
				if b.cells > b.cursor {
					b.cells = b.cursor
				}
				b.memoCell, b.memoByte = 0, 0
			case "1":
				b.markClearedLocked(false)
			default:
				b.markClearedLocked(true)
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

// escapeParam parses a CSI sequence's single numeric parameter, defaulting to
// one. Multi-parameter or otherwise unexpected forms report false, and the
// sequence is flushed verbatim instead of being half-understood.
func escapeParam(params string) (int, bool) {
	if params == "" {
		return 1, true
	}
	n := 0
	for i := 0; i < len(params); i++ {
		c := params[i]
		if c < '0' || c > '9' || n > 9999 {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 {
		n = 1
	}
	return n, true
}

// flushEscapeLocked gives up interpreting the collected escape bytes and
// inserts them into the pending line at the cursor as ordinary output, zero
// cells wide - an SGR belongs where the cursor is, not at the line's end.
func (b *Buffer) flushEscapeLocked() {
	if len(b.esc) > 0 {
		start, _ := b.cellRangeLocked(b.cursor)
		b.pending = splice(b.pending, start, start, b.esc)
		b.memoCell, b.memoByte = 0, 0
	}
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
		b.cursor = 0
		b.cells = 0
		b.memoCell, b.memoByte = 0, 0
	}
	if b.count > 0 && b.lines[(b.start+b.count-1)%cap(b.lines)] == ClearMark {
		return
	}
	b.appendLocked(ClearMark)
}

// commitLocked moves the pending line into the ring, without the erase debris
// past its effective end - trailing spaces the cursor left behind. Spaces the
// host printed and stood on survive, because the cursor is what marks them as
// content. The caller holds the lock.
func (b *Buffer) commitLocked() {
	b.appendLocked(string(b.trimmedPendingLocked()))
	b.pending = b.pending[:0]
	b.cursor = 0
	b.cells = 0
	b.memoCell, b.memoByte = 0, 0
}

// trimmedPendingLocked is the pending line up to its effective end. Trailing
// spaces are single bytes, so the cut is pure arithmetic.
func (b *Buffer) trimmedPendingLocked() []byte {
	if drop := b.cells - b.effectiveCellsLocked(); drop > 0 {
		return b.pending[:len(b.pending)-drop]
	}
	return b.pending
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

	partial := string(b.trimmedPendingLocked())
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

// Reset clears the buffer and its counters. The width survives: it describes
// the terminal, not the content.
func (b *Buffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.lines = b.lines[:0]
	b.start = 0
	b.count = 0
	b.dropped = 0
	b.written = 0
	b.esc = nil
	b.inString = false
	b.runeBuf = nil
	b.pending = b.pending[:0]
	b.cursor = 0
	b.cells = 0
	b.memoCell, b.memoByte = 0, 0
}
