package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
)

// The acceptance criterion of issue #289: a file rebinds one action, and
// everything it does not name keeps its default.
func TestParseKeyMapAppliesAPartialOverride(t *testing.T) {
	keys, err := ParseKeyMap([]byte("Retile: ctrl+t\n"), "keys.yaml")
	if err != nil {
		t.Fatalf("ParseKeyMap() = %v", err)
	}

	if got := keys.Retile.Keys(); !slices.Equal(got, []string{"ctrl+t"}) {
		t.Fatalf("Retile is bound to %v, want the override", got)
	}
	defaults := DefaultKeyMap()
	if got := keys.Split.Keys(); !slices.Equal(got, defaults.Split.Keys()) {
		t.Fatalf("Split moved to %v although the file never named it", got)
	}
	if got, want := keys.Retile.Help().Desc, defaults.Retile.Help().Desc; got != want {
		t.Fatalf("the override lost the description: %q, want %q", got, want)
	}
}

// Several keys for one action, and the action name matched the way a user
// writes it rather than the way the struct declares it.
func TestParseKeyMapReadsListsAndAnyCase(t *testing.T) {
	keys, err := ParseKeyMap([]byte("broadcastall: [b, ctrl+b]\nRETILE: ctrl+t\n"), "keys.yaml")
	if err != nil {
		t.Fatalf("ParseKeyMap() = %v", err)
	}
	if got := keys.BroadcastAll.Keys(); !slices.Equal(got, []string{"b", "ctrl+b"}) {
		t.Fatalf("BroadcastAll is bound to %v", got)
	}
	if got := keys.Retile.Keys(); !slices.Equal(got, []string{"ctrl+t"}) {
		t.Fatalf("Retile is bound to %v", got)
	}
}

// A key is stored the way a terminal reports it, whatever order the modifiers
// were written in and whichever alias was used - a binding that could never
// fire is not a binding.
func TestParseKeyMapCanonicalisesKeys(t *testing.T) {
	keys, err := ParseKeyMap([]byte("ClosePane: shift+alt+x\nSearchLeave: Escape\nScrollUp: shift+PageUp\n"), "keys.yaml")
	if err != nil {
		t.Fatalf("ParseKeyMap() = %v", err)
	}
	for _, tc := range []struct {
		action string
		got    []string
		want   string
	}{
		{"ClosePane", keys.ClosePane.Keys(), "alt+shift+x"},
		{"SearchLeave", keys.SearchLeave.Keys(), "esc"},
		{"ScrollUp", keys.ScrollUp.Keys(), "shift+pgup"},
	} {
		if !slices.Equal(tc.got, []string{tc.want}) {
			t.Errorf("%s is bound to %v, want %q", tc.action, tc.got, tc.want)
		}
	}
}

// An empty document is a user who commented everything out, not a keymap with
// no keys in it.
func TestParseKeyMapEmptyDocumentKeepsTheDefaults(t *testing.T) {
	for _, doc := range []string{"", "# nothing yet\n"} {
		keys, err := ParseKeyMap([]byte(doc), "keys.yaml")
		if err != nil {
			t.Fatalf("ParseKeyMap(%q) = %v", doc, err)
		}
		if got := keys.Retile.Keys(); !slices.Equal(got, DefaultKeyMap().Retile.Keys()) {
			t.Fatalf("an empty document changed Retile to %v", got)
		}
	}
}

// The other acceptance criterion: every way of getting it wrong fails at
// startup, naming the entry that is wrong and the line it is on.
func TestParseKeyMapErrors(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []string // substrings the message must carry
	}{
		{
			name: "unknown action",
			doc:  "Retile: ctrl+t\nRetiel: ctrl+u\n",
			want: []string{"keys.yaml:2", "Retiel", "unknown action"},
		},
		{
			name: "unknown modifier",
			doc:  "Retile: ctl+t\n",
			want: []string{"keys.yaml:1", "Retile", "ctl", "not a modifier"},
		},
		{
			name: "unknown key name",
			doc:  "Retile: ctrl+enterr\n",
			want: []string{"keys.yaml:1", "Retile", "enterr"},
		},
		{
			name: "shifted letters are written as the character",
			doc:  "Retile: shift+t\n",
			want: []string{"Retile", "shifted character", "\"T\""},
		},
		{
			name: "an empty key",
			doc:  "Retile: \"\"\n",
			want: []string{"Retile", "empty key"},
		},
		{
			name: "an action with no value at all",
			doc:  "Retile:\n",
			want: []string{"Retile", "no key given"},
		},
		{
			name: "an empty list",
			doc:  "Retile: []\n",
			want: []string{"Retile", "no key given"},
		},
		{
			name: "the prefix cannot be rebound",
			doc:  "Prefix: ctrl+b\n",
			want: []string{"keys.yaml:1", "Prefix", "cannot be rebound"},
		},
		{
			name: "the literal cannot be rebound",
			doc:  "PrefixLiteral: ctrl+b\n",
			want: []string{"PrefixLiteral", "literal ctrl+a", "cannot be rebound"},
		},
		{
			name: "one action bound twice",
			doc:  "Retile: ctrl+t\nretile: ctrl+u\n",
			want: []string{"keys.yaml:2", "Retile", "twice", "line 1"},
		},
		{
			name: "a document that is not a mapping",
			doc:  "- Retile\n- Split\n",
			want: []string{"keys.yaml:1", "mapping"},
		},
		{
			name: "a value that is neither a key nor a list",
			doc:  "Retile:\n  keys: ctrl+t\n",
			want: []string{"Retile", "key or a list of keys"},
		},
		{
			name: "malformed YAML",
			doc:  "Retile: [ctrl+t\n",
			want: []string{"keys.yaml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := ParseKeyMap([]byte(tt.doc), "keys.yaml")
			if err == nil {
				t.Fatalf("ParseKeyMap(%q) succeeded, want an error", tt.doc)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to name %q", err, want)
				}
			}
			// A rejected file changes nothing: the shipped keymap answers.
			if got := keys.Retile.Keys(); !slices.Equal(got, DefaultKeyMap().Retile.Keys()) {
				t.Errorf("a rejected keymap still moved Retile to %v", got)
			}
		})
	}
}

