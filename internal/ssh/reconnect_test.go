package ssh

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

func TestReconnectPreservesScrollbackWithASeparator(t *testing.T) {
	m, lookup := newTestManager(t, fakeFleet(2), nil)
	m.Start(t.Context())
	m.Wait()

	lookup("srv1").Emit("important error before the crash\r\n")
	lookup("srv1").Disconnect(ErrDisconnected())

	if err := m.Reconnect(t.Context(), "srv1"); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	m.Wait()

	after, _ := m.Session("srv1")
	got := after.Scrollback().String()

	// The pane that just died is exactly the pane whose last lines matter.
	if !strings.Contains(got, "important error before the crash") {
		t.Errorf("scrollback = %q, want the output from before the disconnect", got)
	}
	// And the new connection must not read as a continuation of the old one.
	if !strings.Contains(got, reconnectMarker) {
		t.Errorf("scrollback = %q, want a separator marking the reconnect", got)
	}
	if !strings.Contains(got, "welcome to srv1") {
		t.Errorf("scrollback = %q, want the new connection's output too", got)
	}

	// Order matters: old output, separator, new output.
	oldIdx := strings.Index(got, "important error")
	sepIdx := strings.Index(got, reconnectMarker)
	newIdx := strings.LastIndex(got, "welcome to srv1")
	if !(oldIdx < sepIdx && sepIdx < newIdx) {
		t.Errorf("scrollback is out of order (old %d, separator %d, new %d):\n%s", oldIdx, sepIdx, newIdx, got)
	}

	// Another host's scrollback is untouched.
	other, _ := m.Session("srv2")
	if strings.Contains(other.Scrollback().String(), reconnectMarker) {
		t.Error("reconnecting one host wrote into another host's scrollback")
	}
}

func TestReconnectFromEveryEndState(t *testing.T) {
	tests := []struct {
		name string
		kill func(*Fake)
	}{
		{name: "from failed", kill: func(f *Fake) { f.Disconnect(ErrDisconnected()) }},
		{name: "from closed", kill: func(f *Fake) { f.Close() }},
		{name: "from a remote exit", kill: func(f *Fake) { f.ExitWithStatus(1) }},
		{name: "from connected", kill: func(f *Fake) {}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, lookup := newTestManager(t, fakeFleet(1), nil)
			m.Start(t.Context())
			m.Wait()

			tt.kill(lookup("srv1"))

			if err := m.Reconnect(t.Context(), "srv1"); err != nil {
				t.Fatalf("Reconnect: %v", err)
			}
			m.Wait()

			s, _ := m.Session("srv1")
			if got := s.State(); got != StateConnected {
				t.Errorf("State() = %s, want %s", got, StateConnected)
			}
			if err := s.Err(); err != nil {
				t.Errorf("Err() = %v, want the previous failure to be cleared", err)
			}
		})
	}
}

func TestReconnectTouchesNoOtherSession(t *testing.T) {
	m, lookup := newTestManager(t, fakeFleet(5), nil)
	m.Start(t.Context())
	m.Wait()

	before := make(map[string]Session)
	for _, id := range m.IDs() {
		s, _ := m.Session(id)
		before[id] = s
		lookup(id).Emit("state of " + id + "\r\n")
	}

	if err := m.Reconnect(t.Context(), "srv3"); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	m.Wait()

	for _, id := range m.IDs() {
		s, _ := m.Session(id)
		if id == "srv3" {
			if s == before[id] {
				t.Error("srv3 was not replaced")
			}
			continue
		}
		if s != before[id] {
			t.Errorf("%s was replaced although only srv3 was reconnected", id)
		}
		if got := s.State(); got != StateConnected {
			t.Errorf("%s State() = %s, want it still connected", id, got)
		}
		if !strings.Contains(s.Scrollback().String(), "state of "+id) {
			t.Errorf("%s lost its scrollback", id)
		}
	}

	if got := m.Counts(); got.Connected != 5 {
		t.Errorf("Counts() = %+v, want all five connected", got)
	}
}

// TestReconnectDoesNotRePromptForCredentials uses real sessions against the
// in-process server: the credential cache is the thing under test, and a fake
// never touches it.
func TestReconnectDoesNotRePromptForCredentials(t *testing.T) {
	srv := newTestServer(t)
	addr, port := srv.Addr()
	host := hosts.Host{Alias: "test-host", Addr: addr, User: "deploy", Port: port}

	var prompts atomic.Int32
	creds := &Credentials{
		DisableAgent: true,
		Prompter: FuncPrompter{
			PasswordFunc: func(context.Context, hosts.Host) (string, error) {
				prompts.Add(1)
				return srv.Password, nil
			},
		},
	}

	m, err := NewManager(ManagerConfig{
		Hosts: []hosts.Host{host},
		NewSession: func(req SessionRequest) Session {
			return New(req.ID, Config{
				Host:            req.Host,
				Auth:            creds.Methods(context.Background(), req.ID, req.Host),
				HostKeyCallback: srv.HostKeyCallback(),
				Timeout:         5 * time.Second,
				Scrollback:      req.Scrollback,
			}, req.Events)
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.CloseAll() })

	m.Start(t.Context())
	m.Wait()

	s, _ := m.Session("test-host")
	waitForOutput(t, s, "welcome")
	if got := prompts.Load(); got != 1 {
		t.Fatalf("prompted %d times for the first connection, want 1", got)
	}

	for i := 0; i < 3; i++ {
		if err := m.Reconnect(t.Context(), "test-host"); err != nil {
			t.Fatalf("Reconnect %d: %v", i, err)
		}
		m.Wait()

		s, _ := m.Session("test-host")
		if got := s.State(); got != StateConnected {
			t.Fatalf("State() after reconnect %d = %s, want %s", i, got, StateConnected)
		}
		waitForOutput(t, s, "welcome")
	}

	if got := prompts.Load(); got != 1 {
		t.Errorf("the user was asked %d times across three reconnects, want 1: a credential already in memory must be reused", got)
	}
}

// TestReconnectKeepsScrollbackAcrossRealSessions checks the buffer really is the
// same object once a real session adopts it.
func TestReconnectKeepsScrollbackAcrossRealSessions(t *testing.T) {
	srv := newTestServer(t)
	addr, port := srv.Addr()
	host := hosts.Host{Alias: "test-host", Addr: addr, User: "deploy", Port: port}

	m, err := NewManager(ManagerConfig{
		Hosts: []hosts.Host{host},
		NewSession: func(req SessionRequest) Session {
			return New(req.ID, Config{
				Host:            req.Host,
				Auth:            passwordAuth(srv),
				HostKeyCallback: srv.HostKeyCallback(),
				Timeout:         5 * time.Second,
				Scrollback:      req.Scrollback,
			}, req.Events)
		},
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.CloseAll() })

	m.Start(t.Context())
	m.Wait()

	s, _ := m.Session("test-host")
	waitForOutput(t, s, "welcome")

	if err := m.Reconnect(t.Context(), "test-host"); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	m.Wait()

	s, _ = m.Session("test-host")
	waitFor(t, "the reconnected session to greet us again", func() bool {
		return strings.Count(s.Scrollback().String(), "welcome") == 2
	})
	if got := s.Scrollback().String(); !strings.Contains(got, reconnectMarker) {
		t.Errorf("scrollback = %q, want the separator between the two connections", got)
	}
}
