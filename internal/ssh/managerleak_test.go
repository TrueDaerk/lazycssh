package ssh

import (
	"runtime"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// TestManagerReconnectLeaksNoGoroutines is the manager-level counterpart of
// TestSessionLeaksNoGoroutines: a reconnect hands the emulator to the fresh
// session and closes the old one, and none of that may accumulate goroutines
// over a long run of flapping hosts (issue #274 measured exactly this cycle).
func TestManagerReconnectLeaksNoGoroutines(t *testing.T) {
	srv := newTestServer(t)
	addr, port := srv.Addr()

	factory := func(req SessionRequest) Session {
		return New(req.ID, Config{
			Host:            req.Host,
			Auth:            []ssh.AuthMethod{ssh.Password(srv.Password)},
			HostKeyCallback: srv.HostKeyCallback(),
			Timeout:         5 * time.Second,
			Terminal:        req.Terminal,
		}, req.Events)
	}
	m, err := NewManager(ManagerConfig{
		Hosts:      []hosts.Host{{Alias: "h1", Addr: addr, User: "tester", Port: port}},
		NewSession: factory,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	go func() {
		for range m.Events() {
		}
	}()

	m.Start(t.Context())
	m.Wait()
	before := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		if err := m.Reconnect(t.Context(), "h1"); err != nil {
			t.Fatalf("Reconnect %d: %v", i, err)
		}
		m.Wait()
	}

	if err := m.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	m.Wait()

	waitFor(t, "goroutines to settle", func() bool {
		// Below the connected baseline: the fleet is closed, so all that may
		// remain is the server's own housekeeping.
		return runtime.NumGoroutine() <= before
	})
}