// A missing file is a user who never wrote one.
func TestLoadKeyMapWithoutAFileIsTheDefault(t *testing.T) {
	keys, err := LoadKeyMap(filepath.Join(t.TempDir(), "keys.yaml"))
	if err != nil {
		t.Fatalf("LoadKeyMap() = %v", err)
	}
	if got := keys.Retile.Keys(); !slices.Equal(got, DefaultKeyMap().Retile.Keys()) {
		t.Fatalf("Retile = %v without a file", got)
	}
}

func TestLoadKeyMapReadsTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.yaml")
	if err := os.WriteFile(path, []byte("Retile: ctrl+t\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	keys, err := LoadKeyMap(path)
	if err != nil {
		t.Fatalf("LoadKeyMap() = %v", err)
	}
	if got := keys.Retile.Keys(); !slices.Equal(got, []string{"ctrl+t"}) {
		t.Fatalf("Retile = %v", got)
	}
}

// A broken file names itself in the error, because the user has to find it.
func TestLoadKeyMapNamesTheFileInAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.yaml")
	if err := os.WriteFile(path, []byte("Nonsense: ctrl+t\n"), 0o600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	if _, err := LoadKeyMap(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("LoadKeyMap() = %v, want the path named", err)
	}
}

func TestKeyMapPathFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/config")
	path, err := KeyMapPath()
	if err != nil {
		t.Fatalf("KeyMapPath() = %v", err)
	}
	if want := filepath.Join("/tmp/config", "lazycssh", "keys.yaml"); path != want {
		t.Fatalf("KeyMapPath() = %q, want %q", path, want)
	}
}

// The help is generated from the effective keymap, so a rebound action is
// documented under the key it now answers to - and keeps its description.
func TestOverriddenBindingsShowInTheHelp(t *testing.T) {
	keys, err := ParseKeyMap([]byte("Retile: ctrl+t\n"), "keys.yaml")
	if err != nil {
		t.Fatalf("ParseKeyMap() = %v", err)
	}

	model := help.New()
	th := NewTheme(Options{Dark: true})
	model.Styles = HelpStyles(&th)
	model.SetWidth(400)

	rendered := model.FullHelpView(keys.For(AreaGlobal).FullHelp())
	if !strings.Contains(rendered, "ctrl+t") {
		t.Fatalf("the overlay does not name the remapped key:\n%s", rendered)
	}
	if strings.Contains(rendered, "ctrl+r") {
		t.Fatalf("the overlay still names the default key:\n%s", rendered)
	}
	if !strings.Contains(rendered, keys.Retile.Help().Desc) {
		t.Fatalf("the overlay lost the description:\n%s", rendered)
	}
}

// A chord key is only ever pressed after the prefix, so its label says so
// however it was remapped: the indicator cannot drift from the binding.
func TestOverriddenChordKeepsThePrefixInItsLabel(t *testing.T) {
	keys, err := ParseKeyMap([]byte("PrefixNext: n\nPrefixCancel: ctrl+g\n"), "keys.yaml")
	if err != nil {
		t.Fatalf("ParseKeyMap() = %v", err)
	}
	if got, want := keys.PrefixNext.Help().Key, "ctrl+a n"; got != want {
		t.Fatalf("PrefixNext label = %q, want %q", got, want)
	}
	if got, want := keys.PrefixCancel.Help().Key, "ctrl+a ctrl+g"; got != want {
		t.Fatalf("PrefixCancel label = %q, want %q", got, want)
	}
}

