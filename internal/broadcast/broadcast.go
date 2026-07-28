// Package broadcast decides where a keystroke goes.
//
// The rule this package exists to enforce: **broadcast mode `all` means the
// active working set, not every host in the run**. Reaching every host is a
// separate mode with its own, deliberately harder binding, and it renders as a
// warning. See wiki/core/broadcast-scope.md for why.
package broadcast

import (
	"fmt"
	"strconv"

	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// Mode is the scope a keystroke is sent to.
type Mode int

const (
	// ModeAll sends to every host in the active working set. It is the default
	// and it is bounded by the working set, never by the fleet.
	ModeAll Mode = iota
	// ModeSelected sends to the hosts toggled by the user, intersected with the
	// working set.
	ModeSelected
	// ModeSingle sends only to the focused pane, which is what an interactive
	// prompt - a sudo password, vim - needs.
	ModeSingle
	// ModeFleet sends to every host in the run, ignoring the working set. It is
	// the "really every host" escape hatch and is always rendered as a warning.
	ModeFleet
)

// String returns the lowercase name used in labels and errors.
func (m Mode) String() string {
	switch m {
	case ModeAll:
		return "all"
	case ModeSelected:
		return "selected"
	case ModeSingle:
		return "single"
	case ModeFleet:
		return "fleet"
	default:
		return "unknown(" + strconv.Itoa(int(m)) + ")"
	}
}

// Valid reports whether the mode is one this package knows.
func (m Mode) Valid() bool { return m >= ModeAll && m <= ModeFleet }

// ParseMode reads a mode name.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "all":
		return ModeAll, nil
	case "selected":
		return ModeSelected, nil
	case "single":
		return ModeSingle, nil
	case "fleet":
		return ModeFleet, nil
	default:
		return 0, fmt.Errorf("broadcast: unknown mode %q", s)
	}
}

// Router resolves the current mode, working set, selection and focus into the
// list of hosts a keystroke reaches.
//
// It holds no sessions and sends nothing: it answers "who receives this", and
// the caller does the writing. That keeps the rule testable without a network
// and without a UI.
//
// The zero value is not usable; construct one with [NewRouter].
type Router struct {
	ws *workingset.Manager

	mode     Mode
	selected map[string]struct{}
	focus    string
}

// NewRouter builds a router over a working set, starting in [ModeAll].
func NewRouter(ws *workingset.Manager) (*Router, error) {
	if ws == nil {
		return nil, fmt.Errorf("broadcast: nil working set")
	}
	return &Router{ws: ws, mode: ModeAll, selected: make(map[string]struct{})}, nil
}

// Mode is the current broadcast mode.
func (r *Router) Mode() Mode { return r.mode }

// SetMode changes the mode.
//
// Switching into [ModeFleet] is not blocked here - the confirmation belongs to
// the key binding, not to the router - but it is a distinct mode precisely so
// the UI can treat it differently from every other one.
func (r *Router) SetMode(m Mode) error {
	if !m.Valid() {
		return fmt.Errorf("broadcast: invalid mode %d", int(m))
	}
	r.mode = m
	return nil
}

// Toggle flips a host's selection and reports whether it is now selected.
//
// A host outside the working set may be toggled: the working set can change
// under a selection, and silently refusing a keystroke is worse than showing
// the host as selected-but-not-a-target. [Router.Excluded] reports those hosts
// so the UI can mark them.
func (r *Router) Toggle(id string) bool {
	if _, ok := r.selected[id]; ok {
		delete(r.selected, id)
		return false
	}
	r.selected[id] = struct{}{}
	return true
}

// Select adds hosts to the selection.
func (r *Router) Select(ids ...string) {
	for _, id := range ids {
		r.selected[id] = struct{}{}
	}
}

// Deselect removes hosts from the selection.
func (r *Router) Deselect(ids ...string) {
	for _, id := range ids {
		delete(r.selected, id)
	}
}

// SelectWorkingSet selects every host in the working set, which is how "select
// what I am looking at" is built.
func (r *Router) SelectWorkingSet() {
	for _, id := range r.ws.Members() {
		r.selected[id] = struct{}{}
	}
}

// ClearSelection empties the selection.
func (r *Router) ClearSelection() { r.selected = make(map[string]struct{}) }

// IsSelected reports whether a host is toggled, regardless of whether it is a
// current target.
func (r *Router) IsSelected(id string) bool {
	_, ok := r.selected[id]
	return ok
}

// Selected returns the toggled hosts in host order.
func (r *Router) Selected() []string {
	return inOrder(r.ws.Hosts(), r.selected)
}

// Excluded returns the hosts that are selected but outside the working set, and
// therefore will not receive input in [ModeSelected].
func (r *Router) Excluded() []string {
	var out []string
	for _, id := range r.Selected() {
		if !r.ws.Contains(id) {
			out = append(out, id)
		}
	}
	return out
}

// SetFocus records the focused pane, the target of [ModeSingle].
func (r *Router) SetFocus(id string) { r.focus = id }

// Focus is the focused host identifier.
func (r *Router) Focus() string { return r.focus }

// Targets returns the hosts a keystroke reaches right now, in host order.
func (r *Router) Targets() []string {
	switch r.mode {
	case ModeFleet:
		return r.ws.Hosts()
	case ModeSelected:
		var out []string
		for _, id := range r.Selected() {
			if r.ws.Contains(id) {
				out = append(out, id)
			}
		}
		return out
	case ModeSingle:
		// Single mode deliberately ignores the working set: it is one host, it
		// is the pane the user is looking at, and it is how a password prompt
		// is answered without touching anything else.
		if r.focus == "" {
			return nil
		}
		for _, id := range r.ws.Hosts() {
			if id == r.focus {
				return []string{id}
			}
		}
		return nil
	default:
		return r.ws.Members()
	}
}

// Count is how many hosts a keystroke reaches.
func (r *Router) Count() int { return len(r.Targets()) }

// Total is the number of hosts in the run.
func (r *Router) Total() int { return r.ws.Total() }

// Describe renders the status bar label. The target count and the fleet total
// are always shown together, so the line can never be read as "all forty" while
// only twenty hosts will receive input.
//
//	BROADCAST all (40/40 hosts)
//	BROADCAST set:front-half (20/40 hosts)
//	BROADCAST selected (3/40 hosts)
//	BROADCAST single web-01 (1/40 hosts)
//	BROADCAST EVERY HOST (40/40 hosts)
func (r *Router) Describe() string {
	return fmt.Sprintf("BROADCAST %s (%d/%d hosts)", r.label(), r.Count(), r.Total())
}

// label names the scope without the counts.
func (r *Router) label() string {
	switch r.mode {
	case ModeFleet:
		return "EVERY HOST"
	case ModeSelected:
		return "selected"
	case ModeSingle:
		if r.focus == "" {
			return "single (no pane)"
		}
		return "single " + r.focus
	default:
		if r.ws.Active().Kind() == workingset.KindAll {
			return "all"
		}
		if name := r.ws.ActiveName(); name != "" {
			return "set:" + name
		}
		return "set:" + r.ws.Active().String()
	}
}

// Warning reports whether the current scope weakens the working set's
// protection, which the status bar renders in the warning style.
func (r *Router) Warning() bool { return r.mode == ModeFleet }

// inOrder returns the members of set in the order they appear in all.
func inOrder(all []string, set map[string]struct{}) []string {
	var out []string
	for _, id := range all {
		if _, ok := set[id]; ok {
			out = append(out, id)
		}
	}
	return out
}
