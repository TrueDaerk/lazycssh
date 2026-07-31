// Package ssh is the transport layer: one SSH session per host, with a PTY, a
// stdin writer and output readers.
//
// Nothing in this package touches the bubbletea model. Sessions report what
// happens over an [Event] channel; the UI drains that channel with a command and
// converts events into messages, so model mutation stays inside Update.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/scrollback"
	"github.com/TrueDaerk/lazycssh/internal/term"
)

// Defaults for a session that does not configure them.
const (
	DefaultTimeout = 15 * time.Second
	DefaultTerm    = "xterm-256color"
	DefaultWidth   = 80
	DefaultHeight  = 24
)

// State is where a session is in its lifecycle.
type State int

const (
	// StatePending means Start has not been called yet.
	StatePending State = iota
	// StateDialing means the TCP connection is being established.
	StateDialing
	// StateAuthenticating means the SSH handshake and authentication are running.
	StateAuthenticating
	// StateConnected means a shell with a PTY is running.
	StateConnected
	// StateFailed means the session ended with an error.
	StateFailed
	// StateClosed means the session ended without an error.
	StateClosed
)

// String returns a short lowercase name suitable for the Hosts panel.
func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateDialing:
		return "dialing"
	case StateAuthenticating:
		return "authenticating"
	case StateConnected:
		return "connected"
	case StateFailed:
		return "failed"
	case StateClosed:
		return "closed"
	default:
		return "unknown(" + strconv.Itoa(int(s)) + ")"
	}
}

// Done reports whether the session has reached a final state.
func (s State) Done() bool { return s == StateFailed || s == StateClosed }

// Event is something a session reports.
//
// Events carry no session output: bytes go into the session's
// [scrollback.Buffer] and the consumer reads them from there. An event only says
// that something changed, so a slow UI can never apply backpressure to a remote
// host.
//
// Events are hints, not the source of truth. They are dropped when the consumer
// is behind, so a consumer must read [Session.State], [Session.Err] and
// [Session.Scrollback] when it processes one rather than reconstructing state
// from the event stream.
type Event interface {
	// SessionID identifies the session the event came from.
	SessionID() string
}

// OutputEvent says that new output is available in the session's scrollback.
// It is a coalescing notification: consecutive writes may produce a single
// event, and events are dropped rather than queued when the UI is behind.
type OutputEvent struct {
	ID string
}

func (e OutputEvent) SessionID() string { return e.ID }

// StateEvent reports a lifecycle transition. Err is set for [StateFailed].
type StateEvent struct {
	ID    string
	State State
	Err   error
}

func (e StateEvent) SessionID() string { return e.ID }

// Session is what the UI depends on. The real implementation dials a host; the
// test implementation in fake.go does not, so no test in this repository needs
// a network.
type Session interface {
	// ID uniquely identifies this session for the lifetime of the program.
	ID() string
	// Host is the resolved host this session belongs to.
	Host() hosts.Host
	// State is the current lifecycle state.
	State() State
	// Err is the error that moved the session to [StateFailed], or nil.
	Err() error
	// Scrollback holds the output received so far.
	Scrollback() *scrollback.Buffer
	// Terminal is the emulator fed with the same output bytes as the
	// scrollback. It tracks screen state the scrollback cannot — alternate
	// screen, cursor position — for rendering full-screen remote apps.
	Terminal() *term.Emulator
	// LastExit is the exit status of the last command the remote shell
	// reported, and whether one has been reported at all. A shell without the
	// prompt hook never reports; see exit.go.
	LastExit() (int, bool)
	// Start dials, authenticates and starts a shell with a PTY. It returns once
	// the shell is running; the session then reports asynchronously.
	Start(ctx context.Context) error
	// Write sends bytes to the remote shell's stdin.
	Write(p []byte) (int, error)
	// Resize informs the remote PTY of a new window size.
	Resize(width, height int) error
	// Close ends the session and releases everything it owns. It is safe to call
	// more than once and safe to call on a session that never started.
	Close() error
}

