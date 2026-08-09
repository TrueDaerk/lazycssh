package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// metaPrefix marks a command line entry as an instruction to lazycssh rather
// than a command for the hosts.
//
// It is a prefix rather than a bare word on purpose: `select` is a shell
// builtin, and a line that means "run this on forty machines" must never be
// intercepted because it happens to start with a word this program knows.
const metaPrefix = "/"

// Selector is what the selection commands need from the broadcast router.
// [broadcast.Router] satisfies it.
type Selector interface {
	// SelectAll selects every host in the run.
	SelectAll() int
	// SelectWorkingSet selects the hosts in the active working set.
	SelectWorkingSet()
	// InvertSelection flips the selection across the run.
	InvertSelection() int
	// ClearSelection empties it.
	ClearSelection()
	// SelectMatching and DeselectMatching act on a glob.
	SelectMatching(glob string) (int, error)
	DeselectMatching(glob string) (int, error)
	// SelectConnected and SelectDisconnected act on liveness.
	SelectConnected() int
	SelectDisconnected() int
	// SelectionCount is how many hosts are toggled.
	SelectionCount() int
}

// selector returns the router as a [Selector], and whether the run has one.
func (a App) selector() (Selector, bool) {
	if a.cfg.Targets == nil {
		return nil, false
	}
	s, ok := a.cfg.Targets.(Selector)
	return s, ok
}

// applySelection runs one selection instruction and returns what to report.
//
// The instructions mirror the keys in the Hosts panel, so a user can reach the
// same selection by hand or by pattern:
//
//	/select all          every host in the run
//	/select set          the active working set
//	/select up           the hosts that can take input
//	/select down         the hosts that cannot
//	/select invert       flip the selection
//	/select none         clear it
//	/select web-*        every host matching a glob
//	/deselect web-1*     the same, in reverse
func (a App) applySelection(line string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), metaPrefix)
	if !ok {
		return "", false
	}

	verb, arg, _ := strings.Cut(strings.TrimSpace(rest), " ")
	arg = strings.TrimSpace(arg)

	sel, available := a.selector()
	if !available {
		return "no host selection yet", true
	}

	switch strings.ToLower(verb) {
	case "select":
		return a.runSelect(sel, arg), true
	case "deselect":
		if arg == "" {
			return "deselect needs a pattern", true
		}
		n, err := sel.DeselectMatching(arg)
		if err != nil {
			return err.Error(), true
		}
		return fmt.Sprintf("deselected %d matching %s (%d selected)",
			n, arg, sel.SelectionCount()), true
	default:
		return fmt.Sprintf("unknown command %q", metaPrefix+verb), true
	}
}

// runSelect applies one `/select` argument.
func (a App) runSelect(sel Selector, arg string) string {
	switch strings.ToLower(arg) {
	case "", "all":
		return fmt.Sprintf("selected every host (%d selected)", sel.SelectAll())
	case "set", "working-set":
		sel.SelectWorkingSet()
		return fmt.Sprintf("selected the working set (%d selected)", sel.SelectionCount())
	case "up", "connected":
		return fmt.Sprintf("selected the hosts that are up (%d selected)", sel.SelectConnected())
	case "down", "failed":
		return fmt.Sprintf("selected the hosts that are down (%d selected)", sel.SelectDisconnected())
	case "invert":
		return fmt.Sprintf("inverted the selection (%d selected)", sel.InvertSelection())
	case "none", "clear":
		sel.ClearSelection()
		return "cleared the selection"
	default:
		n, err := sel.SelectMatching(arg)
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("selected %d matching %s (%d selected)", n, arg, sel.SelectionCount())
	}
}

// handleSelectionKey applies the Hosts panel selection keys, and reports whether
// it handled the press.
//
// These are the same operations the command line spells out; the keys exist
// because building a selection by hand is the common case and typing a glob is
// the precise one.
func (a App) handleSelectionKey(msg tea.KeyPressMsg) (App, bool) {
	bound := key.Matches(msg, a.keys.SelectAll, a.keys.Invert, a.keys.ClearSel,
		a.keys.SelectUp, a.keys.SelectDwn)

	sel, ok := a.selector()
	if !ok {
		// Without a router there is nothing to select, but the keys are still
		// consumed rather than falling through to something unrelated.
		return a, bound
	}

	switch {
	case key.Matches(msg, a.keys.SelectAll):
		a.lastDelivery = fmt.Sprintf("selected every host (%d selected)", sel.SelectAll())
	case key.Matches(msg, a.keys.Invert):
		a.lastDelivery = fmt.Sprintf("inverted the selection (%d selected)", sel.InvertSelection())
	case key.Matches(msg, a.keys.ClearSel):
		sel.ClearSelection()
		a.lastDelivery = "cleared the selection"
	case key.Matches(msg, a.keys.SelectUp):
		a.lastDelivery = fmt.Sprintf("selected the hosts that are up (%d selected)", sel.SelectConnected())
	case key.Matches(msg, a.keys.SelectDwn):
		a.lastDelivery = fmt.Sprintf("selected the hosts that are down (%d selected)", sel.SelectDisconnected())
	default:
		return a, false
	}
	return a, true
}
