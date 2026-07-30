// Package term emulates a terminal for one session's output.
//
// It wraps the charmbracelet vt emulator: the session's reader goroutine feeds
// raw output bytes in, and the UI reads screen state back — most importantly
// whether the remote app switched to the alternate screen, which is what tells
// a pane to render a live grid instead of scrollback text.
package term

import (
	"io"
	"sync"

	"github.com/charmbracelet/x/vt"
)

// Emulator is the terminal emulator for one session. It is safe for
// concurrent use: the session's reader goroutine writes while the UI reads.
type Emulator struct {
	vt *vt.SafeEmulator

	mu           sync.Mutex
	onReply      func([]byte)
	cursorHidden bool
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

// Write feeds session output bytes into the emulator. It satisfies io.Writer
// and never fails: parsing happens in memory and malformed sequences are the
// remote host's problem, not the caller's.
func (e *Emulator) Write(p []byte) (int, error) {
	return e.vt.Write(p)
}

// Resize changes the emulator's screen size. It is called in lockstep with
// the pane geometry and the remote PTY, so the emulated app and the real one
// agree on the window.
func (e *Emulator) Resize(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	e.vt.Resize(width, height)
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
	return e.vt.Render()
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

// SetReplyHandler registers the function that receives the emulator's answers
// to terminal queries from the remote app (device attributes, cursor position
// reports). The bytes must reach the session's stdin for full-screen apps to
// start cleanly; wiring that up is the caller's job. Without a handler,
// replies are drained and dropped.
//
// The handler is called from the emulator's own goroutine and owns the slice
// it is given.
func (e *Emulator) SetReplyHandler(fn func([]byte)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onReply = fn
}

// drainReplies pumps query answers out of the vt emulator for the lifetime of
// the emulator. It exits when Close shuts the pipe down.
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

// Close releases the emulator and stops the reply drain. The session owns its
// emulator and closes it when the session itself closes; Close is idempotent.
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
