package program

import (
	"path/filepath"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/history"
	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// TestBuildWiresPersistentHistory checks that a [history.Store] handed to
// [Config.History] reaches the command line: [App.Init]'s command reads the
// file, and the read lands in the model as the recall Up/Down walks.
func TestBuildWiresPersistentHistory(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history"), 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Append("uptime"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	factory, _ := fakeFactory()
	m, err := Build(t.Context(), Config{
		Patterns:   []string{"srv1"},
		NewSession: factory,
		Resolver:   &hosts.Resolver{DefaultUser: "deploy"},
		History:    store,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { m.Manager().CloseAll() })

	// app.Init, not m.Init: the fleet's own Init dials and arms pumps that
	// block on the manager's live event channel, which this test has no use
	// for and cannot drain synchronously.
	runCmd(t, m, m.app.Init())

	got := m.app.History()
	if len(got) != 1 || got[0] != "uptime" {
		t.Fatalf("History() = %v, want [uptime]", got)
	}
}

// TestBuildWithoutHistoryLeavesRecallEmpty checks that a run started without
// [Config.History] - the CLI's default, and every other test in this package
// - never touches a history file and starts with nothing to recall.
func TestBuildWithoutHistoryLeavesRecallEmpty(t *testing.T) {
	m, _ := testModel(t, "srv1")
	runCmd(t, m, m.app.Init())

	if got := m.app.History(); len(got) != 0 {
		t.Fatalf("History() = %v, want none", got)
	}
}