// Config describes one session. Auth and HostKeyCallback are supplied by the
// caller so that the authentication chain and host key verification can evolve
// independently of the session lifecycle.
type Config struct {
	// Host is the resolved target.
	Host hosts.Host
	// Auth lists the authentication methods to offer, in order.
	Auth []ssh.AuthMethod
	// HostKeyCallback verifies the server's key. A nil callback is refused:
	// defaulting to InsecureIgnoreHostKey would turn a forgotten field into a
	// silent downgrade.
	HostKeyCallback ssh.HostKeyCallback
	// Timeout bounds the TCP connect and the SSH handshake.
	Timeout time.Duration
	// Term is the TERM value requested for the PTY.
	Term string
	// Width and Height are the initial PTY dimensions.
	Width, Height int
	// ScrollbackLines bounds the retained output; zero means the default.
	ScrollbackLines int
	// Scrollback, when set, is reused instead of allocating a fresh buffer.
	// Reconnecting passes the previous buffer here so the pane keeps what the
	// host said before it died.
	Scrollback *scrollback.Buffer
}

func (c Config) withDefaults() Config {
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.Term == "" {
		c.Term = DefaultTerm
	}
	if c.Width <= 0 {
		c.Width = DefaultWidth
	}
	if c.Height <= 0 {
		c.Height = DefaultHeight
	}
	return c
}

// ErrNoHostKeyCallback is returned when a session is configured without host key
// verification. Host key checking is on by default and cannot be skipped by
// omission - only by an explicit callback that accepts everything.
var ErrNoHostKeyCallback = errors.New("ssh: no host key callback configured")

// sshSession is the real [Session]: it dials.
type sshSession struct {
	id     string
	cfg    Config
	events chan<- Event
	buf    *scrollback.Buffer
	emu    *term.Emulator

	mu            sync.Mutex
	state         State
	err           error
	client        *ssh.Client
	session       *ssh.Session
	stdin         io.WriteCloser
	droppedEvents int
	lastExit      int
	hasExit       bool

	closeOnce sync.Once
	closed    chan struct{} // closed when the session is shutting down
	wg        sync.WaitGroup
}

// New returns a session for one host. Events are sent to the given channel,
// which the caller owns; nil is allowed for a session nobody is watching.
func New(id string, cfg Config, events chan<- Event) Session {
	cfg = cfg.withDefaults()

	buf := cfg.Scrollback
	if buf == nil {
		buf = scrollback.New(cfg.ScrollbackLines)
	}
	// The scrollback's line discipline maps cursor movement through the remote
	// terminal's width; keep it in lockstep with the PTY, like the emulator.
	buf.SetWidth(cfg.Width)

	s := &sshSession{
		id:     id,
		cfg:    cfg,
		events: events,
		buf:    buf,
		emu:    term.New(cfg.Width, cfg.Height),
		closed: make(chan struct{}),
	}
	// The emulator's answers to terminal queries (device attributes, cursor
	// position) go back to this session's stdin — and only this session's;
	// they never travel through broadcast. Full-screen apps hang without them.
	// Before the shell starts or after the session dies, Write refuses and the
	// reply is dropped rather than queued for the wrong moment.
	s.emu.SetReplyHandler(func(p []byte) { _, _ = s.Write(p) })
	return s
}

func (s *sshSession) ID() string                     { return s.id }
func (s *sshSession) Host() hosts.Host               { return s.cfg.Host }
func (s *sshSession) Scrollback() *scrollback.Buffer { return s.buf }
func (s *sshSession) Terminal() *term.Emulator       { return s.emu }

func (s *sshSession) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *sshSession) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Start dials the host and starts a shell with a PTY.
func (s *sshSession) Start(ctx context.Context) error {
	if s.cfg.HostKeyCallback == nil {
		return s.fail(ErrNoHostKeyCallback)
	}

	s.setState(StateDialing, nil)

	addr := net.JoinHostPort(s.cfg.Host.Addr, strconv.Itoa(s.cfg.Host.Port))
	dialer := net.Dialer{Timeout: s.cfg.Timeout}

	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return s.fail(fmt.Errorf("dial %s: %w", addr, err))
	}

	s.setState(StateAuthenticating, nil)

	client, err := s.handshake(ctx, conn, addr)
	if err != nil {
		conn.Close()
		return s.fail(err)
	}

	session, err := client.NewSession()
	if err != nil {
		client.Close()
		return s.fail(fmt.Errorf("open session on %s: %w", s.cfg.Host.Alias, err))
	}

	if err := s.startShell(session); err != nil {
		session.Close()
		client.Close()
		return s.fail(err)
	}

	s.mu.Lock()
	s.client, s.session = client, session
	s.mu.Unlock()

	s.setState(StateConnected, nil)

	s.wg.Add(1)
	go s.waitForExit(session)

	return nil
}

