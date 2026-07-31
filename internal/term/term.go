// Package term emulates a terminal for one session's output.
//
// It wraps the charmbracelet vt emulator: the session's reader goroutine feeds
// raw output bytes in, and the UI reads screen state back — the visible grid,
// the scrollback history that scrolled off it, the cursor, and whether the
// remote app switched to the alternate screen.
//
// Input also flows through here: SendKey encodes a key press the way the
// emulated terminal's current modes demand (application cursor keys,
// bracketed paste, …) and the encoded bytes come out of the reply pipe, the
// same path that carries the emulator's answers to terminal queries. The
// session wires that pipe to its stdin, so one mechanism serves both.
package term

import (
	"bytes"
	"io"
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// KeyEvent is a key press for SendKey. It is the ultraviolet event type the
// vt emulator consumes; the UI converts bubbletea key messages into it.
type KeyEvent = uv.KeyPressEvent

// KeyMod is the modifier bitmask of a [KeyEvent]. It shares values with the
// bubbletea v2 modifiers, so a tea modifier can be cast directly.
type KeyMod = uv.KeyMod

// Key modifiers for building KeyEvents.
const (
	ModShift = uv.ModShift
	ModAlt   = uv.ModAlt
	ModCtrl  = uv.ModCtrl
	ModSuper = uv.ModSuper
	ModMeta  = uv.ModMeta
)

// Key codes for building KeyEvents. They share values with the bubbletea v2
// key codes (both are ultraviolet's), so a tea key code can be passed through
// unchanged; these names exist for callers that build events by hand.
const (
	KeyUp        = uv.KeyUp
	KeyDown      = uv.KeyDown
	KeyLeft      = uv.KeyLeft
	KeyRight     = uv.KeyRight
	KeyBackspace = uv.KeyBackspace
	KeyDelete    = uv.KeyDelete
	KeyEnter     = uv.KeyEnter
	KeyTab       = uv.KeyTab
	KeyEscape    = uv.KeyEscape
	KeySpace     = uv.KeySpace
	KeyHome      = uv.KeyHome
	KeyEnd       = uv.KeyEnd
	KeyPgUp      = uv.KeyPgUp
	KeyPgDown    = uv.KeyPgDown
)

// Emulator is the terminal emulator for one session. It is safe for
// concurrent use: the session's reader goroutine writes while the UI reads.
type Emulator struct {
	vt *vt.SafeEmulator

	// gridMu serializes every access to the vt state that spans more than one
	// call — a resize snapshotting rows, a reader building a history line —
	// against the write path. The vt wrapper's own lock only protects single
	// calls.
	gridMu sync.Mutex

	// Resize machinery state; see resize.go. All guarded by gridMu.
	reserve      []uv.Line
	reserveW     int
	shrinkPushed int
	shrinkMark   int
	reflowCache  []uv.Line

	// ed3 counts the matched prefix bytes of the ESC[3J guard in Write.
	ed3 int

	mu           sync.Mutex
	onReply      func([]byte)
	cursorHidden bool
	written      int
}

// New returns an emulator with the given screen size in cells. Dimensions
// below one cell are clamped so a pane that has not been measured yet cannot
// produce a zero-size grid.
func New(width, height int) *Emulator {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	e := &Emulator{vt: vt.NewSafeEmulator(width, height)}
	// The vt emulator reports cursor visibility only through a callback, so it
	// is mirrored here for the renderer to read. The callback runs inside
	// Write, which never holds e.mu.
	e.vt.SetCallbacks(vt.Callbacks{CursorVisibility: func(visible bool) {
		e.mu.Lock()
		e.cursorHidden = !visible
		e.mu.Unlock()
	}})
	// The vt emulator answers terminal queries through an unbuffered pipe and
	// Write blocks until the answer is consumed. Drain it unconditionally, or
	// the first `vim` would freeze the session's reader goroutine.
	go e.drainReplies()
	return e
}

// ed3Sequence is the erase-scrollback control the guard in Write removes: on
// a fleet tool the history is worth more than strict emulation, so a remote
// `clear` empties the screen (ED 2, which pushes the visible rows into the
// scrollback) but never wipes what the host said before.
var ed3Sequence = []byte("\x1b[3J")

// Write feeds session output bytes into the emulator. It satisfies io.Writer
// and never fails: parsing happens in memory and malformed sequences are the
// remote host's problem, not the caller's.
//
// One sequence never reaches the parser: ESC[3J (erase scrollback). See
// [ed3Sequence]. The guard is a byte state machine because the sequence can
// be split across any read boundary; a partial match is held back until the
// next write decides.
func (e *Emulator) Write(p []byte) (int, error) {
	e.gridMu.Lock()
	defer e.gridMu.Unlock()

	n := len(p)
	e.mu.Lock()
	e.written += n
	e.mu.Unlock()

	for len(p) > 0 {
		if e.ed3 > 0 {
			// Continue a partial match from the previous write.
			k := 0
			for k < len(p) && e.ed3+k < len(ed3Sequence) && p[k] == ed3Sequence[e.ed3+k] {
				k++
			}
			if e.ed3+k == len(ed3Sequence) {
				e.ed3 = 0
				p = p[k:]
				continue
			}
			if k == len(p) {
				e.ed3 += k
				return n, nil
			}
			// Mismatch: the held prefix was real output after all.
			_, _ = e.vt.Write(ed3Sequence[:e.ed3])
			e.ed3 = 0
			continue
		}

		i := bytes.Index(p, ed3Sequence)
		if i < 0 {
			// No full match; hold back a trailing partial one.
			held := 0
			for k := min(len(ed3Sequence)-1, len(p)); k > 0; k-- {
				if bytes.Equal(p[len(p)-k:], ed3Sequence[:k]) {
					held = k
					break
				}
			}
			_, _ = e.vt.Write(p[:len(p)-held])
			e.ed3 = held
			return n, nil
		}
		_, _ = e.vt.Write(p[:i])
		p = p[i+len(ed3Sequence):]
	}
	return n, nil
}

// Size reports the current screen size in cells.
func (e *Emulator) Size() (width, height int) {
	return e.vt.Width(), e.vt.Height()
}

// IsAltScreen reports whether the remote app has switched to the alternate
// screen — the signal that a full-screen app (vim, htop, less) is running.
func (e *Emulator) IsAltScreen() bool {
	return e.vt.IsAltScreen()
}

// Render returns the visible screen as styled text, one line per row.
func (e *Emulator) Render() string {
	e.gridMu.Lock()
	defer e.gridMu.Unlock()
	return e.vt.Render()
}

// SetHistorySize bounds the retained history to n lines. Older lines are
// dropped when the cap is exceeded; the writer never blocks on it.
func (e *Emulator) SetHistorySize(n int) {
	if n <= 0 {
		return
	}
	e.gridMu.Lock()
	defer e.gridMu.Unlock()
	e.vt.SetScrollbackSize(n)
}

// HistoryLen is the number of lines that have scrolled off the screen and are
// retained. The retention is bounded (the vt default, 10k lines); older lines
// are dropped, never blocking the writer.
func (e *Emulator) HistoryLen() int {
	e.gridMu.Lock()
	defer e.gridMu.Unlock()
	if e.vt.IsAltScreen() {
		return 0
	}
	return e.vt.ScrollbackLen()
}

// HistoryFull reports whether the retention cap has been reached — the oldest
// output has been dropped.
func (e *Emulator) HistoryFull() bool {
	e.gridMu.Lock()
	defer e.gridMu.Unlock()
	sb := e.vt.Scrollback()
	return sb.Len() > 0 && sb.Len() >= sb.MaxLines()
}

// HistoryLine returns one scrolled-off line as styled text. Index 0 is the
// oldest retained line. Out-of-range indexes return the empty string.
func (e *Emulator) HistoryLine(i int) string {
	e.gridMu.Lock()
	defer e.gridMu.Unlock()
	line := e.vt.Scrollback().Line(i)
	if line == nil {
		return ""
	}
	return line.Render()
}

// CursorVisible reports whether the remote app wants the cursor drawn.
// Full-screen apps hide it while repainting (CSI ?25l) and show it again when
// they settle.
func (e *Emulator) CursorVisible() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.cursorHidden
}

