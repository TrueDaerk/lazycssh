package program

import (
	"fmt"
	"strings"

	"github.com/TrueDaerk/lazycssh/internal/scrollback"
)

// The terminal-style auth echo (issue #180): questions and their answers are
// written into the blocked session's scrollback in ssh's own wording, so the
// pane reads exactly like a plain terminal would - prompt, inline answer,
// newline - and the history still says afterwards what was asked and answered.
// Secrets are never echoed: a masked answer writes only the newline.

// promptScrollback finds the scrollback of the session an auth question names.
// Questions carry the host alias; session ids are the alias, or alias#n when
// the same alias was dialled twice, in which case the first one is echoed to.
// Nil when the run has no such session (or no manager, as tests may not).
func (m *Model) promptScrollback(alias string) *scrollback.Buffer {
	if m.mgr == nil || alias == "" {
		return nil
	}
	if s, ok := m.mgr.Session(alias); ok {
		return s.Scrollback()
	}
	for _, id := range m.mgr.IDs() {
		if strings.HasPrefix(id, alias+"#") {
			if s, ok := m.mgr.Session(id); ok {
				return s.Scrollback()
			}
		}
	}
	return nil
}

// echoPrompt writes a prompt into a session's scrollback, on its own line when
// output already ran - "\r\n" first commits whatever partial line the host
// left - and as the first line otherwise, the way ssh's own prompt appears.
func echoPrompt(buf *scrollback.Buffer, prompt string) {
	if buf == nil {
		return
	}
	if buf.Written() > 0 {
		prompt = "\r\n" + prompt
	}
	buf.Write([]byte(prompt))
}

// echoAnswer finishes the prompt line: the shown answer for a question that
// echoes, a bare newline for a masked one - which is all a terminal shows of a
// typed password.
func echoAnswer(buf *scrollback.Buffer, shown string) {
	if buf == nil {
		return
	}
	buf.Write([]byte(shown + "\r\n"))
}

// hostKeyPromptText is ssh's own known-hosts question, ending in the open
// "(yes/no)? " line the typed answer completes.
func hostKeyPromptText(q *keyQuestion) string {
	return fmt.Sprintf("The authenticity of host '%s (%s)' can't be established.\r\n"+
		"%s key fingerprint is %s.\r\n"+
		"Are you sure you want to continue connecting (yes/no)? ",
		q.host.Alias, q.host.Addr, q.keyType, q.fingerprint)
}
