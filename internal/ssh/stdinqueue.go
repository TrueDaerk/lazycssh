package ssh

import (
	"fmt"
	"io"
	"sync"
)

// DefaultStdinQueueDepth bounds how many pending writes a session's stdin can
// hold before Write starts refusing. Each entry is one Write call - a
// keystroke, a pasted chunk, a broadcast command - so the depth is generous
// for interactive use and still small enough that a stalled host cannot eat
// the process's memory.
const DefaultStdinQueueDepth = 512

// stdinQueue decouples everything that writes to a host from the SSH channel
// behind it. An SSH channel write blocks when the remote window is exhausted -
// a host frozen by ctrl+s, a hung sshd, a dead TCP peer - and the writers are
// the UI's Update loop: the command line, the broadcast bar, a focused pane's
// keystrokes. One stalled host must never stall them (the design rule in
// CLAUDE.md; issue #225).
//
// Write therefore only enqueues, and a single goroutine (run) drains the queue
// into the channel, preserving order. A full queue refuses the write with an
// error rather than blocking or dropping silently: the caller reports the miss
// the same way it reports a dead session.
type stdinQueue struct {
	alias string
	ch    chan []byte
	done  <-chan struct{}

	mu  sync.Mutex
	err error
}

// newStdinQueue builds a queue for one session. done is the session's shutdown
// signal: it releases the drain goroutine when the session closes.
func newStdinQueue(alias string, depth int, done <-chan struct{}) *stdinQueue {
	if depth <= 0 {
		depth = DefaultStdinQueueDepth
	}
	return &stdinQueue{alias: alias, ch: make(chan []byte, depth), done: done}
}

// Write enqueues one chunk and returns immediately. The bytes are copied,
// because the caller may reuse its buffer before the drain goroutine gets to
// them. A full queue is a stalled host: the write is refused loudly, never
// blocked on and never dropped in silence.
func (q *stdinQueue) Write(p []byte) (int, error) {
	q.mu.Lock()
	err := q.err
	q.mu.Unlock()
	if err != nil {
		return 0, fmt.Errorf("write to %s: %w", q.alias, err)
	}

	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case q.ch <- buf:
		return len(p), nil
	default:
		return 0, fmt.Errorf("write to %s: input backlog full, %d bytes refused", q.alias, len(p))
	}
}

// run drains the queue into the channel until the session shuts down or the
// channel refuses a write. It is the only goroutine that touches w, which is
// what preserves write order. A write error is terminal: the transport under
// the session is gone, and every later Write reports it instead of queueing
// bytes nobody will send.
func (q *stdinQueue) run(w io.Writer) {
	for {
		select {
		case p := <-q.ch:
			if _, err := w.Write(p); err != nil {
				q.mu.Lock()
				q.err = err
				q.mu.Unlock()
				return
			}
		case <-q.done:
			return
		}
	}
}

// Err is the write error that stopped the drain, or nil.
func (q *stdinQueue) Err() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}