// handshake performs the SSH handshake, honouring ctx for the duration: the
// crypto library takes no context, so the deadline is enforced on the raw
// connection and a cancelled context closes it.
func (s *sshSession) handshake(ctx context.Context, conn net.Conn, addr string) (*ssh.Client, error) {
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	} else {
		conn.SetDeadline(time.Now().Add(s.cfg.Timeout))
	}

	handshakeDone := make(chan struct{})
	defer close(handshakeDone)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-handshakeDone:
		}
	}()

	clientConfig := &ssh.ClientConfig{
		User:            s.cfg.Host.User,
		Auth:            s.cfg.Auth,
		HostKeyCallback: s.cfg.HostKeyCallback,
		Timeout:         s.cfg.Timeout,
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, addr, clientConfig)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("connect to %s: %w", s.cfg.Host.Alias, ctxErr)
		}
		return nil, fmt.Errorf("connect to %s: %w", s.cfg.Host.Alias, err)
	}

	// The handshake is done; further reads and writes must not inherit its
	// deadline or an idle session would be torn down.
	conn.SetDeadline(time.Time{})

	return ssh.NewClient(c, chans, reqs), nil
}

// startShell requests a PTY, wires up the streams and starts the login shell.
func (s *sshSession) startShell(session *ssh.Session) error {
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty(s.cfg.Term, s.cfg.Height, s.cfg.Width, modes); err != nil {
		return fmt.Errorf("request pty on %s: %w", s.cfg.Host.Alias, err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe for %s: %w", s.cfg.Host.Alias, err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe for %s: %w", s.cfg.Host.Alias, err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe for %s: %w", s.cfg.Host.Alias, err)
	}

	if err := session.Shell(); err != nil {
		return fmt.Errorf("start shell on %s: %w", s.cfg.Host.Alias, err)
	}

	s.mu.Lock()
	s.stdin = stdin
	s.mu.Unlock()

	// Arm the exit code reporting before anything else is typed. The bytes
	// queue in the PTY input until the shell reads them; a shell that does not
	// understand the hook prints an error once and the session still works.
	if _, err := stdin.Write([]byte(ExitSetupCommand)); err != nil {
		return fmt.Errorf("arm exit reporting on %s: %w", s.cfg.Host.Alias, err)
	}

	s.wg.Add(2)
	// Only stdout carries the prompt, so only stdout gets the scanner and the
	// echo filter that hides the setup line's PTY echo.
	go s.pump(stdout, &exitScanner{onExit: s.recordExit}, newEchoFilter())
	go s.pump(stderr, nil, nil)

	return nil
}

// pump copies one stream into the scrollback. Both streams land in the same
// buffer because that is how they appear on a terminal, interleaved in arrival
// order. The scanner, when given, watches the same bytes for exit markers.
// The filter, when given, removes the echoed exit-hook setup line before the
// bytes reach the scrollback or the emulator; the scanner sees the raw stream,
// because the marker it hunts is never part of the filtered text.
func (s *sshSession) pump(r io.Reader, scan *exitScanner, filter *echoFilter) {
	defer s.wg.Done()

	buf := make([]byte, 32<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if scan != nil {
				scan.Scan(buf[:n])
			}
			out := buf[:n]
			if filter != nil {
				out = filter.Filter(out)
			}
			if len(out) > 0 {
				s.buf.Write(out)
				s.emu.Write(out)
				s.notifyOutput()
			}
		}
		if err != nil {
			if filter != nil {
				if held := filter.Flush(); len(held) > 0 {
					s.buf.Write(held)
					s.emu.Write(held)
					s.notifyOutput()
				}
			}
			return
		}
	}
}

// waitForExit reports the end of the remote shell.
func (s *sshSession) waitForExit(session *ssh.Session) {
	defer s.wg.Done()

	err := session.Wait()

	select {
	case <-s.closed:
		// Close is running; it reports the final state itself.
		return
	default:
	}

	if err == nil {
		s.setState(StateClosed, nil)
		return
	}

	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		// The remote shell exited with a status. That is the shell ending, not
		// a transport failure.
		s.setState(StateClosed, nil)
		return
	}
	s.setState(StateFailed, fmt.Errorf("session on %s ended: %w", s.cfg.Host.Alias, err))
}

