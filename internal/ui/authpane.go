package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Concurrent per-pane auth prompts (issue #182): every dialling session may
// have its own question open at once - the prompt text sits in that session's
// scrollback (written by the program layer, issues #180/#177), and the answer
// is typed the way a terminal takes it: into the focused pane for one host, or
// through the broadcast line for every prompting target at once. Nothing is
// modal; a pane with a question simply takes typed input as its answer until
// it is submitted or cancelled.

// paneAuth is one pane's open auth question and the answer being typed for it.
// Exactly one of hostKey and secret is set.
type paneAuth struct {
	hostKey *HostKeyQuestionMsg
	secret  *SecretQuestionMsg
	answer  []rune
}

// authFor is the pane's open question, nil when none is.
func (a App) authFor(id string) *paneAuth { return a.auth[id] }

// AuthPending is how many auth questions are open.
func (a App) AuthPending() int { return len(a.auth) }

// addAuth records an opening question for its pane. The pane is not focused
// for it: ten questions arriving at once must not fight over the cursor - the
// prompts are in the panes, the status bar counts them, and the user picks
// where to answer (or answers all of them through the broadcast line).
func (a App) addAuth(id string, q paneAuth) App {
	if id == "" {
		return a
	}
	if a.auth == nil {
		a.auth = make(map[string]*paneAuth)
	}
	stored := q
	a.auth[id] = &stored
	return a
}

// dropAuth closes a pane's question without answering it.
func (a App) dropAuth(id string) App {
	delete(a.auth, id)
	return a
}

// pruneAuth drops questions whose session is gone or done: a dial that failed
// or was closed mid-question withdrew it on the program side already, and a
// stale buffer must not swallow keystrokes.
func (a App) pruneAuth() App {
	for id := range a.auth {
		if st, ok := a.hostStates[id]; !ok || st.state.Done() {
			delete(a.auth, id)
		}
	}
	return a
}

// handleAuthKey is typing into a pane's open question: characters append,
// backspace edits, enter submits, ctrl+c and esc cancel - the prompt behaves
// like the terminal prompt it echoes. ctrl+q still quits.
func (a App) handleAuthKey(id string, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	q := a.auth[id]
	if q == nil {
		return a, nil
	}
	switch msg.String() {
	case "enter":
		return a.submitAuth(id)
	case "esc", "ctrl+c":
		return a.cancelAuth(id)
	case "ctrl+q":
		return a, tea.Quit
	case "backspace":
		if n := len(q.answer); n > 0 {
			q.answer = q.answer[:n-1]
		}
		return a, nil
	}
	if msg.Text != "" {
		q.answer = append(q.answer, []rune(msg.Text)...)
	}
	return a, nil
}

// submitAuth answers a pane's question with what was typed. A host key
// question takes yes/y or no/n; anything else clears and asks again, so a
// keystroke meant for a shell cannot accidentally accept a key.
func (a App) submitAuth(id string) (App, tea.Cmd) {
	q := a.auth[id]
	if q == nil {
		return a, nil
	}
	if q.hostKey != nil {
		switch strings.ToLower(strings.TrimSpace(string(q.answer))) {
		case "yes", "y":
			a = a.dropAuth(id)
			return a, func() tea.Msg { return HostKeyAnswerMsg{SessionID: id, Accept: true} }
		case "no", "n":
			a = a.dropAuth(id)
			return a, func() tea.Msg { return HostKeyAnswerMsg{SessionID: id, Accept: false} }
		}
		q.answer = nil
		return a, nil
	}
	value := string(q.answer)
	a = a.dropAuth(id)
	return a, func() tea.Msg { return SecretAnswerMsg{SessionID: id, Value: value, Ok: true} }
}

// cancelAuth fails a pane's question: a host key question is rejected, a
// secret prompt cancelled - that authentication attempt fails rather than
// hanging.
func (a App) cancelAuth(id string) (App, tea.Cmd) {
	q := a.auth[id]
	if q == nil {
		return a, nil
	}
	a = a.dropAuth(id)
	if q.hostKey != nil {
		return a, func() tea.Msg { return HostKeyAnswerMsg{SessionID: id, Accept: false} }
	}
	return a, func() tea.Msg { return SecretAnswerMsg{SessionID: id} }
}

// feedAuthBroadcast mirrors a broadcast keystroke into every prompting target
// pane: characters append, backspace edits, enter submits each, ctrl+c
// cancels each - one typing action answers a uniform cluster. It reports how
// many prompts took the keystroke, so the caller can tell "nothing listened"
// from "the prompts did".
func (a App) feedAuthBroadcast(msg tea.KeyPressMsg) (App, []tea.Cmd, int) {
	if len(a.auth) == 0 || a.cfg.Targets == nil {
		return a, nil, 0
	}
	var cmds []tea.Cmd
	fed := 0
	for _, id := range a.cfg.Targets.Targets() {
		if a.auth[id] == nil {
			continue
		}
		fed++
		switch msg.String() {
		case "enter":
			var cmd tea.Cmd
			a, cmd = a.submitAuth(id)
			cmds = append(cmds, cmd)
		case "ctrl+c":
			var cmd tea.Cmd
			a, cmd = a.cancelAuth(id)
			cmds = append(cmds, cmd)
		default:
			model, _ := a.handleAuthKey(id, msg)
			a = model.(App)
		}
	}
	return a, cmds, fed
}

// inlineAnswerEcho is what a pane appends to its last scrollback line while
// its question is open: the answer being typed - the yes/no of a host key
// question, an echoing keyboard-interactive answer - and a cursor block, so
// the prompt visibly awaits input. A masked answer echoes only the cursor,
// which is all a terminal shows of a typed password.
func (a App) inlineAnswerEcho(id string) (string, bool) {
	q := a.auth[id]
	if q == nil {
		return "", false
	}
	echo := ""
	if q.hostKey != nil || (q.secret != nil && q.secret.Echo) {
		echo = string(q.answer)
	}
	return echo + a.theme.Cursor.Render(" "), true
}

// authStatusLabel is the status bar's AUTH segment while questions are open,
// empty otherwise.
func (a App) authStatusLabel() string {
	switch n := len(a.auth); n {
	case 0:
		return ""
	case 1:
		for id := range a.auth {
			return "AUTH " + id
		}
		return "" // unreachable
	default:
		return fmt.Sprintf("AUTH %d hosts", n)
	}
}

// authPanelLines is the Status panel's summary of the open questions, host
// names sorted so the line is stable across redraws.
func (a App) authPanelLines() []string {
	if len(a.auth) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.auth))
	for id := range a.auth {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	n := len(ids)
	return []string{
		a.theme.StatusWarning.Render(fmt.Sprintf("auth: %d prompt%s open — %s", n, plural(n), strings.Join(ids, " "))),
		a.theme.Muted.Render("type in the pane, or in the broadcast line for all of them"),
	}
}
