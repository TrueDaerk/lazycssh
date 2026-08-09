package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/term"
)

// CommandSentMsg reports what one send did. It is emitted rather than only
// rendered so anything above the model - a log, a test, a future scripting
// hook - sees the same report the user does.
type CommandSentMsg struct {
	// Command is what was sent.
	Command string
	// Delivery is what happened to it.
	Delivery broadcast.Delivery
}

// CommandLine is what the command line and the broadcast bar need to send.
// [broadcast.Router] satisfies it; it is an interface so the UI can be tested
// without a transport.
//
// Both methods are called inline from Update: they must never block on the
// network. The transport honours that with a per-session stdin queue - a
// stalled host refuses the write rather than stalling the loop (issue #225).
type CommandLine interface {
	// Send writes the same bytes to the active broadcast set — whole command
	// lines, where the bytes are plain text.
	Send(p []byte) (broadcast.Delivery, error)
	// SendKey delivers one key press to the active broadcast set, encoded per
	// host by each session's own terminal emulator (issue #206).
	SendKey(k term.KeyEvent) (broadcast.Delivery, error)
}

// Recorder is the command log as the command line writes to it.
type Recorder interface {
	// Record appends a command and reports whether it was recorded. Input sent
	// in single mode is deliberately not; see internal/commandlog.
	Record(command string, mode broadcast.Mode, targets int) bool
}

// HistoryStore is the persistent broadcast command history: what Up/Down
// walks, loaded once on start and appended to on every send so the recall
// survives a restart. It is separate from [Recorder]: the log there is the
// audit trail of what was sent to hosts, this is what was typed at the
// prompt, and only that - never a raw broadcast keystroke - reaches it.
type HistoryStore interface {
	// Load returns the saved commands, oldest first.
	Load() ([]string, error)
	// Append adds a command, once per consecutive repeat.
	Append(command string) error
}

// HistoryLoadedMsg carries a read of the on-disk command history back into
// Update after [App.Init] asks for it. The read is disk I/O, so it happens in
// a tea.Cmd; Update never blocks (issue #225).
type HistoryLoadedMsg struct {
	// Entries are the saved commands, oldest first.
	Entries []string
	// Err is why the file could not be read, or nil.
	Err error
}

// loadHistoryCmd reads the persistent command history off the Update loop.
// The result arrives as a [HistoryLoadedMsg].
func (a App) loadHistoryCmd() tea.Cmd {
	store := a.cfg.History
	if store == nil {
		return nil
	}
	return func() tea.Msg {
		entries, err := store.Load()
		return HistoryLoadedMsg{Entries: entries, Err: err}
	}
}

// applyHistoryLoaded seeds Up/Down recall with a read of the history file. The
// load races Init's first render against whatever the user manages to type and
// send before it returns, so the loaded entries are prepended rather than
// replacing what is there: anything sent already this run stays, in order,
// after the older entries the file remembers.
func (a App) applyHistoryLoaded(msg HistoryLoadedMsg) App {
	if msg.Err != nil {
		a.lastDelivery = "history: " + msg.Err.Error()
		return a
	}
	a.cmdHistory = append(append([]string(nil), msg.Entries...), a.cmdHistory...)
	a.cmdHistoryPos = len(a.cmdHistory)
	return a
}

// CommandLineOpen reports whether the command line has the keyboard.
func (a App) CommandLineOpen() bool { return a.cmdInput.Focused() }

// CommandLineValue is what has been typed into the command line.
func (a App) CommandLineValue() string { return a.cmdInput.Value() }

// LastDelivery is the report from the last command sent, empty before the first
// one.
func (a App) LastDelivery() string { return a.lastDelivery }

// History returns the commands typed this run, oldest first.
func (a App) History() []string { return append([]string(nil), a.cmdHistory...) }

// openCommandLine gives the keyboard to the command line.
func (a App) openCommandLine() App {
	a.cmdInput.SetValue("")
	a.cmdInput.Focus()
	a.cmdHistoryPos = len(a.cmdHistory)
	return a
}

// closeCommandLine takes the keyboard back without sending anything.
func (a App) closeCommandLine() App {
	a.cmdInput.Blur()
	a.cmdInput.SetValue("")
	a.cmdHistoryPos = len(a.cmdHistory)
	return a
}

// handleCommandLineKey drives the command line.
//
// While it is open every key belongs to it. A command containing a `b` or a `:`
// must not switch the broadcast mode or open a second prompt, and a `ctrl+c`
// while editing must not reach forty machines.
func (a App) handleCommandLineKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.PromptSubmit):
		return a.sendCommandLine()
	case key.Matches(msg, a.keys.PromptCancel):
		return a.closeCommandLine(), nil
	case key.Matches(msg, a.keys.HistoryPrev):
		return a.recallHistory(-1), nil
	case key.Matches(msg, a.keys.HistoryNext):
		return a.recallHistory(+1), nil
	}

	var cmd tea.Cmd
	a.cmdInput, cmd = a.cmdInput.Update(msg)
	return a, cmd
}

