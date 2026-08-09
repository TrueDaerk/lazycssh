package program

import (
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/sessionlog"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
)

// buildWithLogs is testModel with an opt-in session log attached.
func buildWithLogs(t *testing.T, patterns ...string) (*Model, *sessionlog.Run) {
	t.Helper()

	logs, err := sessionlog.Open(t.TempDir(), time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sessionlog.Open: %v", err)
	}
	t.Cleanup(func() { logs.Close() })

	factory, _ := fakeFactory()
	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m, err := Build(t.Context(), Config{
		Patterns:   patterns,
		Store:      store,
		NewSession: factory,
		Resolver:   &hosts.Resolver{DefaultUser: "deploy"},
		Logs:       logs,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { m.Manager().CloseAll() })
	return m, logs
}

func TestSingleModeSuppressesTheSessionLog(t *testing.T) {
	m, logs := buildWithLogs(t, "web-01")

	if logs.Suppressed() {
		t.Fatal("logging starts suppressed")
	}
	if err := m.targets.SetMode(broadcast.ModeSingle); err != nil {
		t.Fatalf("SetMode(single): %v", err)
	}
	if !logs.Suppressed() {
		t.Fatal("single mode did not pause the session log")
	}
	if err := m.targets.SetMode(broadcast.ModeAll); err != nil {
		t.Fatalf("SetMode(all): %v", err)
	}
	if logs.Suppressed() {
		t.Fatal("leaving single mode did not resume the session log")
	}
}

func TestStartingInSingleModeSuppressesTheSessionLogFromTheFirstByte(t *testing.T) {
	logs, err := sessionlog.Open(t.TempDir(), time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("sessionlog.Open: %v", err)
	}
	t.Cleanup(func() { logs.Close() })

	factory, _ := fakeFactory()
	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m, err := Build(t.Context(), Config{
		Patterns:   []string{"web-01"},
		Broadcast:  broadcast.ModeSingle,
		Store:      store,
		NewSession: factory,
		Resolver:   &hosts.Resolver{DefaultUser: "deploy"},
		Logs:       logs,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { m.Manager().CloseAll() })

	if !logs.Suppressed() {
		t.Fatal("a run starting in single mode logs from the first byte")
	}
}

func TestModeSwitchesWithoutALogStillWork(t *testing.T) {
	m, _ := testModel(t, "web-01")
	if err := m.targets.SetMode(broadcast.ModeSingle); err != nil {
		t.Fatalf("SetMode without logs: %v", err)
	}
}
