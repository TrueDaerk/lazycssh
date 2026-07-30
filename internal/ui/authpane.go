package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// In-pane auth prompts (issue #177): an auth question - unknown host key,
// password, passphrase, keyboard-interactive - is about one host, so it renders
// inside that host's pane, which the question also focuses. The Status panel
// rendering remains only as the fallback for a host whose pane is not visible
// (hidden by a filter or a split), and the status bar carries an AUTH segment
// either way - a question owning the keyboard must never be easy to miss.

// paneIndexFor finds the pane of the host an auth question names. Question
// messages carry the host alias; pane ids are the alias, or alias#n when the
// same alias was dialled twice, in which case the first such pane is where the
// question shows.
func (a App) paneIndexFor(host string) (int, bool) {
	if host == "" {
		return -1, false
	}
	ids := a.hostIDs()
	for i, id := range ids {
		if id == host {
			return i, true
		}
	}
	for i, id := range ids {
		if strings.HasPrefix(id, host+"#") {
			return i, true
		}
	}
	return -1, false
}

// focusQuestionPane routes an opening auth question: onto the host's pane when
// it is visible - focused, on its page - or into the Status panel when it is
// not. It returns the model with questionPaneID set to where the question
// renders, empty for the Status fallback.
func (a App) focusQuestionPane(host string) App {
	a.questionPaneID = ""
	if i, ok := a.paneIndexFor(host); ok {
		a.focus = AreaGrid
		a.paneIndex = i
		a.questionPaneID = a.hostIDs()[i]
		return a.followFocus()
	}
	// No pane to render in: the Status panel is where the question shows, so
	// the panel is selected for the user.
	a.panel = PanelStatus
	return a
}

// questionPaneVisible reports whether the open question renders in a pane. The
// pane is re-checked against the current host list: a host that left mid-
// question sends the question back to the Status panel rather than nowhere.
func (a App) questionPaneVisible() bool {
	if a.questionPaneID == "" {
		return false
	}
	for _, id := range a.hostIDs() {
		if id == a.questionPaneID {
			return true
		}
	}
	return false
}

// paneQuestionLines renders the open auth question for the pane it belongs to,
// wrapped to the pane's width; nil for every other pane.
func (a App) paneQuestionLines(id string, width int) []string {
	if width <= 0 || id == "" || a.questionPaneID != id {
		return nil
	}
	var raw []string
	switch {
	case a.keyQuestion != nil:
		raw = a.hostKeyQuestionLines()
	case a.secretQuestion != nil:
		raw = a.secretPromptLines()
	}
	if len(raw) == 0 {
		return nil
	}
	return strings.Split(ansi.Hardwrap(strings.Join(raw, "\n"), width, true), "\n")
}

// authStatusLabel is the status bar's AUTH segment while a question is open,
// empty otherwise. It replaces the TYPING/BROADCASTING segment: while a
// question owns the keyboard, no keystroke reaches a host.
func (a App) authStatusLabel() string {
	var host string
	switch {
	case a.keyQuestion != nil:
		host = a.keyQuestion.Host
	case a.secretQuestion != nil:
		host = a.secretQuestion.Host
	default:
		return ""
	}
	label := "AUTH"
	if host != "" {
		label += " " + host
	}
	if !a.questionPaneVisible() {
		label += " — see [1] Status"
	}
	return label
}
