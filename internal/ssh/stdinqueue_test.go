package ssh

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockedWriter blocks every Write until released, like an SSH channel whose
// remote window is exhausted.
type blockedWriter struct {
	release chan struct{}

	mu  sync.Mutex
	buf bytes.Buffer
}

func newBlockedWriter() *blockedWriter {
	return &blockedWriter{release: make(chan struct{})}
}

func (w *blockedWriter) Write(p []byte) (int, error) {
	<-w.release
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *blockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// The design rule this type exists for: a stalled host must never stall the
// caller, which is the UI's Update loop (issue #225).
func TestStdinQueueWriteDoesNotBlockOnAStalledHost(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	w := newBlockedWriter()

	q := newStdinQueue("srv1", 4, done)
	go q.run(w)

	finished := make(chan error, 1)
	go func() {
		for i := 0; i < 4; i++ {
			if _, err := q.Write([]byte{'a'}); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
	}()

	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("Write within the queue depth failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked on a writer that never completes")
	}
}

func TestStdinQueueRefusesWhenTheBacklogIsFull(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	w := newBlockedWriter()

	q := newStdinQueue("srv1", 2, done)
	go q.run(w)

	// The drain goroutine may pull one chunk off the channel and block inside
	// the writer, freeing a slot; writing depth+2 chunks guarantees the
	// backlog fills whatever the interleaving.
	var refused error
	for i := 0; i < 4; i++ {
		if _, err := q.Write([]byte{'a'}); err != nil {
			refused = err
			break
		}
	}
	if refused == nil {
		t.Fatal("a full backlog accepted every write")
	}
	if !strings.Contains(refused.Error(), "srv1") {
		t.Errorf("the refusal does not name the host: %v", refused)
	}
	if !strings.Contains(refused.Error(), "backlog full") {
		t.Errorf("the refusal does not say why: %v", refused)
	}
}

func TestStdinQueuePreservesWriteOrder(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	w := newBlockedWriter()

	q := newStdinQueue("srv1", 8, done)
	for _, chunk := range []string{"one ", "two ", "three"} {
		if _, err := q.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write(%q): %v", chunk, err)
		}
	}

	drained := make(chan struct{})
	go func() { q.run(w); close(drained) }()
	close(w.release)

	deadline := time.After(2 * time.Second)
	for w.String() != "one two three" {
		select {
		case <-deadline:
			t.Fatalf("drained %q, want %q", w.String(), "one two three")
		case <-time.After(time.Millisecond):
		}
	}
}

// errWriter fails every write, like a channel whose transport died.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestStdinQueueReportsAWriteErrorInsteadOfQueueingForever(t *testing.T) {
	done := make(chan struct{})
	defer close(done)
	broken := errors.New("connection lost")

	q := newStdinQueue("srv1", 4, done)
	if _, err := q.Write([]byte("x")); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	finished := make(chan struct{})
	go func() { q.run(errWriter{err: broken}); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop on a write error")
	}

	_, err := q.Write([]byte("y"))
	if err == nil {
		t.Fatal("Write after a transport error reported success")
	}
	if !errors.Is(err, broken) {
		t.Errorf("Write error = %v, want the transport's %v", err, broken)
	}
	if !errors.Is(q.Err(), broken) {
		t.Errorf("Err() = %v, want %v", q.Err(), broken)
	}
}

func TestStdinQueueRunStopsWhenTheSessionCloses(t *testing.T) {
	done := make(chan struct{})
	q := newStdinQueue("srv1", 4, done)

	finished := make(chan struct{})
	go func() { q.run(newBlockedWriter()); close(finished) }()

	close(done)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not stop when the session closed")
	}
}
