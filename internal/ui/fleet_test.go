package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// gridApp is a sized app with the grid focused on its first pane.
func gridApp(t *testing.T) App {
	t.Helper()
	return focusGrid(t, resize(t, testApp(), 120, 40))
}

// keyMsgResult drives one key press and returns the message its command
// produces, or nil when there is no command.
func keyMsgResult(t *testing.T, a App, keystroke string) tea.Msg {
	t.Helper()
	model, cmd := a.Update(keyMsgFor(t, keystroke))
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestReconnectKeyEmitsReconnectHostMsg(t *testing.T) {
	got := keyMsgResult(t, gridApp(t), "alt+r")
	msg, ok := got.(ReconnectHostMsg)
	if !ok {
		t.Fatalf("pressing r produced %T, want ReconnectHostMsg", got)
	}
	if msg.ID != "web-01" {
		t.Errorf("ReconnectHostMsg.ID = %q, want the focused host web-01", msg.ID)
	}
}

func TestCloseKeyEmitsCloseHostMsg(t *testing.T) {
	got := keyMsgResult(t, gridApp(t), "alt+x")
	msg, ok := got.(CloseHostMsg)
	if !ok {
		t.Fatalf("pressing x produced %T, want CloseHostMsg", got)
	}
	if msg.ID != "web-01" {
		t.Errorf("CloseHostMsg.ID = %q, want the focused host web-01", msg.ID)
	}
}

func TestReconnectKeyWithoutHostsEmitsNothing(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "tab")
	if got := keyMsgResult(t, a, "r"); got != nil {
		t.Fatalf("pressing r with no hosts produced %T, want nothing", got)
	}
}

// x is state-dependent: a live host's session is closed, a dead host's pane
// is removed.
func TestCloseKeyOnADeadHostEmitsRemove(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.fail(t, "web-01")
	a = focusGrid(t, a)

	got := keyMsgResult(t, a, "alt+x")
	msg, ok := got.(RemoveHostMsg)
	if !ok {
		t.Fatalf("pressing x on a failed host produced %T, want RemoveHostMsg", got)
	}
	if msg.ID != "web-01" {
		t.Errorf("RemoveHostMsg.ID = %q", msg.ID)
	}
}

func TestCloseKeyOnALiveHostEmitsClose(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = focusGrid(t, a)

	if _, ok := keyMsgResult(t, a, "alt+x").(CloseHostMsg); !ok {
		t.Fatal("pressing alt+x on a connected host did not emit CloseHostMsg")
	}
}

// A host that left the run takes its scroll offset with it.
func TestRemovedHostScrollOffsetIsPruned(t *testing.T) {
	a, _ := scrollApp(t, 50)
	a = pressKey(t, a, "shift+home") // scroll to the top, recording an offset
	if len(a.scroll) == 0 {
		t.Fatal("setup: no scroll offset recorded")
	}

	model, _ := a.Update(HostsChangedMsg{Hosts: nil})
	next := model.(App)
	if len(next.scroll) != 0 {
		t.Fatalf("scroll offsets survived the host leaving: %v", next.scroll)
	}
}
