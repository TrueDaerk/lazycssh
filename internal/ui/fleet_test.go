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
	model, cmd := a.Update(tea.KeyPressMsg{Code: []rune(keystroke)[0], Text: keystroke})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestReconnectKeyEmitsReconnectHostMsg(t *testing.T) {
	got := keyMsgResult(t, gridApp(t), "r")
	msg, ok := got.(ReconnectHostMsg)
	if !ok {
		t.Fatalf("pressing r produced %T, want ReconnectHostMsg", got)
	}
	if msg.ID != "web-01" {
		t.Errorf("ReconnectHostMsg.ID = %q, want the focused host web-01", msg.ID)
	}
}

func TestCloseKeyEmitsCloseHostMsg(t *testing.T) {
	got := keyMsgResult(t, gridApp(t), "x")
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
