package ssh

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/scrollback"
)

// Fake is a [Session] that never opens a socket.
//
// It exists so that the UI can be tested against the same interface the real
// transport implements, without a network, a fixture host or a timing
// assumption. Every test above the transport layer should use it: no test in
// this repository dials a real host.
//
// A Fake is scripted through its exported fields before [Fake.Start], and driven
// through its methods afterwards:
//
//	f := ssh.NewFake("s1", host, events)
//	f.ConnectDelay = 50 * time.Millisecond
//	f.AuthErr = errors.New("permission denied")
//	f.Start(ctx)
//
// A Fake is safe for concurrent use.
type Fake struct {
	// ConnectDelay is how long Start spends in the dialing state. It lets a
	// test observe intermediate states without racing.
	ConnectDelay time.Duration
	// DialErr, when set, fails Start while dialing, as an unreachable host does.
	DialErr error
	// AuthErr, when set, fails Start while authenticating, as a wrong
	// credential does.
	AuthErr error
	// Banner is written to the scrollback once the session is connected, in
	// place of a login message.
	Banner string
	// EchoInput makes everything written to the session appear in its
	// scrollback, as a remote PTY echo would.
	EchoInput bool
	// Responses maps a line written to the session to the output it produces.
	// The lookup key is the line with its trailing carriage return or newline
	// removed.
	Responses map[string]string

	id     string
	host   hosts.Host
	events chan<- Event
	buf    *scrollback.Buffer

	mu      sync.Mutex
	state   State
	err     error
	written strings.Builder
	pending strings.Builder
	width   int
	height  int
	closed  bool
	resizes int

	scan     exitScanner
	lastExit int
	hasExit  bool
}

// NewFake returns a fake session for the given host. Events are optional.
func NewFake(id string, host hosts.Host, events chan<- Event) *Fake {
	return &Fake{
		id:     id,
		host:   host,
		events: events,
		buf:    scrollback.New(scrollback.DefaultCapacity),
		width:  DefaultWidth,
		height: DefaultHeight,
	}
}

// UseScrollback adopts an existing buffer instead of the fake's own, which is
// what a reconnect does to keep a pane's history.
func (f *Fake) UseScrollback(buf *scrollback.Buffer) {
	if buf == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buf = buf
}

func (f *Fake) ID() string       { return f.id }
func (f *Fake) Host() hosts.Host { return f.host }

func (f *Fake) Scrollback() *scrollback.Buffer {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf
}

func (f *Fake) State() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}

func (f *Fake) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// Start walks the scripted connection sequence. It honours ctx, so a test can
// cancel a connection in progress.
func (f *Fake) Start(ctx context.Context) error {
	f.setState(StateDialing, nil)

	if f.ConnectDelay > 0 {
		select {
		case <-time.After(f.ConnectDelay):
		case <-ctx.Done():
			return f.fail(fmt.Errorf("connect to %s: %w", f.host.Alias, ctx.Err()))
		}
	}
	if err := ctx.Err(); err != nil {
		return f.fail(fmt.Errorf("connect to %s: %w", f.host.Alias, err))
	}
	if f.DialErr != nil {
		return f.fail(fmt.Errorf("dial %s: %w", f.host.Alias, f.DialErr))
	}

	f.setState(StateAuthenticating, nil)

	if f.AuthErr != nil {
		return f.fail(fmt.Errorf("authenticate to %s: %w", f.host.Alias, f.AuthErr))
	}

	f.setState(StateConnected, nil)

	if f.Banner != "" {
		f.Emit(f.Banner)
	}
	return nil
}

// Write records the input and applies the scripted reactions: echo, then any
// response configured for a completed line.
func (f *Fake) Write(p []byte) (int, error) {
	f.mu.Lock()
	if f.state != StateConnected {
		state := f.state
		f.mu.Unlock()
		return 0, fmt.Errorf("write to %s: session is %s", f.host.Alias, state)
	}
	f.written.Write(p)
	f.pending.Write(p)
	lines := splitCompleteLines(&f.pending)
	echo := f.EchoInput
	f.mu.Unlock()

	if echo {
		f.Emit(strings.ReplaceAll(string(p), "\r", "\r\n"))
	}
	for _, line := range lines {
		if out, ok := f.Responses[line]; ok {
			f.Emit(out)
		}
	}
	return len(p), nil
}