// CursorPosition returns the cursor cell, zero-based, column then row.
func (e *Emulator) CursorPosition() (x, y int) {
	pos := e.vt.CursorPosition()
	return pos.X, pos.Y
}

// HasOutput reports whether any bytes were ever written — "has this session
// said anything yet", which is what decides whether injected text needs a
// leading line break.
func (e *Emulator) HasOutput() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.written > 0
}

// Text returns the retained history and the visible screen as plain text,
// one line per row, ANSI stripped and trailing blank rows trimmed. It is the
// whole pane content as a user would read it — primarily for tests and
// clipboard use, not for rendering.
func (e *Emulator) Text() string {
	e.gridMu.Lock()
	defer e.gridMu.Unlock()

	var lines []string
	if !e.vt.IsAltScreen() {
		sb := e.vt.Scrollback()
		for i := 0; i < sb.Len(); i++ {
			lines = append(lines, ansi.Strip(sb.Line(i).Render()))
		}
	}
	for _, row := range strings.Split(e.vt.Render(), "\n") {
		lines = append(lines, ansi.Strip(row))
	}
	for len(lines) > 0 && strings.TrimRight(lines[len(lines)-1], " ") == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// SendKey encodes one key press the way this terminal's current modes demand
// and emits the bytes on the reply pipe. What reaches the remote host depends
// on the emulator state — application cursor keys, keypad mode — which is the
// point: the hand-written key table this replaces could not know.
func (e *Emulator) SendKey(k KeyEvent) {
	e.vt.SendKey(k)
}

// Paste sends text as a paste: wrapped in bracketed-paste markers when the
// remote app turned that mode on, plain bytes otherwise.
func (e *Emulator) Paste(text string) {
	e.vt.Paste(text)
}

// SetReplyHandler registers the function that receives the emulator's
// host-bound bytes: answers to terminal queries from the remote app (device
// attributes, cursor position reports) and the encodings SendKey and Paste
// produce. The bytes must reach the session's stdin; wiring that up is the
// caller's job. Without a handler, replies are drained and dropped.
//
// The handler is called from the emulator's own goroutine and owns the slice
// it is given.
func (e *Emulator) SetReplyHandler(fn func([]byte)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onReply = fn
}

// drainReplies pumps host-bound bytes out of the vt emulator for the lifetime
// of the emulator. It exits when Close shuts the pipe down.
func (e *Emulator) drainReplies() {
	buf := make([]byte, 4096)
	for {
		n, err := e.vt.Read(buf)
		if n > 0 {
			e.mu.Lock()
			fn := e.onReply
			e.mu.Unlock()
			if fn != nil {
				out := make([]byte, n)
				copy(out, buf[:n])
				fn(out)
			}
		}
		if err != nil {
			return
		}
	}
}

// Close releases the emulator and stops the reply drain. The emulator's owner
// closes it when the pane is gone for good — not on a reconnect, which hands
// the same emulator to the next session so the pane keeps its history.
// Close is idempotent.
func (e *Emulator) Close() error {
	// Not vt.Close: it flips an unsynchronized bool that the drain goroutine's
	// Read is checking concurrently, which the race detector rightly flags.
	// Closing the reply pipe directly unblocks the drain the same way without
	// touching that flag.
	if pw, ok := e.vt.InputPipe().(*io.PipeWriter); ok {
		return pw.CloseWithError(io.EOF)
	}
	return e.vt.Close()
}
