package program

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/recent"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// recentModel builds a run whose recent-host list is a file in a temporary
// directory, so what the picker would offer next time is inspectable here.
func recentModel(t *testing.T, patterns ...string) (*Model, *recent.Store, func(id string) *ssh.Fake) {
	t.Helper()

	factory, lookup := fakeFactory()
	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	list, err := recent.NewStore(filepath.Join(t.TempDir(), "recent"), 0)
	if err != nil {
		t.Fatalf("recent.NewStore: %v", err)
	}
	m, err := Build(t.Context(), Config{
		Patterns:   patterns,
		Store:      store,
		Recent:     list,
		NewSession: factory,
		Resolver:   &hosts.Resolver{DefaultUser: "deploy"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { m.Manager().CloseAll() })
	return m, list, lookup
}

// recorded is the recent list as it stands on disk.
func recorded(t *testing.T, list *recent.Store) []string {
	t.Helper()
	hosts, err := list.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return hosts
}

// Every session that reaches connected is written to the recent list, most
// recent first, so the next run's picker offers it (issue #254).
func TestConnectedHostsAreRecordedAsRecent(t *testing.T) {
	m, list, _ := recentModel(t, "srv1", "srv2")
	m.Init()
	m.Manager().Wait()

	runCmd(t, m, m.recordRecent())

	if got, want := recorded(t, list), []string{"srv2", "srv1"}; !slices.Equal(got, want) {
		t.Fatalf("recent list = %v, want %v", got, want)
	}
}

// A host that never connected is not a recent host: the list is what answered,
// not what was asked for.
func TestFailedHostsAreNotRecordedAsRecent(t *testing.T) {
	factory, _ := fakeFactory()
	list, err := recent.NewStore(filepath.Join(t.TempDir(), "recent"), 0)
	if err != nil {
		t.Fatalf("recent.NewStore: %v", err)
	}
	m, err := Build(t.Context(), Config{
		Patterns: []string{"srv1", "srv2"},
		Recent:   list,
		NewSession: func(req ssh.SessionRequest) ssh.Session {
			s := factory(req)
			if req.ID == "srv1" {
				s.(*ssh.Fake).DialErr = errors.New("connection refused")
			}
			return s
		},
		Resolver: &hosts.Resolver{DefaultUser: "deploy"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { m.Manager().CloseAll() })

	m.Init()
	m.Manager().Wait()
	runCmd(t, m, m.recordRecent())

	if got, want := recorded(t, list), []string{"srv2"}; !slices.Equal(got, want) {
		t.Fatalf("recent list = %v, want %v", got, want)
	}
}

// The list records the resolved host alias, not the session identifier: a
// clone's `srv1#2` is not something a later run could connect to.
func TestRecentRecordsTheAliasNotTheSessionID(t *testing.T) {
	m, list, _ := recentModel(t, "srv1")
	m.Init()
	m.Manager().Wait()
	runCmd(t, m, m.recordRecent())

	settle(t, m, ui.CloneHostMsg{ID: "srv1"})
	m.Manager().Wait()
	runCmd(t, m, m.recordRecent())

	if got, want := recorded(t, list), []string{"srv1"}; !slices.Equal(got, want) {
		t.Fatalf("recent list = %v, want %v", got, want)
	}
}

// A session already recorded this run is not recorded again, so a fleet that
// reports state repeatedly does not rewrite the file on every event.
func TestRecentRecordsEachSessionOnce(t *testing.T) {
	m, list, _ := recentModel(t, "srv1", "srv2")
	m.Init()
	m.Manager().Wait()
	runCmd(t, m, m.recordRecent())

	if cmd := m.recordRecent(); cmd != nil {
		t.Fatal("a second pass produced another write")
	}
	if got, want := recorded(t, list), []string{"srv2", "srv1"}; !slices.Equal(got, want) {
		t.Fatalf("recent list = %v, want %v", got, want)
	}
}

// A run without a recent store writes nothing and does not fall over: that is
// every test in this package that builds a Model without one.
func TestRecentWithoutAStoreIsANoOp(t *testing.T) {
	m, _ := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()

	if cmd := m.recordRecent(); cmd != nil {
		t.Fatal("a run without a recent store produced a write")
	}
}
