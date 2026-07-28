package workingset

import (
	"strings"
	"testing"
)

func TestNewStartsWithEveryHost(t *testing.T) {
	m := New(fleet(40))
	if got := m.Count(); got != 40 {
		t.Fatalf("Count() = %d, want 40", got)
	}
	if got := m.Active().Kind(); got != KindAll {
		t.Fatalf("Active().Kind() = %v, want %v", got, KindAll)
	}
	if got := m.ActiveName(); got != "" {
		t.Fatalf("ActiveName() = %q, want empty", got)
	}
}

func TestNewCopiesHostList(t *testing.T) {
	hosts := fleet(3)
	m := New(hosts)
	hosts[0] = "mutated"
	if got := m.Members()[0]; got != "web-01" {
		t.Fatalf("manager followed a later mutation: %q", got)
	}
}

// The acceptance criterion for #34: with 40 hosts, going from the first 20 to
// the next 20 is one keystroke, and the last page does not wrap or overrun.
func TestNextPagesByChunkSize(t *testing.T) {
	m := New(fleet(40))
	if err := m.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}

	if got := m.Members(); got[0] != "web-01" || got[19] != "web-20" || len(got) != 20 {
		t.Fatalf("first page = %v", got)
	}

	if !m.Next() {
		t.Fatal("Next() did not move")
	}
	got := m.Members()
	if len(got) != 20 || got[0] != "web-21" || got[19] != "web-40" {
		t.Fatalf("second page = %v", got)
	}

	if m.Next() {
		t.Fatalf("Next() moved past the end of the host list: %v", m.Members())
	}
	if got := m.Members(); len(got) != 20 || got[0] != "web-21" {
		t.Fatalf("refused page changed the set: %v", got)
	}
}

func TestNextShortLastPage(t *testing.T) {
	m := New(fleet(25))
	if err := m.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	if !m.Next() {
		t.Fatal("Next() did not move")
	}
	if got := m.Members(); len(got) != 5 || got[0] != "web-21" {
		t.Fatalf("short last page = %v", got)
	}
	// The chunk size stays 20 even though only five hosts are left, so paging
	// back returns to exactly the first page.
	if !m.Prev() {
		t.Fatal("Prev() did not move")
	}
	if got := m.Members(); len(got) != 20 || got[0] != "web-01" {
		t.Fatalf("page back = %v", got)
	}
}

func TestPrevStopsAtTheStart(t *testing.T) {
	m := New(fleet(40))
	if err := m.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	if m.Prev() {
		t.Fatal("Prev() moved before the first host")
	}
}