// The status bar promises keys, and the promise is read from the bindings: a
// remapped escape moves what the bar says leaves.
func TestStatusBarHintsFollowTheKeyMap(t *testing.T) {
	keys, err := ParseKeyMap([]byte("LeaveTyping: ctrl+g\n"), "keys.yaml")
	if err != nil {
		t.Fatalf("ParseKeyMap() = %v", err)
	}

	a, fleet, _, _ := statusAppWithKeys(t, &keys, "web-01")
	fleet.connect(t, "web-01")
	a = focusGrid(t, a)

	view := plain(a.View().Content)
	if !strings.Contains(view, "ctrl+g leaves") {
		t.Fatalf("the status bar does not name the remapped escape:\n%s", view)
	}
	if strings.Contains(view, escapeKeystroke+" leaves") {
		t.Fatalf("the status bar still promises the default escape:\n%s", view)
	}
}

// Every key the shipped keymap binds can be written in a keymap file, and is
// written the same way it is stored. The two space spellings are the exception
// the defaults carry on purpose: terminals disagree about which one they send.
func TestEveryDefaultKeyCanBeWrittenInAFile(t *testing.T) {
	for name, binding := range bindingFields(DefaultKeyMap()) {
		for _, pressed := range binding.Keys() {
			canonical, err := canonicalKeystroke(pressed)
			if err != nil {
				t.Errorf("%s: the shipped key %q cannot be written in a keymap file: %v",
					name, pressed, err)
				continue
			}
			if canonical != pressed && pressed != " " && pressed != "alt+ " {
				t.Errorf("%s: the shipped key %q canonicalises to %q", name, pressed, canonical)
			}
		}
	}
}

func TestParseKeystroke(t *testing.T) {
	tests := []struct {
		written   string
		canonical string
		want      tea.KeyPressMsg
	}{
		{"a", "a", tea.KeyPressMsg{Code: 'a', Text: "a"}},
		{"A", "A", tea.KeyPressMsg{Code: 'A', Text: "A"}},
		{"?", "?", tea.KeyPressMsg{Code: '?', Text: "?"}},
		{"ctrl+a", "ctrl+a", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}},
		{"ctrl+]", "ctrl+]", tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl}},
		{"alt++", "alt++", tea.KeyPressMsg{Code: '+', Mod: tea.ModAlt}},
		{"+", "+", tea.KeyPressMsg{Code: '+', Text: "+"}},
		{"shift+tab", "shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}},
		{"ctrl+shift+left", "ctrl+shift+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl | tea.ModShift}},
		{"shift+ctrl+left", "ctrl+shift+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl | tea.ModShift}},
		{"super+left", "super+left", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper}},
		{"f5", "f5", tea.KeyPressMsg{Code: tea.KeyF5}},
		{"space", "space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
		{" ", "space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}},
		{"alt+ ", "alt+space", tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModAlt}},
		{"CTRL+Q", "ctrl+q", tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl}},
	}

	for _, tt := range tests {
		t.Run(tt.written, func(t *testing.T) {
			msg, err := parseKeystroke(tt.written)
			if err != nil {
				t.Fatalf("parseKeystroke(%q) = %v", tt.written, err)
			}
			if msg != tt.want {
				t.Errorf("parseKeystroke(%q) = %+v, want %+v", tt.written, msg, tt.want)
			}
			canonical, err := canonicalKeystroke(tt.written)
			if err != nil {
				t.Fatalf("canonicalKeystroke(%q) = %v", tt.written, err)
			}
			if canonical != tt.canonical {
				t.Errorf("canonicalKeystroke(%q) = %q, want %q", tt.written, canonical, tt.canonical)
			}
			// What a keymap file writes is what a press reports back, which is
			// what key.Matches compares - the property the whole format rests on.
			if got := msg.String(); got != tt.canonical {
				t.Errorf("a press of %q reports %q, want the stored %q", tt.written, got, tt.canonical)
			}
		})
	}
}

func TestParseKeystrokeRejects(t *testing.T) {
	for _, written := range []string{"", "ctl+b", "kontrol+b", "ctrl+enterr", "shift+t", "abc"} {
		if msg, err := parseKeystroke(written); err == nil {
			t.Errorf("parseKeystroke(%q) = %+v, want an error", written, msg)
		}
	}
}

// The vocabulary is listable: every binding is an action, and the two that may
// not move say so.
func TestKeyMapActionsCoverTheKeyMap(t *testing.T) {
	actions := KeyMapActions()
	if got, want := len(actions), len(bindingFields(DefaultKeyMap())); got != want {
		t.Fatalf("KeyMapActions() lists %d actions, the keymap declares %d", got, want)
	}
	if !slices.IsSortedFunc(actions, func(a, b KeyMapAction) int { return strings.Compare(a.Name, b.Name) }) {
		t.Fatal("KeyMapActions() is not sorted by name")
	}

	fixed := make(map[string]bool)
	for _, action := range actions {
		if action.Description == "" || len(action.Keys) == 0 {
			t.Errorf("%s is listed without keys or a description", action.Name)
		}
		if action.Fixed {
			fixed[action.Name] = true
		}
	}
	if !fixed["Prefix"] || !fixed["PrefixLiteral"] {
		t.Fatalf("the fixed actions are %v, want the prefix and its literal", fixed)
	}
}
