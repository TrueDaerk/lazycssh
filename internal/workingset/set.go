package workingset

import (
	"fmt"
	"strings"
)

// Set is a named working set definition.
type Set struct {
	// Name is what the user types to return to it.
	Name string
	// Selector is the definition, re-evaluated against the live host list.
	Selector Selector
}

// Manager owns the host list, the named sets and which set is active.
//
// It holds definitions, never snapshots: [Manager.Members] is computed from the
// current host list on every call, so a set cannot drift out of date after a
// reconnect renames nothing and after hosts are added.
//
// The zero value is not usable; construct one with [New].
type Manager struct {
	hosts []string

	active     Selector
	activeName string

	named []Set
}

// New builds a manager over an ordered host list, with every host active.
func New(hosts []string) *Manager {
	return &Manager{
		hosts:  append([]string(nil), hosts...),
		active: All{},
	}
}

// SetHosts replaces the host list. Definitions are kept: a range still means the
// same positions, a pattern is re-matched, and a manual set silently loses the
// identifiers that no longer exist.
func (m *Manager) SetHosts(hosts []string) {
	m.hosts = append([]string(nil), hosts...)
}

// Hosts returns every host, in order.
func (m *Manager) Hosts() []string { return append([]string(nil), m.hosts...) }

// Total is the number of hosts in the run, which is the denominator the status
// bar shows next to the working set size.
func (m *Manager) Total() int { return len(m.hosts) }

// Active is the selector currently in force.
func (m *Manager) Active() Selector { return m.active }

// ActiveName is the name of the active set, or the empty string when the active
// selector is ad hoc - typed once, or produced by paging.
func (m *Manager) ActiveName() string { return m.activeName }

// Members returns the hosts in the working set, in host order.
func (m *Manager) Members() []string { return m.active.Select(m.hosts) }

// Count is the number of hosts in the working set.
func (m *Manager) Count() int { return len(m.Members()) }

// Contains reports whether a host is part of the working set. It is the question
// the broadcast router asks for every keystroke, so it avoids allocating the
// member list for the common all-hosts case.
func (m *Manager) Contains(id string) bool {
	if m.active.Kind() == KindAll {
		for _, h := range m.hosts {
			if h == id {
				return true
			}
		}
		return false
	}
	for _, member := range m.Members() {
		if member == id {
			return true
		}
	}
	return false
}

// Apply makes a selector active without naming it.
func (m *Manager) Apply(sel Selector) error {
	if sel == nil {
		return fmt.Errorf("working set: nil selector")
	}
	m.active = sel
	m.activeName = ""
	return nil
}

// ApplySpec parses a user-typed definition and applies it. selection is the
// currently toggled host set, used by the "selection" spec.
func (m *Manager) ApplySpec(spec string, selection []string) error {
	sel, err := ParseSelector(spec, selection)
	if err != nil {
		return err
	}
	return m.Apply(sel)
}

// Reset makes every host the working set again.
func (m *Manager) Reset() {
	m.active = All{}
	m.activeName = ""
}

// Define stores a named set. An existing name is redefined.
func (m *Manager) Define(name string, sel Selector) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("working set: empty name")
	}
	if sel == nil {
		return fmt.Errorf("working set %q: nil selector", name)
	}
	for i, s := range m.named {
		if s.Name == name {
			m.named[i].Selector = sel
			if m.activeName == name {
				m.active = sel
			}
			return nil
		}
	}
	m.named = append(m.named, Set{Name: name, Selector: sel})
	return nil
}

// Save names the currently active selector and activates it under that name, so
// paging away and coming back is possible.
func (m *Manager) Save(name string) error {
	if err := m.Define(name, m.active); err != nil {
		return err
	}
	m.activeName = strings.TrimSpace(name)
	return nil
}

// Activate makes a named set the working set.
func (m *Manager) Activate(name string) error {
	for _, s := range m.named {
		if s.Name == name {
			m.active = s.Selector
			m.activeName = s.Name
			return nil
		}
	}
	return fmt.Errorf("working set %q: not defined", name)
}

// Remove deletes a named set. The active selector is untouched, so removing the
// definition of the set you are working in does not move you somewhere else.
func (m *Manager) Remove(name string) error {
	for i, s := range m.named {
		if s.Name == name {
			m.named = append(m.named[:i], m.named[i+1:]...)
			if m.activeName == name {
				m.activeName = ""
			}
			return nil
		}
	}
	return fmt.Errorf("working set %q: not defined", name)
}

// Named returns the named sets in definition order.
func (m *Manager) Named() []Set { return append([]Set(nil), m.named...) }

// Lookup returns a named set.
func (m *Manager) Lookup(name string) (Set, bool) {
	for _, s := range m.named {
		if s.Name == name {
			return s, true
		}
	}
	return Set{}, false
}

// Next advances to the following chunk of the same size - "now the next 20" -
// and reports whether it moved. It only applies to a positional set; a pattern
// or a manual selection has no next chunk.
//
// Paging produces an ad hoc set: the named definition it started from is left
// intact, so activating that name again returns to the original positions.
func (m *Manager) Next() bool { return m.page(+1) }

// Prev goes back one chunk of the same size and reports whether it moved.
func (m *Manager) Prev() bool { return m.page(-1) }

// page shifts a positional working set by one chunk in the given direction.
func (m *Manager) page(direction int) bool {
	r, ok := m.active.(Range)
	if !ok {
		return false
	}
	size := r.Size()
	if size <= 0 {
		return false
	}

	from := r.From + direction*size
	if from < 1 {
		if r.From == 1 {
			return false
		}
		from = 1
	}
	if direction > 0 && from > len(m.hosts) {
		return false
	}

	m.active = Range{From: from, To: from + size - 1}
	m.activeName = ""
	return true
}

// Describe renders the working set for the status bar: the name when it has
// one, the definition otherwise, always with the size against the fleet total.
// The two numbers are never separated, because "20" alone reads as "all twenty
// hosts" to a user whose run has forty.
func (m *Manager) Describe() string {
	label := m.active.String()
	if m.activeName != "" {
		label = m.activeName
	}
	return fmt.Sprintf("%s (%d/%d hosts)", label, m.Count(), len(m.hosts))
}
