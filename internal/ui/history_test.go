package ui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/commandlog"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// fakeHistoryStore is a [HistoryStore] the tests can preload and inspect,
// without touching a disk.
type fakeHistoryStore struct {
	entries []string
	loadErr error

	appended  []string
	appendErr error
}

func (f *fakeHistoryStore) Load() ([]string, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return append([]string(nil), f.entries...), nil
}

func (f *fakeHistoryStore) Append(command string) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	f.appended = append(f.appended, command)
	return nil
}

// historyApp builds a one-host app wired to a fake sender and the given
// history store, for testing recall persistence without a fleet.
func historyApp(t *testing.T, store HistoryStore) App {
	t.Helper()

	fleet := newFakeFleet("web-01")
	ws := workingset.New(fleet.IDs())
	router, err := broadcast.NewRouter(ws)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	sender := &fakeSender{delivery: broadcast.Delivery{
		Mode: broadcast.ModeAll, Scope: 1, Targets: 1, Delivered: 1,
	}}
	log := commandlog.New(0)

	return resize(t, NewApp(Config{
		Fleet:      fleet,
		Targets:    router,
		WorkingSet: ws,
		Sender:     sender,
		Recorder:   log,
		Panes:      fleet,
		CommandLog: log,
		History:    store,
		Theme:      Options{Dark: true},
	}), 120, 40)
}

func TestHistoryLoadsFromTheStoreOnInit(t *testing.T) {
	store := &fakeHistoryStore{entries: []string{"uptime", "df -h"}}
	a := historyApp(t, store)

	a = settle(t, a, a.loadHistoryCmd())
	if got := strings.Join(a.History(), ","); got != "uptime,df -h" {
		t.Fatalf("History() = %q", got)
	}

	a = pressKey(t, a, ":")
	a = pressArrow(t, a, tea.KeyUp)
	if a.CommandLineValue() != "df -h" {
		t.Fatalf("recall = %q, want the most recently loaded entry", a.CommandLineValue())
	}
}

func TestHistoryLoadErrorIsReported(t *testing.T) {
	store := &fakeHistoryStore{loadErr: errors.New("boom")}
	a := historyApp(t, store)

	a = settle(t, a, a.loadHistoryCmd())
	if !strings.Contains(a.LastDelivery(), "boom") {
		t.Fatalf("LastDelivery() = %q, want it to mention the load error", a.LastDelivery())
	}
}

// A history file is disk I/O; a store that has not resolved yet must not
// erase a command sent while it was still loading.
func TestHistoryLoadDoesNotLoseWhatWasAlreadySent(t *testing.T) {
	store := &fakeHistoryStore{entries: []string{"old"}}
	a := historyApp(t, store)

	a = typeCommand(t, a, "new")
	a, _ = enter(t, a)

	a = settle(t, a, a.loadHistoryCmd())

	if got := strings.Join(a.History(), ","); got != "old,new" {
		t.Fatalf("History() = %q, want the loaded entry ahead of what was sent", got)
	}
}

func TestSendPersistsToTheHistoryStore(t *testing.T) {
	store := &fakeHistoryStore{}
	a := historyApp(t, store)

	a = typeCommand(t, a, "uptime")
	a, _ = enter(t, a)

	if got := strings.Join(store.appended, ","); got != "uptime" {
		t.Fatalf("store.appended = %v, want [uptime]", store.appended)
	}
}

func TestSendSkipsAConsecutiveRepeatInTheStore(t *testing.T) {
	store := &fakeHistoryStore{}
	a := historyApp(t, store)

	for range 2 {
		a = typeCommand(t, a, "uptime")
		a, _ = enter(t, a)
	}

	if len(store.appended) != 2 {
		t.Fatalf("store.appended = %v, want two calls; the dedup is the store's job, not the caller's",
			store.appended)
	}
}

// A history write failure must not block the send it rode in on.
func TestSendSurvivesAHistoryWriteFailure(t *testing.T) {
	store := &fakeHistoryStore{appendErr: errors.New("disk full")}
	a := historyApp(t, store)
	sender := a.cfg.Sender.(*fakeSender)

	a = typeCommand(t, a, "uptime")
	a, _ = enter(t, a)

	if len(sender.sent) != 1 {
		t.Fatalf("sent = %v, want the command delivered despite the history write failing", sender.sent)
	}
}

func TestSendWithoutAHistoryStoreDoesNotPanic(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01")

	a = typeCommand(t, a, "uptime")
	a, _ = enter(t, a)
}