func TestPrevClampsPartialChunkToTheStart(t *testing.T) {
	m := New(fleet(40))
	if err := m.Apply(Range{From: 6, To: 15}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !m.Prev() {
		t.Fatal("Prev() did not move")
	}
	if got := m.Members(); got[0] != "web-01" || len(got) != 10 {
		t.Fatalf("clamped page = %v", got)
	}
}

func TestPageOnlyAppliesToPositionalSets(t *testing.T) {
	tests := []struct {
		name string
		sel  Selector
	}{
		{"all", All{}},
		{"pattern", Pattern{Glob: "web-*"}},
		{"manual", NewManual([]string{"web-01"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(fleet(40))
			if err := m.Apply(tc.sel); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if m.Next() || m.Prev() {
				t.Fatal("paged a set that has no chunks")
			}
		})
	}
}

// The second acceptance criterion: a named set survives paging and can be
// returned to by name.
func TestNamedSetSurvivesPaging(t *testing.T) {
	m := New(fleet(40))
	if err := m.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	if err := m.Save("front-half"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := m.ActiveName(); got != "front-half" {
		t.Fatalf("ActiveName() = %q, want front-half", got)
	}

	if !m.Next() {
		t.Fatal("Next() did not move")
	}
	if got := m.ActiveName(); got != "" {
		t.Fatalf("paging kept the name %q; a paged set is ad hoc", got)
	}

	if err := m.Activate("front-half"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if got := m.Members(); len(got) != 20 || got[0] != "web-01" {
		t.Fatalf("named set did not survive paging: %v", got)
	}
}

func TestDefineRedefinesAndUpdatesTheActiveSet(t *testing.T) {
	m := New(fleet(40))
	if err := m.Define("front", Range{From: 1, To: 10}); err != nil {
		t.Fatalf("Define: %v", err)
	}
	if err := m.Activate("front"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if err := m.Define("front", Range{From: 1, To: 5}); err != nil {
		t.Fatalf("redefine: %v", err)
	}
	if got := m.Count(); got != 5 {
		t.Fatalf("Count() = %d after redefining the active set, want 5", got)
	}
	if got := len(m.Named()); got != 1 {
		t.Fatalf("Named() = %d entries, want 1", got)
	}
}

func TestDefineRejectsEmptyNameAndNilSelector(t *testing.T) {
	m := New(fleet(3))
	if err := m.Define("  ", Range{From: 1, To: 2}); err == nil {
		t.Fatal("Define accepted an empty name")
	}
	if err := m.Define("x", nil); err == nil {
		t.Fatal("Define accepted a nil selector")
	}
	if err := m.Apply(nil); err == nil {
		t.Fatal("Apply accepted a nil selector")
	}
}

func TestActivateUnknownName(t *testing.T) {
	m := New(fleet(3))
	err := m.Activate("nope")
	if err == nil {
		t.Fatal("Activate accepted an unknown name")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("error %q does not name the set", err)
	}
}

func TestRemove(t *testing.T) {
	m := New(fleet(10))
	if err := m.Define("front", Range{From: 1, To: 5}); err != nil {
		t.Fatalf("Define: %v", err)
	}
	if err := m.Activate("front"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if err := m.Remove("front"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok := m.Lookup("front"); ok {
		t.Fatal("Lookup found a removed set")
	}
	// Removing the definition must not move the user somewhere else.
	if got := m.Count(); got != 5 {
		t.Fatalf("Count() = %d after removing the active definition, want 5", got)
	}
	if got := m.ActiveName(); got != "" {
		t.Fatalf("ActiveName() = %q after removal, want empty", got)
	}
	if err := m.Remove("front"); err == nil {
		t.Fatal("Remove accepted an unknown name")
	}
}

func TestSetHostsKeepsDefinitions(t *testing.T) {
	m := New(fleet(40))
	if err := m.Apply(Pattern{Glob: "web-1*"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := m.Count(); got != 10 {
		t.Fatalf("Count() = %d, want 10", got)
	}

	m.SetHosts(fleet(5))
	if got := m.Count(); got != 0 {
		t.Fatalf("Count() = %d after the fleet shrank, want 0", got)
	}
	if got := m.Total(); got != 5 {
		t.Fatalf("Total() = %d, want 5", got)
	}
}

func TestContains(t *testing.T) {
	m := New(fleet(40))
	if !m.Contains("web-01") {
		t.Fatal("Contains() missed a host while every host is active")
	}
	if m.Contains("web-99") {
		t.Fatal("Contains() found a host that is not in the run")
	}

	if err := m.Apply(Range{From: 21, To: 40}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if m.Contains("web-01") {
		t.Fatal("Contains() reported a host outside the working set")
	}
	if !m.Contains("web-21") {
		t.Fatal("Contains() missed a host inside the working set")
	}
}

func TestReset(t *testing.T) {
	m := New(fleet(40))
	if err := m.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	if err := m.Save("front"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	m.Reset()
	if got := m.Count(); got != 40 {
		t.Fatalf("Count() = %d after Reset, want 40", got)
	}
	if got := m.ActiveName(); got != "" {
		t.Fatalf("ActiveName() = %q after Reset, want empty", got)
	}
}

func TestApplySpecReportsParseErrors(t *testing.T) {
	m := New(fleet(3))
	if err := m.ApplySpec("40-21", nil); err == nil {
		t.Fatal("ApplySpec accepted a reversed range")
	}
	if got := m.Count(); got != 3 {
		t.Fatalf("a rejected spec changed the working set: %d hosts", got)
	}
}

// Describe is what the status bar shows; it must never be readable as a bare
// count that hides how many hosts are outside the set.
func TestDescribe(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Manager)
		want  string
	}{
		{"all", func(*Manager) {}, "all (40/40 hosts)"},
		{"range", func(m *Manager) { _ = m.Apply(Range{From: 1, To: 20}) }, "first-20 (20/40 hosts)"},
		{"offset range", func(m *Manager) { _ = m.Apply(Range{From: 21, To: 40}) }, "21-40 (20/40 hosts)"},
		{"pattern", func(m *Manager) { _ = m.Apply(Pattern{Glob: "web-1*"}) }, "web-1* (10/40 hosts)"},
		{"named", func(m *Manager) {
			_ = m.Apply(Range{From: 1, To: 20})
			_ = m.Save("front-half")
		}, "front-half (20/40 hosts)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := New(fleet(40))
			tc.setup(m)
			if got := m.Describe(); got != tc.want {
				t.Fatalf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSaveRejectsEmptyName(t *testing.T) {
	m := New(fleet(3))
	if err := m.Save(""); err == nil {
		t.Fatal("Save accepted an empty name")
	}
}

func TestHostsReturnsACopy(t *testing.T) {
	m := New(fleet(2))
	got := m.Hosts()
	got[0] = "mutated"
	if m.Members()[0] != "web-01" {
		t.Fatal("Hosts() aliased the manager's host list")
	}
}
