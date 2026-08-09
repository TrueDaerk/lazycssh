// Package sessionlog writes per-host session output to disk, one file per
// host, for one run. It is opt-in: nothing in lazycssh constructs a [Run]
// unless the user asked for one with --log-dir, and the status bar says
// SESSION LOGGING ON for the whole run while it exists.
//
// Two rules the package enforces rather than documents:
//
//   - Only host **output** is ever written. Keystrokes never pass through here;
//     the transport taps its output stream and nothing else, the same boundary
//     [commandlog] draws for the audit trail.
//   - While the run is suppressed - broadcast mode single, the mode a password
//     prompt is answered in - output is dropped, not written. The gap is
//     visible: a marker line says logging paused, and another says it resumed.
//     A log that quietly forgets is worse than one that says it forgot.
package sessionlog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultMaxFileSize bounds one host's log file. When a file would grow past
// it, the file rotates once: the current file becomes <name>.log.1 (replacing
// the previous rotation) and a fresh file continues. A run therefore keeps at
// most twice this much per host.
const DefaultMaxFileSize = 8 << 20

// dirFormat names the per-run directory after the moment the run started.
// Filesystem-safe on every platform: no colons.
const dirFormat = "2006-01-02_15-04-05"

// Markers written around a suppression gap, and on reconnect. They carry a
// lazycssh prefix so they cannot be mistaken for host output.
const (
	pausedMarker    = "[lazycssh: output not logged: single-mode input]\n"
	resumedMarker   = "[lazycssh: logging resumed]\n"
	reconnectMarker = "[lazycssh: reconnected]\n"
)

// Run is one run's log directory. It hands out one [HostLog] per host and
// owns the suppression flag they all share.
//
// A Run is safe for concurrent use: host logs are requested from the event
// loop while session goroutines write.
type Run struct {
	dir      string
	maxBytes int64

	// suppressed is shared with every HostLog; it is flipped by the event
	// loop when the broadcast mode changes and read by session goroutines on
	// every write.
	suppressed atomic.Bool

	mu    sync.Mutex
	logs  map[string]*HostLog // by host id
	names map[string]bool     // file names taken, after sanitising
}

// Open creates a fresh directory for one run under root, named after start.
// Root is created if it does not exist. When two runs start within the same
// second the second directory gets a numeric suffix rather than sharing.
//
// The directory is private (0700): session output is exactly the kind of data
// that must not be world-readable.
func Open(root string, start time.Time) (*Run, error) {
	if root == "" {
		return nil, fmt.Errorf("sessionlog: empty root directory")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("sessionlog: create %s: %w", root, err)
	}

	base := start.Format(dirFormat)
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		dir := filepath.Join(root, name)
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return &Run{
				dir:      dir,
				maxBytes: DefaultMaxFileSize,
				logs:     make(map[string]*HostLog),
				names:    make(map[string]bool),
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("sessionlog: create %s: %w", dir, err)
		}
	}
}

// Dir is the run's directory, where the per-host files live.
func (r *Run) Dir() string { return r.dir }

// SetMaxFileSize changes the per-file rotation bound for host logs opened
// after the call. Zero or negative restores the default.
func (r *Run) SetMaxFileSize(n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n <= 0 {
		n = DefaultMaxFileSize
	}
	r.maxBytes = n
}

// SetSuppressed pauses or resumes output logging for every host at once. The
// event loop calls it when the broadcast mode enters or leaves single mode;
// each host log writes its own pause and resume markers lazily, so a gap
// appears exactly in the logs that dropped something.
func (r *Run) SetSuppressed(on bool) { r.suppressed.Store(on) }

// Suppressed reports whether output logging is paused.
func (r *Run) Suppressed() bool { return r.suppressed.Load() }

// HostLog returns the host's log, opening its file on first request. Asking
// again for the same host - a reconnect - returns the same log, appending,
// with a marker so the seam is visible in the file.
//
// It never returns nil: a log whose file could not be opened swallows writes
// and reports the failure through [HostLog.Err] and [Run.Close], because one
// unwritable file must not kill the session it belongs to.
func (r *Run) HostLog(id string) *HostLog {
	r.mu.Lock()
	defer r.mu.Unlock()

	if h, ok := r.logs[id]; ok {
		h.writeMarker(reconnectMarker)
		return h
	}

	h := &HostLog{
		path:     filepath.Join(r.dir, r.claimName(id)),
		maxBytes: r.maxBytes,
		run:      r,
	}
	f, err := os.OpenFile(h.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		h.err = fmt.Errorf("sessionlog: open %s: %w", h.path, err)
	} else {
		h.f = f
	}
	r.logs[id] = h
	return h
}

