package ssh

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/scrollback"
)

// reconnectMarker begins the separator line written into a preserved scrollback.
// It is matched by tests and by the renderer, so it is a constant rather than a
// literal in a format string.
const reconnectMarker = "── reconnecting"

// writeReconnectSeparator marks where an old connection ended and a new one
// begins, so the two cannot be read as one continuous stream.
func writeReconnectSeparator(buf *scrollback.Buffer, host hosts.Host) {
	if buf == nil {
		return
	}
	line := fmt.Sprintf("\r\n%s to %s at %s %s\r\n",
		reconnectMarker, host.Alias, time.Now().Format(time.RFC3339), strings.Repeat("─", 8))
	buf.Write([]byte(line))
}

// writeFailureNotice prints a failed session's error into its scrollback the
// way a plain terminal would show it (issue #180): as output, where the eye
// already is, scrolling with the history rather than floating over it.
func writeFailureNotice(buf *scrollback.Buffer, err error) {
	if buf == nil || err == nil {
		return
	}
	buf.Write([]byte("\r\n" + err.Error() + "\r\n"))
}

// Defaults for a manager that does not configure them.
const (
	// DefaultMaxParallelDials bounds how many connections are opened at once.
	// Two hundred hosts must not mean two hundred simultaneous sockets, file
	// descriptors and key exchanges.
	DefaultMaxParallelDials = 16
	// DefaultEventBuffer is the depth of the fan-in channel.
	DefaultEventBuffer = 512
)

// SessionRequest is what the manager asks a [Factory] for.
type SessionRequest struct {
	// ID is the identifier the session must report.
	ID string
	// Host is the resolved target.
	Host hosts.Host
	// Events is the fan-in channel to report on.
	Events chan<- Event
	// Scrollback, when set, must be adopted rather than replaced. Reconnecting
	// passes the previous session's buffer here so the pane keeps its history.
	Scrollback *scrollback.Buffer
}

// Factory builds one session. It exists so the manager can be driven by fakes:
// every manager test runs without a network.
type Factory func(SessionRequest) Session

// ManagerConfig describes a fleet.
type ManagerConfig struct {
	// Hosts are the targets, in the order the user gave them.
	Hosts []hosts.Host
	// NewSession builds each session. Required.
	NewSession Factory
	// MaxParallelDials bounds concurrent connection attempts; zero means the
	// default.
	MaxParallelDials int
	// EventBuffer is the fan-in channel depth; zero means the default.
	EventBuffer int
}

// Manager owns the sessions for a fleet and funnels their events into one
// channel.
//
// It never blocks on the consumer: sessions drop event hints when the channel is
// full, and the authoritative state is always readable from the sessions
// themselves.
type Manager struct {
	cfg    ManagerConfig
	events chan Event

	mu       sync.RWMutex
	sessions []Session
	byID     map[string]Session

	dialSem chan struct{}
	wg      sync.WaitGroup
}

// NewManager builds a session per host. Nothing is dialled until [Manager.Start].
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.NewSession == nil {
		return nil, fmt.Errorf("ssh: manager needs a session factory")
	}
	if cfg.MaxParallelDials <= 0 {
		cfg.MaxParallelDials = DefaultMaxParallelDials
	}
	if cfg.EventBuffer <= 0 {
		cfg.EventBuffer = DefaultEventBuffer
	}

	m := &Manager{
		cfg:     cfg,
		events:  make(chan Event, cfg.EventBuffer),
		byID:    make(map[string]Session, len(cfg.Hosts)),
		dialSem: make(chan struct{}, cfg.MaxParallelDials),
	}

	for _, host := range cfg.Hosts {
		id := m.uniqueID(host)
		s := cfg.NewSession(SessionRequest{ID: id, Host: host, Events: m.events})
		m.sessions = append(m.sessions, s)
		m.byID[id] = s
	}
	return m, nil
}

// uniqueID derives a stable identifier from the host alias. Aliases can repeat -
// the same host may be listed twice, or two patterns may overlap - and every
// session needs its own identity.
func (m *Manager) uniqueID(host hosts.Host) string {
	base := host.Alias
	if base == "" {
		base = host.Addr
	}
	if _, taken := m.byID[base]; !taken {
		return base
	}
	for n := 2; ; n++ {
		id := base + "#" + strconv.Itoa(n)
		if _, taken := m.byID[id]; !taken {
			return id
		}
	}
}

// Events is the single channel the UI drains. Every session reports here.
func (m *Manager) Events() <-chan Event { return m.events }

// Sessions returns the sessions in host order.
func (m *Manager) Sessions() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Session(nil), m.sessions...)
}

// Session looks one up by identifier.
func (m *Manager) Session(id string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byID[id]
	return s, ok
}

// Len is the number of sessions.
func (m *Manager) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// Start dials every host and returns immediately. Connections are opened with
// bounded concurrency; a host that is slow or unreachable delays only itself,
// because each attempt runs in its own goroutine and holds a semaphore slot for
// no longer than its own dial.
func (m *Manager) Start(ctx context.Context) {
	for _, s := range m.Sessions() {
		m.startOne(ctx, s)
	}
}

func (m *Manager) startOne(ctx context.Context, s Session) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		select {
		case m.dialSem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-m.dialSem }()

		// The error is deliberately dropped: Start has already recorded it on
		// the session and reported a StateEvent. A fleet-wide error return
		// would say nothing useful about which host failed.
		_ = s.Start(ctx)
	}()
}

// Wait blocks until every dial attempt has finished. It exists for tests and for
// a clean shutdown, not for the UI, which must never block.
func (m *Manager) Wait() { m.wg.Wait() }

