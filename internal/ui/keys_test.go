package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
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

// managedKeys are the keys the prompts and dialogs answer. They used to be
// matched with raw msg.String() comparisons scattered over every prompt site,
// which made them invisible to the help and to the invariants above; issue
// #226 moved them into [KeyMap.prompts]. The test below is what keeps them
// there.
var managedKeys = map[string]bool{
	"esc": true, "enter": true, "tab": true, "up": true, "down": true,
	"y": true, "Y": true, "n": true, "N": true,
	"ctrl+c": true, "ctrl+q": true, "backspace": true, "ctrl+a": true,
}

// No prompt matches a key by comparing msg.String() to a literal: a key that
// is compared by hand is a key the overlay and the box footers cannot see.
//
// The check parses the package rather than grepping it, so a literal in a
// comment or in a message string is not a false positive - only a comparison
// against msg.String() counts, which is exactly the pattern being banned.
func TestPromptKeysAreMatchedThroughTheKeyMap(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parsing the package: %v", err)
	}

	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.SwitchStmt:
					if !isKeyString(node.Tag) {
						return true
					}
					t.Errorf("%s: switch msg.String() dispatches keys by literal; "+
						"match them with key.Matches against a KeyMap binding",
						fset.Position(node.Pos()))
				case *ast.BinaryExpr:
					if node.Op != token.EQL && node.Op != token.NEQ {
						return true
					}
					if !isKeyString(node.X) && !isKeyString(node.Y) {
						return true
					}
					for _, side := range []ast.Expr{node.X, node.Y} {
						lit, ok := side.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						if pressed, err := strconv.Unquote(lit.Value); err == nil && managedKeys[pressed] {
							t.Errorf("%s: %s is compared to msg.String(); "+
								"match it with key.Matches against a KeyMap binding",
								fset.Position(lit.Pos()), lit.Value)
						}
					}
				}
				return true
			})
		}
	}
}

// The prompts really go through the keymap: rebinding cancel moves what closes
// the command line, which a literal comparison could not do.
func TestRebindingCancelMovesThePromptKey(t *testing.T) {
	a := testApp()
	a.keys.PromptCancel = key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "cancel"))

	a = pressKey(t, a, ":")
	if !a.CommandLineOpen() {
		t.Fatal("the command line did not open")
	}
	a = pressKey(t, a, "esc")
	if !a.CommandLineOpen() {
		t.Fatal("esc closed the command line although cancel was rebound off it")
	}
	a = pressKey(t, a, "ctrl+g")
	if a.CommandLineOpen() {
		t.Fatal("the rebound cancel key did not close the command line")
	}
}

// isKeyString reports whether an expression is a call to a key message's
// String method - the shape "msg.String()".
func isKeyString(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "String" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "msg"
}

// Every prompt binding is declared with the help the overlay needs, and the
// prompt area is one of the areas the overlay lists - which is how a user with
// a box in front of them can find out what answers it.
func TestPromptAreaIsInTheHelp(t *testing.T) {
	k := DefaultKeyMap()

	var listed bool
	for _, area := range Areas() {
		listed = listed || area == AreaPrompt
	}
	if !listed {
		t.Fatal("AreaPrompt is not one of the areas the help lists")
	}
	if len(k.Bindings(AreaPrompt)) == 0 {
		t.Fatal("the prompt area declares no bindings")
	}

	model := help.New()
	th := NewTheme(Options{Dark: true})
	model.Styles = HelpStyles(&th)
	model.SetWidth(400)
	rendered := model.FullHelpView(k.For(AreaSidebar).FullHelp())
	for _, b := range k.Bindings(AreaPrompt) {
		if !strings.Contains(rendered, b.Help().Desc) {
			t.Fatalf("the overlay does not mention the prompt binding %q:\n%s", b.Help().Desc, rendered)
		}
	}
}