// recallHistory walks the command history. Moving past the newest entry returns
// to an empty line, which is where a user expects "down" to end up.
func (a App) recallHistory(delta int) App {
	if len(a.cmdHistory) == 0 {
		return a
	}

	pos := clamp(a.cmdHistoryPos+delta, 0, len(a.cmdHistory))
	a.cmdHistoryPos = pos
	if pos == len(a.cmdHistory) {
		a.cmdInput.SetValue("")
		return a
	}
	a.cmdInput.SetValue(a.cmdHistory[pos])
	a.cmdInput.CursorEnd()
	return a
}

// sendCommandLine sends what was typed to the active broadcast set.
//
// There is no confirmation dialog - broadcasting to N hosts is the point of the
// tool - but the target count was on screen the whole time it was being typed,
// and the report says how many hosts actually received it.
func (a App) sendCommandLine() (tea.Model, tea.Cmd) {
	command := strings.TrimSpace(a.cmdInput.Value())
	if command == "" {
		return a.closeCommandLine(), nil
	}

	a.cmdHistory = appendHistory(a.cmdHistory, command)
	if a.cfg.History != nil {
		// Best effort: a disk write failing must not block a send that is
		// otherwise fine, and the in-memory copy Up/Down already walks is
		// unaffected either way.
		_ = a.cfg.History.Append(command)
	}
	a = a.closeCommandLine()

	// A find instruction is for lazycssh too: it sets the shared search term
	// and reports which hosts matched, and nothing reaches a remote shell.
	if next, report, meta := a.applyFind(command); meta {
		next.lastDelivery = report
		return next, nil
	}

	// A selection instruction is for lazycssh, not for the hosts: nothing is
	// written to a remote shell and nothing is recorded as a command.
	if report, meta := a.applySelection(command); meta {
		a.lastDelivery = report
		return a, nil
	}

	return a.sendCommand(command)
}

// sendCommand delivers one command to the active broadcast set and records it.
//
// Typing a command and resending one from the Command log take the same path,
// so a resend goes to the set that is active now, gets the same report and
// leaves the same audit entry.
func (a App) sendCommand(command string) (tea.Model, tea.Cmd) {
	command = strings.TrimSpace(command)
	if command == "" {
		return a, nil
	}

	if a.cfg.Sender == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}

	// Where each host's exit marker stands *before* the command goes out. Read
	// after the write it would be a race against a host that answers fast, and
	// the answer would be mistaken for the state the send found.
	marks := a.exitMarksAtSend()

	// The newline is what makes it a command rather than a line of typing.
	delivery, err := a.cfg.Sender.Send([]byte(command + "\n"))
	a.lastDelivery = delivery.String()
	if err != nil {
		a.lastDelivery += ": " + err.Error()
	}

	if a.cfg.Recorder != nil {
		a.cfg.Recorder.Record(command, delivery.Mode, delivery.Delivered)
	}

	// The send opens two windows on the same question: the Output diff
	// panel's, where each reached target's scrollback length now is where its
	// answer starts, and the pane headers' exit indicator, which greys out
	// until this command's own exit marker comes back (issue #251).
	a = a.markDiff(command, delivery)
	a = a.markCommandExits(marks, delivery)

	// The send moved the window the output filter matches in - it is "since
	// the last command" - so an active filter is re-evaluated against the new,
	// empty window rather than against the previous command's answers.
	a = a.refreshFilter()

	sent := CommandSentMsg{Command: command, Delivery: delivery}
	return a, func() tea.Msg { return sent }
}

// appendHistory adds a command, without repeating the previous one: holding
// enter on the same command should not fill the history with it.
func appendHistory(history []string, command string) []string {
	if n := len(history); n > 0 && history[n-1] == command {
		return history
	}
	return append(history, command)
}

// renderCommandLine draws the prompt. The broadcast scope is part of the prompt
// rather than only the status bar, so the number of machines about to receive
// the command is on screen at the moment of typing it.
func (a App) renderCommandLine() string {
	scope := a.theme.Base
	label := "BROADCAST (not started)"
	if a.cfg.Targets != nil {
		label = a.cfg.Targets.Describe()
		if a.cfg.Targets.Warning() {
			scope = a.theme.StatusWarning
		}
	}

	line := a.theme.Base.Render(":"+a.cmdInput.Value()) + " " + scope.Render("→ "+label)
	return a.theme.StatusBar.
		Width(a.layout.Width).
		MaxHeight(StatusBarHeight).
		Render(line)
}
