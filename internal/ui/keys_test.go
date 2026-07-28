package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

// The acceptance criterion: adding a binding without a help string fails.
func TestEveryBindingHasHelp(t *testing.T) {
	for name, b := range bindingFields(DefaultKeyMap()) {
		if len(b.Keys()) == 0 {
			t.Errorf("%s is bound to no key", describe(name, b))
		}
		if b.Help().Key == "" || b.Help().Desc == "" {
			t.Errorf("%s has no help string; the overlay is generated from these", describe(name, b))
		}
	}
}

// The other acceptance criterion, from the other side: every binding that
// exists is shown somewhere, so the overlay lists exactly what is handled.
func TestEveryBindingAppearsInTheHelp(t *testing.T) {
	k := DefaultKeyMap()

	shown := make(map[string]bool)
	for _, area := range Areas() {
		for _, b := range k.Bindings(area) {
			shown[strings.Join(b.Keys(), ",")] = true
		}
	}

	for name, b := range bindingFields(k) {
		if !shown[strings.Join(b.Keys(), ",")] {
			t.Errorf("%s is declared but appears in no help group", describe(name, b))
		}
	}
}

func TestAllReturnsEveryBinding(t *testing.T) {
	k := DefaultKeyMap()
	if got, want := len(k.All()), len(bindingFields(k)); got != want {
		t.Fatalf("All() returned %d bindings, the keymap declares %d", got, want)
	}
}

// No key binding is ambiguous between two things that are handled at the same
// time: an area's own bindings plus the global ones.
func TestNoAmbiguousBindingsWithinAnArea(t *testing.T) {
	k := DefaultKeyMap()

	for _, area := range []Area{AreaSidebar, AreaGrid} {
		t.Run(area.String(), func(t *testing.T) {
			seen := make(map[string]string)
			active := append(k.Bindings(area), k.Bindings(AreaGlobal)...)

			for _, b := range active {
				for _, pressed := range b.Keys() {
					if other, taken := seen[pressed]; taken {
						t.Errorf("key %q means both %q and %q while the %s has focus",
							pressed, other, b.Help().Desc, area)
						continue
					}
					seen[pressed] = b.Help().Desc
				}
			}
		})
	}
}

// The sidebar and the grid may reuse keys - they are never focused at once -
// but the global set must not collide with either.
func TestGlobalBindingsDoNotCollideWithAnyArea(t *testing.T) {
	k := DefaultKeyMap()

	globals := make(map[string]key.Binding)
	for _, b := range k.Bindings(AreaGlobal) {
		for _, pressed := range b.Keys() {
			globals[pressed] = b
		}
	}

	for _, area := range []Area{AreaSidebar, AreaGrid} {
		for _, b := range k.Bindings(area) {
			for _, pressed := range b.Keys() {
				if clash, taken := globals[pressed]; taken {
					t.Errorf("%s: key %q is both %q and the global %q",
						area, pressed, b.Help().Desc, clash.Help().Desc)
				}
			}
		}
	}
}

// Help is context sensitive: the focused area comes first, and its bindings are
// the ones listed first.
func TestFullHelpLeadsWithTheFocusedArea(t *testing.T) {
	k := DefaultKeyMap()

	for _, area := range Areas() {
		h, ok := k.For(area).(contextHelp)
		if !ok {
			t.Fatalf("For(%v) did not return a contextHelp", area)
		}

		groups := h.FullHelp()
		if len(groups) != len(Areas()) {
			t.Fatalf("%v: FullHelp() returned %d groups, want %d", area, len(groups), len(Areas()))
		}
		want := k.Bindings(area)
		if len(groups[0]) != len(want) {
			t.Fatalf("%v: the first group holds %d bindings, want the area's %d",
				area, len(groups[0]), len(want))
		}
		if titles := h.Titles(); len(titles) != len(groups) || titles[0] != area.String() {
			t.Fatalf("%v: Titles() = %v", area, titles)
		}
	}
}

func TestShortHelpIsNotEmptyAnywhere(t *testing.T) {
	k := DefaultKeyMap()
	for _, area := range Areas() {
		short := k.For(area).ShortHelp()
		if len(short) == 0 {
			t.Fatalf("%v: the short help line is empty", area)
		}
		for _, b := range short {
			if b.Help().Key == "" {
				t.Fatalf("%v: a short help entry has no key label", area)
			}
		}
	}
}

// The rendered overlay is generated from the same declarations, so a binding
// cannot be documented without existing.
func TestRenderedHelpContainsTheAreaBindings(t *testing.T) {
	k := DefaultKeyMap()
	model := help.New()
	model.Styles = HelpStyles(NewTheme(Options{Dark: true}))
	model.SetWidth(200)

	rendered := model.FullHelpView(k.For(AreaGrid).FullHelp())
	for _, b := range k.Bindings(AreaGrid) {
		if !strings.Contains(rendered, b.Help().Desc) {
			t.Fatalf("the overlay does not mention %q:\n%s", b.Help().Desc, rendered)
		}
	}

	short := model.ShortHelpView(k.For(AreaGrid).ShortHelp())
	if !strings.Contains(short, "reconnect this host") {
		t.Fatalf("the short help line is missing a binding it declares:\n%s", short)
	}
}

// The mode that ignores the working set is deliberately not a single letter,
// and it cannot be reached by cycling through the others.
func TestFleetBroadcastIsHardToPressByAccident(t *testing.T) {
	k := DefaultKeyMap()
	for _, pressed := range k.BroadcastFleet.Keys() {
		if len([]rune(pressed)) == 1 {
			t.Fatalf("the fleet broadcast mode is bound to the single key %q", pressed)
		}
	}
	if !strings.Contains(strings.ToUpper(k.BroadcastFleet.Help().Desc), "EVERY") {
		t.Fatalf("the fleet binding does not say what it does: %q", k.BroadcastFleet.Help().Desc)
	}
}

func TestAreaString(t *testing.T) {
	tests := map[Area]string{
		AreaGlobal:  "global",
		AreaSidebar: "sidebar",
		AreaGrid:    "panes",
		Area(42):    "unknown(42)",
	}
	for area, want := range tests {
		if got := area.String(); got != want {
			t.Fatalf("Area(%d).String() = %q, want %q", area, got, want)
		}
	}
}

func TestBindingsFallsBackToGlobal(t *testing.T) {
	k := DefaultKeyMap()
	if len(k.Bindings(Area(42))) != len(k.Bindings(AreaGlobal)) {
		t.Fatal("an unknown area did not fall back to the global bindings")
	}
}

func TestHelpStylesComeFromTheTheme(t *testing.T) {
	th := NewTheme(Options{Dark: true})
	styles := HelpStyles(th)

	if styles.FullKey.Render("x") != th.Key.Render("x") {
		t.Fatal("the help key style does not come from the theme")
	}
	if styles.FullDesc.Render("x") != th.Desc.Render("x") {
		t.Fatal("the help description style does not come from the theme")
	}
}