// The chord that quits from inside a text input is declared, not typed into a
// comparison, and the app-level quit advertises it too.
func TestQuitIsReachableFromInsideAPrompt(t *testing.T) {
	k := DefaultKeyMap()
	if !slices.Contains(k.ForceQuit.Keys(), "ctrl+q") {
		t.Fatalf("ForceQuit is bound to %v, want ctrl+q", k.ForceQuit.Keys())
	}
	if !slices.Contains(k.Quit.Keys(), "ctrl+q") {
		t.Fatalf("Quit is bound to %v; the chord it documents must be one of them", k.Quit.Keys())
	}
}

// A dialog footer is assembled from the bindings that answer it, so rebinding
// a key moves the hint with it instead of leaving a lie in the box.
func TestPromptHintComesFromTheBindings(t *testing.T) {
	k := DefaultKeyMap()
	if got, want := promptHint(does(k.PromptSubmit, "connects"), does(k.PromptCancel, "cancels")),
		"enter connects · esc cancels"; got != want {
		t.Fatalf("promptHint() = %q, want %q", got, want)
	}
	if got, want := promptHint(note("empty or 0 shows all"), does(k.PromptSubmit, "applies")),
		"empty or 0 shows all · enter applies"; got != want {
		t.Fatalf("promptHint() with a note = %q, want %q", got, want)
	}

	k.PromptCancel = key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "cancel"))
	if got, want := promptHint(does(k.PromptCancel, "cancels")), "ctrl+g cancels"; got != want {
		t.Fatalf("a rebound key did not move its hint: %q, want %q", got, want)
	}
	k.ConfirmNo = key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "no"))
	if got, want := confirmHint(&k), "enter/y confirms · ctrl+g cancels"; got != want {
		t.Fatalf("confirmHint() = %q, want %q", got, want)
	}
}

// deliberateShadows are the keys that intentionally mean something else in one
// panel than they do globally, lazygit style: the Groups panel keeps n and d
// for itself, and the router in handleKey resolves them before the global
// bindings are consulted. Every other duplicate is a bug.
var deliberateShadows = map[Area]map[string]bool{
	AreaSidebar: {"n": true, "d": true},
}

// No key binding is ambiguous between two things that are handled at the same
// time: an area's own bindings plus the global ones - except the declared
// panel shadows, which are resolved by routing order.
func TestNoAmbiguousBindingsWithinAnArea(t *testing.T) {
	k := DefaultKeyMap()

	for _, area := range []Area{AreaSidebar, AreaGrid} {
		t.Run(area.String(), func(t *testing.T) {
			seen := make(map[string]string)
			active := append(k.Bindings(area), k.Bindings(AreaGlobal)...)

			for _, b := range active {
				for _, pressed := range b.Keys() {
					if deliberateShadows[area][pressed] {
						continue
					}
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
				if deliberateShadows[area][pressed] {
					continue
				}
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
	th := NewTheme(Options{Dark: true})
	model.Styles = HelpStyles(&th)
	model.SetWidth(200)

	rendered := model.FullHelpView(k.For(AreaGrid).FullHelp())
	for _, b := range k.Bindings(AreaGrid) {
		if !strings.Contains(rendered, b.Help().Desc) {
			t.Fatalf("the overlay does not mention %q:\n%s", b.Help().Desc, rendered)
		}
	}

	short := model.ShortHelpView(k.For(AreaGrid).ShortHelp())
	if !strings.Contains(short, "stop typing to the host") {
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
	styles := HelpStyles(&th)

	if styles.FullKey.Render("x") != th.Key.Render("x") {
		t.Fatal("the help key style does not come from the theme")
	}
	if styles.FullDesc.Render("x") != th.Desc.Render("x") {
		t.Fatal("the help description style does not come from the theme")
	}
}

// The machine-checkable form of "typing never collides with a command": every
// grid binding is an alt or shift chord, or the one reserved escape, so a
// plain keystroke always belongs to the host.
func TestGridBindingsAreAllModified(t *testing.T) {
	k := DefaultKeyMap()
	for _, b := range k.grid() {
		for _, pressed := range b.Keys() {
			if pressed == "ctrl+]" {
				continue
			}
			if !strings.HasPrefix(pressed, "alt+") && !strings.HasPrefix(pressed, "shift+") {
				t.Errorf("grid binding %q would be typed into the host", pressed)
			}
		}
	}
}