// claimName reserves a file name for a host id, sanitised and unique within
// the run. Callers hold r.mu.
func (r *Run) claimName(id string) string {
	base := sanitize(id)
	for i := 1; ; i++ {
		name := base + ".log"
		if i > 1 {
			name = fmt.Sprintf("%s-%d.log", base, i)
		}
		if !r.names[name] {
			r.names[name] = true
			return name
		}
	}
}

// sanitize turns a host id into a safe file name. Host ids are ssh aliases,
// so this is belt and braces: path separators and control bytes cannot become
// directory traversal, and a leading dot cannot hide the file.
func sanitize(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r == '/' || r == '\\' || r == ':' || r < 0x20 || r == 0x7f:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" || s[0] == '.' {
		s = "_" + s
	}
	return s
}

// Close closes every host log and returns the first error any of them hit,
// opening, writing or closing. Logging was explicitly asked for; a run whose
// logs are incomplete must end by saying so.
func (r *Run) Close() error {
	r.mu.Lock()
	logs := make([]*HostLog, 0, len(r.logs))
	for _, h := range r.logs {
		logs = append(logs, h)
	}
	r.mu.Unlock()

	var first error
	for _, h := range logs {
		if err := h.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// HostLog is one host's log file. It implements io.Writer for the transport's
// output tap and is safe for concurrent use: a session's stdout and stderr
// pumps both write here.
type HostLog struct {
	run      *Run
	path     string
	maxBytes int64

	mu     sync.Mutex
	f      *os.File
	size   int64
	err    error
	paused bool // a pause marker was written and no resume marker yet
}

// Write appends host output. It never returns an error and never blocks on
// anything but the disk: a full disk records the failure once, then drops -
// the reader goroutine feeding it must not stall, and one broken log must not
// kill its session. The first failure is kept for [HostLog.Err].
//
// While the run is suppressed the bytes are dropped and a pause marker is
// written once; the first write after resuming closes the gap with a resume
// marker. The markers only appear when output actually arrived during the
// gap, so a quiet host's log stays clean.
func (h *HostLog) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.f == nil || h.err != nil {
		return len(p), nil
	}

	if h.run != nil && h.run.suppressed.Load() {
		if !h.paused {
			h.paused = true
			h.append([]byte(pausedMarker))
		}
		return len(p), nil
	}
	if h.paused {
		h.paused = false
		h.append([]byte(resumedMarker))
	}

	h.append(p)
	return len(p), nil
}

// writeMarker appends a marker line, honouring suppression the way output
// does: a marker must not punch through a paused log.
func (h *HostLog) writeMarker(marker string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.f == nil || h.err != nil {
		return
	}
	if h.run != nil && h.run.suppressed.Load() {
		return
	}
	h.append([]byte(marker))
}

// append writes bytes, rotating first when the file would grow past the
// bound. Callers hold h.mu and have checked h.f and h.err.
func (h *HostLog) append(p []byte) {
	if h.maxBytes > 0 && h.size > 0 && h.size+int64(len(p)) > h.maxBytes {
		h.rotate()
		if h.err != nil {
			return
		}
	}
	n, err := h.f.Write(p)
	h.size += int64(n)
	if err != nil {
		h.err = fmt.Errorf("sessionlog: write %s: %w", h.path, err)
	}
}

// rotate moves the current file aside as <path>.1 - replacing the previous
// rotation - and starts a fresh one. Callers hold h.mu.
func (h *HostLog) rotate() {
	if err := h.f.Close(); err != nil && h.err == nil {
		h.err = fmt.Errorf("sessionlog: rotate %s: %w", h.path, err)
	}
	if err := os.Rename(h.path, h.path+".1"); err != nil && h.err == nil {
		h.err = fmt.Errorf("sessionlog: rotate %s: %w", h.path, err)
	}
	f, err := os.OpenFile(h.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		h.f = nil
		if h.err == nil {
			h.err = fmt.Errorf("sessionlog: rotate %s: %w", h.path, err)
		}
		return
	}
	h.f = f
	h.size = 0
}

// Path is where this host's log is written.
func (h *HostLog) Path() string {
	return h.path
}

// Err is the first failure this log hit, or nil. A failed log keeps accepting
// writes and drops them; this is where the loss becomes visible.
func (h *HostLog) Err() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.err
}

// Close flushes and closes the file. Further writes are dropped. It returns
// the log's first error, so a run that lost output ends by saying so.
func (h *HostLog) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.f != nil {
		if err := h.f.Close(); err != nil && h.err == nil {
			h.err = fmt.Errorf("sessionlog: close %s: %w", h.path, err)
		}
		h.f = nil
	}
	return h.err
}
