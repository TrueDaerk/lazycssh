// Package broadcast decides where a keystroke goes.
//
// The rule this package exists to enforce: **broadcast mode `all` means the
// active working set, not every host in the run**. Reaching every host is a
// separate mode with its own, deliberately harder binding, and it renders as a
// warning. See wiki/core/broadcast-scope.md for why.
package broadcast

import (
	"errors"
	"fmt"
	"io"
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

// Sessions is the live transport as the router sees it: which hosts can take a
// byte right now, and where to write it.
//
// It is an interface so this package still needs no network and no UI to be
// tested. [ssh.Manager] satisfies it.
type Sessions interface {
	// Connected reports whether a host's session can take input right now. A
	// host that is dialling, failed or closed cannot.
	Connected(id string) bool
	// Writer returns the host's stdin, and whether there is one.
	Writer(id string) (io.Writer, bool)
}

// Router resolves the current mode, working set, selection and focus into the
// list of hosts a keystroke reaches, and - once a transport is attached -
// delivers to exactly those hosts.
//
// The scope and the targets are deliberately two different questions. The scope
// is who the user is addressing; the targets are who can actually receive,
// which is the scope minus the hosts that are down. The status bar shows both,
// because "sent to 7" and "meant 8" is exactly the difference a user needs to
// notice.
//
// The zero value is not usable; construct one with [NewRouter].
type Router struct {
	ws       *workingset.Manager
	sessions Sessions

	mode     Mode
	selected map[string]struct{}
	focus    string
}

// Attach connects the router to the live transport. Until it is attached the
// router answers about scope only, which is what the tests and a run that has
// not dialled yet need.
func (r *Router) Attach(s Sessions) { r.sessions = s }

// Attached reports whether a transport is attached.
func (r *Router) Attached() bool { return r.sessions != nil }

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

// SelectWhere selects every host in the run matching a predicate and reports
// how many are selected as a result.
//
// The predicate is supplied by the caller because the router knows liveness but
// not state: "every failed host" is a question about the transport, and pushing
// it in here would drag the whole session model into this package.
func (r *Router) SelectWhere(match func(id string) bool) int {
	if match == nil {
		return len(r.selected)
	}
	for _, id := range r.ws.Hosts() {
		if match(id) {
			r.selected[id] = struct{}{}
		}
	}
	return len(r.selected)
}

// SelectMatching selects the hosts matching a glob and reports how many matched.
//
// The pattern is matched against every host in the run, not only the working
// set: a selection is a statement about machines, and silently skipping the
// ones outside the current window would make `web-*` mean different things at
// different times.
func (r *Router) SelectMatching(glob string) (int, error) {
	pattern, err := workingset.NewPattern(glob)
	if err != nil {
		return 0, err
	}

	matched := pattern.Select(r.ws.Hosts())
	r.Select(matched...)
	return len(matched), nil
}

// DeselectMatching removes the hosts matching a glob and reports how many
// matched.
func (r *Router) DeselectMatching(glob string) (int, error) {
	pattern, err := workingset.NewPattern(glob)
	if err != nil {
		return 0, err
	}

	matched := pattern.Select(r.ws.Hosts())
	r.Deselect(matched...)
	return len(matched), nil
}

// InvertSelection selects everything that was not selected, and vice versa,
// across the whole run.
func (r *Router) InvertSelection() int {
	inverted := make(map[string]struct{})
	for _, id := range r.ws.Hosts() {
		if _, ok := r.selected[id]; !ok {
			inverted[id] = struct{}{}
		}
	}
	r.selected = inverted
	return len(r.selected)
}

// SelectAll selects every host in the run.
func (r *Router) SelectAll() int {
	for _, id := range r.ws.Hosts() {
		r.selected[id] = struct{}{}
	}
	return len(r.selected)
}

// SelectConnected selects the hosts that can take input right now. Without a
// transport nothing is known about liveness, so nothing is selected rather than
// everything.
func (r *Router) SelectConnected() int {
	if r.sessions == nil {
		return len(r.selected)
	}
	return r.SelectWhere(r.sessions.Connected)
}

// SelectDisconnected selects the hosts that cannot take input right now, which
// is how "show me what broke" is built.
func (r *Router) SelectDisconnected() int {
	if r.sessions == nil {
		return len(r.selected)
	}
	return r.SelectWhere(func(id string) bool { return !r.sessions.Connected(id) })
}

// SelectionCount is how many hosts are toggled.
func (r *Router) SelectionCount() int { return len(r.selected) }

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

// Targets returns the hosts a keystroke actually reaches right now: the scope
// minus every host whose session cannot take input.
//
// Without a transport every host in scope counts as a target, because there is
// nothing yet that could say otherwise.
func (r *Router) Targets() []string {
	scope := r.Scope()
	if r.sessions == nil {
		return scope
	}

	var out []string
	for _, id := range scope {
		if r.sessions.Connected(id) {
			out = append(out, id)
		}
	}
	return out
}

// Unreachable returns the hosts in scope that will not receive input, because
// they are dialling, failed or closed.
func (r *Router) Unreachable() []string {
	if r.sessions == nil {
		return nil
	}

	var out []string
	for _, id := range r.Scope() {
		if !r.sessions.Connected(id) {
			out = append(out, id)
		}
	}
	return out
}

// Scope returns the hosts the user is addressing, whether or not they are up.
func (r *Router) Scope() []string {
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

// ScopeCount is how many hosts the user is addressing, up or not.
func (r *Router) ScopeCount() int { return len(r.Scope()) }

// Send writes to every target and reports what happened.
//
// One host that refuses a write does not stop the others: a broken pipe on one
// machine is one dead pane, never a command that half the fleet missed without
// anyone saying so. The report says how many actually received it.
func (r *Router) Send(p []byte) (Delivery, error) {
	targets := r.Targets()
	d := Delivery{Mode: r.mode, Scope: r.ScopeCount(), Targets: len(targets)}

	if r.sessions == nil {
		return d, fmt.Errorf("broadcast: no transport attached")
	}

	for _, id := range targets {
		w, ok := r.sessions.Writer(id)
		if !ok {
			d.Failed = append(d.Failed, id)
			continue
		}
		if _, err := w.Write(p); err != nil {
			d.Failed = append(d.Failed, id)
			d.Errs = append(d.Errs, fmt.Errorf("write to %s: %w", id, err))
			continue
		}
		d.Delivered++
	}
	return d, errors.Join(d.Errs...)
}

// Delivery is what one [Router.Send] did.
type Delivery struct {
	// Mode is the broadcast mode it went out in.
	Mode Mode
	// Scope is how many hosts the user was addressing.
	Scope int
	// Targets is how many of those could take input.
	Targets int
	// Delivered is how many actually took it.
	Delivered int
	// Failed lists the hosts that did not, in host order.
	Failed []string
	// Errs are the write errors behind Failed.
	Errs []error
}

// String renders the report the UI shows after a send. It always says how many
// hosts were meant, not only how many were reached: a command that quietly went
// to seven of forty machines is the failure this tool exists to make visible.
func (d Delivery) String() string {
	s := fmt.Sprintf("sent to %d/%d host", d.Delivered, d.Scope)
	if d.Scope != 1 {
		s += "s"
	}
	if missed := d.Scope - d.Delivered; missed > 0 {
		s += fmt.Sprintf(" (%d did not receive it)", missed)
	}
	return s
}

// Total is the number of hosts in the run.
func (r *Router) Total() int { return r.ws.Total() }

// Describe renders the status bar label.
//
// With a transport it reports what will actually happen - how many hosts of the
// scope can take the next byte:
//
//	BROADCAST all (7/8 up)
//	BROADCAST set:front-half (19/20 up)
//	BROADCAST single web-01 (1/1 up)
//	BROADCAST EVERY HOST (38/40 up)
//
// Without one there is nothing that could say a host is down, so it reports the
// scope alone rather than claiming everything is up:
//
//	BROADCAST all (8 hosts)
func (r *Router) Describe() string {
	if r.sessions == nil {
		scope := r.ScopeCount()
		unit := "hosts"
		if scope == 1 {
			unit = "host"
		}
		return fmt.Sprintf("BROADCAST %s (%d %s)", r.label(), scope, unit)
	}
	return fmt.Sprintf("BROADCAST %s (%d/%d up)", r.label(), r.Count(), r.ScopeCount())
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