// Add appends a session for one more host to a running fleet and dials it.
// It returns the new session's identifier. Merging a saved session into a run
// and starting from an empty run both land here; the existing sessions are not
// touched, so a merge cannot disturb a host that is already connected.
func (m *Manager) Add(ctx context.Context, host hosts.Host) string {
	m.mu.Lock()
	id := m.uniqueID(host)
	s := m.cfg.NewSession(SessionRequest{ID: id, Host: host, Events: m.events})
	m.sessions = append(m.sessions, s)
	m.byID[id] = s
	m.mu.Unlock()

	m.startOne(ctx, s)
	return id
}

// Reconnect replaces one session with a fresh one for the same host and dials
// it, without touching any other session.
//
// The identifier is preserved, so panes and selections survive, and so is the
// scrollback: a pane that just died is exactly the pane whose last lines the
// user wants to read. A separator is written into it so the old output cannot be
// mistaken for the new connection's.
func (m *Manager) Reconnect(ctx context.Context, id string) error {
	m.mu.Lock()
	old, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("reconnect %s: no such session", id)
	}

	buf := old.Scrollback()
	writeReconnectSeparator(buf, old.Host())

	fresh := m.cfg.NewSession(SessionRequest{
		ID: id, Host: old.Host(), Events: m.events, Scrollback: buf,
	})
	m.byID[id] = fresh
	for i, s := range m.sessions {
		if s.ID() == id {
			m.sessions[i] = fresh
			break
		}
	}
	m.mu.Unlock()

	// Closing the old session outside the lock: it waits for its reader
	// goroutines, and holding the lock through that would stall the UI.
	if err := old.Close(); err != nil {
		return fmt.Errorf("reconnect %s: %w", id, err)
	}

	m.startOne(ctx, fresh)
	return nil
}

// Remove takes one session out of the fleet entirely: it is closed and no
// longer listed, so its pane disappears. Close leaves a dead pane on screen to
// be read; Remove is the user saying they are done with it.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	s, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("remove %s: no such session", id)
	}
	delete(m.byID, id)
	for i, session := range m.sessions {
		if session.ID() == id {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			break
		}
	}
	m.mu.Unlock()

	// Closing outside the lock: it waits for reader goroutines, and holding
	// the lock through that would stall the UI.
	if err := s.Close(); err != nil {
		return fmt.Errorf("remove %s: %w", id, err)
	}
	return nil
}

// Close ends one session. The others keep running: one dead host is one dead
// pane.
func (m *Manager) Close(id string) error {
	s, ok := m.Session(id)
	if !ok {
		return fmt.Errorf("close %s: no such session", id)
	}
	return s.Close()
}

// CloseAll ends every session, concurrently, and reports the first failure.
func (m *Manager) CloseAll() error {
	sessions := m.Sessions()

	errs := make([]error, len(sessions))
	var wg sync.WaitGroup
	for i, s := range sessions {
		wg.Add(1)
		go func(i int, s Session) {
			defer wg.Done()
			errs[i] = s.Close()
		}(i, s)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// Resize informs every live session of a new window size and reports the first
// failure, having tried them all.
func (m *Manager) Resize(width, height int) error {
	var firstErr error
	for _, s := range m.Sessions() {
		if err := s.Resize(width, height); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Connected reports whether a host's session can take input right now. It is
// what the broadcast router asks before counting a host as a target: a session
// that is dialling, failed or closed has no remote shell to write to.
func (m *Manager) Connected(id string) bool {
	s, ok := m.Session(id)
	return ok && s.State() == StateConnected
}

// AltScreen reports whether a host's remote app is on the alternate screen —
// a full-screen app like vim owning that session's keyboard. The broadcast
// router asks so that all and selected mode keep their keystrokes out of it.
func (m *Manager) AltScreen(id string) bool {
	s, ok := m.Session(id)
	return ok && s.State() == StateConnected && s.Terminal().IsAltScreen()
}

// Writer returns a host's stdin, and whether there is one. A session that is not
// connected is deliberately not a writer: writing into a dead session would
// report success to a user who is about to assume the command ran.
func (m *Manager) Writer(id string) (io.Writer, bool) {
	s, ok := m.Session(id)
	if !ok || s.State() != StateConnected {
		return nil, false
	}
	return s, true
}

// Counts summarises the fleet for the status bar.
type Counts struct {
	Total     int
	Connected int
	Pending   int // not yet connected and not finished: pending, dialing, authenticating
	Failed    int
	Closed    int
}

// Counts reports how many sessions are in each group.
func (m *Manager) Counts() Counts {
	var c Counts
	for _, s := range m.Sessions() {
		c.Total++
		switch s.State() {
		case StateConnected:
			c.Connected++
		case StateFailed:
			c.Failed++
		case StateClosed:
			c.Closed++
		default:
			c.Pending++
		}
	}
	return c
}

// String renders the fleet summary the status bar shows.
func (c Counts) String() string {
	return fmt.Sprintf("%d/%d up", c.Connected, c.Total)
}

// ByState returns the identifiers of the sessions in a given state, in host
// order, which is how "jump to the next failed host" is built.
func (m *Manager) ByState(state State) []string {
	var ids []string
	for _, s := range m.Sessions() {
		if s.State() == state {
			ids = append(ids, s.ID())
		}
	}
	return ids
}

// IDs returns every session identifier in host order.
func (m *Manager) IDs() []string {
	sessions := m.Sessions()
	ids := make([]string, len(sessions))
	for i, s := range sessions {
		ids[i] = s.ID()
	}
	return ids
}

// SortedIDs returns the identifiers in lexical order, for displays that group by
// name rather than by the order the user typed.
func (m *Manager) SortedIDs() []string {
	ids := m.IDs()
	sort.Strings(ids)
	return ids
}