// splitCompleteLines drains every terminated line from b, leaving the partial
// remainder. The caller holds the lock.
func splitCompleteLines(b *strings.Builder) []string {
	s := b.String()
	var lines []string

	for {
		i := strings.IndexAny(s, "\r\n")
		if i < 0 {
			break
		}
		lines = append(lines, s[:i])
		s = s[i+1:]
	}

	b.Reset()
	b.WriteString(s)
	return lines
}

// Resize records the new size.
func (f *Fake) Resize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("resize %s: invalid size %dx%d", f.host.Alias, width, height)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.width, f.height = width, height
	f.resizes++
	return nil
}

// Close ends the session. It is idempotent.
func (f *Fake) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	alreadyDone := f.state.Done()
	f.mu.Unlock()

	if !alreadyDone {
		f.setState(StateClosed, nil)
	}
	return nil
}

// Emit appends output as if the remote host had sent it. The output passes
// through the same exit marker scanner as a real session's stdout, so a test
// emitting a marker exercises the real parsing.
func (f *Fake) Emit(output string) {
	f.mu.Lock()
	if f.scan.onExit == nil {
		f.scan.onExit = f.recordExit
	}
	f.scan.Scan([]byte(output))
	buf := f.buf
	f.mu.Unlock()

	buf.Write([]byte(output))
	f.emit(OutputEvent{ID: f.id})
}

// recordExit stores the newest exit status. The caller holds f.mu.
func (f *Fake) recordExit(code int) {
	f.lastExit = code
	f.hasExit = true
}

// LastExit reports the exit status of the last command, if one was reported.
func (f *Fake) LastExit() (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastExit, f.hasExit
}

// ReportExit emits the exit marker a hooked shell prints before its prompt.
func (f *Fake) ReportExit(code int) {
	f.Emitf("\x1b]133;D;%d\a", code)
}

// Emitf is Emit with formatting.
func (f *Fake) Emitf(format string, args ...any) {
	f.Emit(fmt.Sprintf(format, args...))
}

// Flood emits n lines quickly, for exercising the bounded scrollback and a UI
// under load.
func (f *Fake) Flood(n int) {
	for i := 0; i < n; i++ {
		f.Emitf("flood line %d\r\n", i)
	}
}

// Disconnect simulates a session dropping mid-stream. A nil err ends the
// session normally, as a remote shell exiting does.
func (f *Fake) Disconnect(err error) {
	if err == nil {
		f.setState(StateClosed, nil)
		return
	}
	f.setState(StateFailed, fmt.Errorf("session on %s ended: %w", f.host.Alias, err))
}

// ExitWithStatus simulates the remote shell exiting with a status code. A
// non-zero status ends the session without failing it: the shell exited, the
// transport did not break.
func (f *Fake) ExitWithStatus(status int) {
	f.Emitf("exit status %d\r\n", status)
	f.setState(StateClosed, nil)
}

// Written returns everything ever written to the session's stdin, which is how
// a test asserts what a broadcast actually delivered.
func (f *Fake) Written() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.written.String()
}

// Size returns the last size passed to Resize.
func (f *Fake) Size() (width, height int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.width, f.height
}

// Resizes returns how many times Resize was called, so a test can assert that a
// terminal resize reached every session exactly once.
func (f *Fake) Resizes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resizes
}

func (f *Fake) fail(err error) error {
	f.setState(StateFailed, err)
	return err
}

func (f *Fake) setState(state State, err error) {
	f.mu.Lock()
	if f.state.Done() && state != StateClosed {
		f.mu.Unlock()
		return
	}
	f.state = state
	if err != nil {
		f.err = err
	}
	f.mu.Unlock()

	f.emit(StateEvent{ID: f.id, State: state, Err: err})
}

// emit mirrors the real session: never block on the consumer.
func (f *Fake) emit(ev Event) {
	if f.events == nil {
		return
	}
	select {
	case f.events <- ev:
	default:
	}
}

// errFakeDisconnect is a convenient error for tests that only need "the
// connection dropped".
var errFakeDisconnect = errors.New("connection reset by peer")

// ErrDisconnected is a stand-in transport failure for tests.
func ErrDisconnected() error { return errFakeDisconnect }
