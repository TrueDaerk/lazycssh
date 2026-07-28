package ssh

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// Defaults for a manager that does not configure them.
const (
	// DefaultMaxParallelDials bounds how many connections are opened at once.
	// Two hundred hosts must not mean two hundred simultaneous sockets, file
	// descriptors and key exchanges.
	DefaultMaxParallelDials = 16
	// DefaultEventBuffer is the depth of the fan-in channel.
	DefaultEventBuffer = 512
)

// Factory builds one session. It exists so the manager can be driven by fakes:
// every manager test runs without a network.
type Factory func(id string, host hosts.Host, events chan<- Event) Session

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
		s := cfg.NewSession(id, host, m.events)
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

// Reconnect replaces one session with a fresh one for the same host and dials
// it. The identifier is preserved, so panes and selections survive.
func (m *Manager) Reconnect(ctx context.Context, id string) error {
	m.mu.Lock()
	old, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("reconnect %s: no such session", id)
	}

	fresh := m.cfg.NewSession(id, old.Host(), m.events)
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