// Write sends bytes to the remote shell's stdin.
func (s *sshSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	stdin, state := s.stdin, s.state
	s.mu.Unlock()

	if stdin == nil {
		return 0, fmt.Errorf("write to %s: session is %s", s.cfg.Host.Alias, state)
	}
	n, err := stdin.Write(p)
	if err != nil {
		return n, fmt.Errorf("write to %s: %w", s.cfg.Host.Alias, err)
	}
	return n, nil
}

// Resize informs the remote PTY of a new window size.
func (s *sshSession) Resize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("resize %s: invalid size %dx%d", s.cfg.Host.Alias, width, height)
	}

	s.mu.Lock()
	session := s.session
	s.cfg.Width, s.cfg.Height = width, height
	s.mu.Unlock()

	s.emu.Resize(width, height)
	s.buf.SetWidth(width)

	if session == nil {
		// Not connected yet: the size is remembered and requested at start.
		return nil
	}
	if err := session.WindowChange(height, width); err != nil {
		return fmt.Errorf("resize %s: %w", s.cfg.Host.Alias, err)
	}
	return nil
}

// Close ends the session. It is idempotent and safe on a session that never
// started.
func (s *sshSession) Close() error {
	var err error

	s.closeOnce.Do(func() {
		close(s.closed)

		s.mu.Lock()
		stdin, session, client := s.stdin, s.session, s.client
		s.stdin, s.session, s.client = nil, nil, nil
		s.mu.Unlock()

		if stdin != nil {
			stdin.Close()
		}
		if session != nil {
			session.Close()
		}
		if client != nil {
			err = client.Close()
		}

		// The reader goroutines end when their streams close, which closing the
		// client guarantees. Waiting here is what makes "no goroutine leaks"
		// testable.
		s.wg.Wait()
		s.emu.Close()

		s.mu.Lock()
		if !s.state.Done() {
			s.state = StateClosed
		}
		state, stateErr := s.state, s.err
		s.mu.Unlock()

		s.emit(StateEvent{ID: s.id, State: state, Err: stateErr})
	})

	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close %s: %w", s.cfg.Host.Alias, err)
	}
	return nil
}

// fail records a terminal error and returns it, so callers can just return it.
func (s *sshSession) fail(err error) error {
	s.setState(StateFailed, err)
	return err
}

// setState records a transition and reports it.
func (s *sshSession) setState(state State, err error) {
	s.mu.Lock()
	if s.state.Done() && state != StateClosed {
		// A session that already failed does not move on.
		s.mu.Unlock()
		return
	}
	s.state = state
	if err != nil {
		s.err = err
	}
	s.mu.Unlock()

	if state == StateFailed {
		writeFailureNotice(s.buf, err)
	}
	s.emit(StateEvent{ID: s.id, State: state, Err: err})
}

// emit delivers an event, or drops it when the consumer is behind.
//
// Nothing in the transport may ever block on the UI. An event is a hint that
// something changed, never the source of truth: the authoritative answers are
// [Session.State], [Session.Err] and [Session.Scrollback], all of which a
// consumer can read at any time. Making this send blocking instead would
// deadlock Start against a full channel - which is exactly what happens without
// this, and what TestSessionSurvivesAnUndrainedEventChannel catches.
//
// Dropped events are counted so the gap is measurable rather than invisible.
func (s *sshSession) emit(ev Event) {
	if s.events == nil {
		return
	}
	select {
	case s.events <- ev:
	default:
		s.mu.Lock()
		s.droppedEvents++
		s.mu.Unlock()
	}
}

// notifyOutput delivers a coalescing "there is new output" hint. The bytes are
// already in the scrollback, so dropping the hint costs nothing but a slightly
// later redraw.
func (s *sshSession) notifyOutput() {
	s.emit(OutputEvent{ID: s.id})
}

// DroppedEvents returns how many events were discarded because the consumer was
// behind. It exists so that a UI which appears stuck can be diagnosed.
func (s *sshSession) DroppedEvents() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.droppedEvents
}
