package program

import (
	"fmt"

	"github.com/TrueDaerk/lazycssh/internal/term"
)

// The terminal-style auth echo (issue #180): questions and their answers are
// written into the blocked session's terminal in ssh's own wording, so the
// pane reads exactly like a plain terminal would - prompt, inline answer,
// newline - and the history still says afterwards what was asked and answered.
// Secrets are never echoed: a masked answer writes only the newline.

// promptTerminal is the terminal of the session an auth question names - by
// session id, exact: ten dials of the same alias are ten panes, and each
// question echoes into its own (issue #182). Nil when the run has no such
// session (or no manager, as tests may not).
func (m *Model) promptTerminal(sessionID string) *term.Emulator {
	if m.mgr == nil || sessionID == "" {
		return nil
	}
	s, ok := m.mgr.Session(sessionID)
	if !ok {
		return nil
	}
	return s.Terminal()
}

// echoPrompt writes a prompt into a session's terminal, on its own line when
// output already ran - "\r\n" first commits whatever partial line the host
// left - and as the first line otherwise, the way ssh's own prompt appears.
func echoPrompt(emu *term.Emulator, prompt string) {
	if emu == nil {
		return
	}
	if emu.HasOutput() {
		prompt = "\r\n" + prompt
	}
	_, _ = emu.Write([]byte(prompt))
}

// echoAnswer finishes the prompt line: the shown answer for a question that
// echoes, a bare newline for a masked one - which is all a terminal shows of a
// typed password.
func echoAnswer(emu *term.Emulator, shown string) {
	if emu == nil {
		return
	}
	_, _ = emu.Write([]byte(shown + "\r\n"))
}

// hostKeyPromptText is ssh's own known-hosts question, ending in the open
// "(yes/no)? " line the typed answer completes.
func hostKeyPromptText(q *keyQuestion) string {
	return fmt.Sprintf("The authenticity of host '%s (%s)' can't be established.\r\n"+
		"%s key fingerprint is %s.\r\n"+
		"Are you sure you want to continue connecting (yes/no)? ",
		q.host.Alias, q.host.Addr, q.keyType, q.fingerprint)
}
