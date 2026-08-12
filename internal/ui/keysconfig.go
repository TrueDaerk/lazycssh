package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"gopkg.in/yaml.v3"
)

// The user keymap file (issue #289).
//
// Every binding is declared once in [DefaultKeyMap], and the file below moves
// individual ones without touching the rest: it is a YAML mapping of action
// name to key or list of keys, and an action that is not named keeps its
// default. The format follows the session files - YAML, one document, no
// includes - so there is one configuration language in this program.
//
//	# ~/.config/lazycssh/keys.yaml
//	Retile: ctrl+t
//	BroadcastAll: [b, ctrl+b]
//
// Nothing is guessed. An action nobody declared, a key name that no terminal
// can produce, or an attempt to move the reserved chord prefix is an error at
// startup, naming the line it came from: a keymap that silently dropped half
// its entries would be discovered while typing to forty machines.

// KeyMapFileName is the file a run reads its binding overrides from.
const KeyMapFileName = "keys.yaml"

// fixedActions are the bindings a configuration may not touch, with the reason
// the error message gives. The chord prefix and its literal escape are the way
// out of every mode this program has - remapping them away would leave a user
// with no portable way to page, and no way at all to send the ctrl+a a remote
// screen, tmux or readline needs.
var fixedActions = map[string]string{
	"Prefix": "ctrl+a is the lazycssh command prefix and cannot be rebound",
	"PrefixLiteral": "ctrl+a ctrl+a always sends one literal ctrl+a to the hosts " +
		"and cannot be rebound",
}

// chordActions are the bindings whose help label reads as the second key of the
// chord, so a remapped one renders "ctrl+a n" rather than a bare "n".
var chordActions = map[string]bool{
	"PrefixNext":   true,
	"PrefixPrev":   true,
	"PrefixCancel": true,
}

// KeyMapPath is where the optional keymap file lives:
// `$XDG_CONFIG_HOME/lazycssh/keys.yaml`, falling back to
// `~/.config/lazycssh/keys.yaml` - the location the sessions, the history and
// the recent hosts already use.
func KeyMapPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lazycssh", KeyMapFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("keymap: locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lazycssh", KeyMapFileName), nil
}

// LoadKeyMap reads path over [DefaultKeyMap].
//
// A missing file is not an error: it is a user who never wrote one, and the
// shipped bindings are the answer. Anything else - an unreadable file, a
// malformed document, an unknown action, a key name that cannot be pressed -
// is returned, because a keymap that half applied is worse than none.
func LoadKeyMap(path string) (KeyMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultKeyMap(), nil
		}
		return DefaultKeyMap(), fmt.Errorf("keymap: read %s: %w", path, err)
	}
	return ParseKeyMap(data, path)
}

// ParseKeyMap applies a YAML keymap document over [DefaultKeyMap]. source names
// the file in the error messages; it may be empty for a document that came from
// nowhere in particular.
func ParseKeyMap(data []byte, source string) (KeyMap, error) {
	keys := DefaultKeyMap()

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return keys, fmt.Errorf("keymap: %s: %w", where(source), err)
	}
	// An empty file is a user who commented everything out.
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return keys, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return keys, fmt.Errorf("keymap: %s: the document is not a mapping of "+
			"action to key, for example \"Retile: ctrl+t\"", at(source, root.Line))
	}

	// The whole document is read before anything is applied: a file with a
	// typo on its last line leaves the shipped keymap untouched rather than
	// starting the run half remapped.
	type override struct {
		field string
		bound []string
	}
	var overrides []override
	seen := make(map[string]int, len(root.Content)/2)

	for i := 0; i+1 < len(root.Content); i += 2 {
		nameNode, valueNode := root.Content[i], root.Content[i+1]
		name := nameNode.Value

		field, ok := actionField(name)
		if !ok {
			return keys, fmt.Errorf("keymap: %s: unknown action %q; "+
				"run lazycssh -list-key-actions for the names",
				at(source, nameNode.Line), name)
		}
		if reason, fixed := fixedActions[field]; fixed {
			return keys, fmt.Errorf("keymap: %s: %s: %s",
				at(source, nameNode.Line), field, reason)
		}
		if line, dup := seen[field]; dup {
			return keys, fmt.Errorf("keymap: %s: %s is bound twice; the first is on line %d",
				at(source, nameNode.Line), field, line)
		}
		seen[field] = nameNode.Line

		bound, err := parseKeyList(valueNode)
		if err != nil {
			return keys, fmt.Errorf("keymap: %s: %s: %w",
				at(source, valueNode.Line), field, err)
		}
		overrides = append(overrides, override{field: field, bound: bound})
	}

	for _, o := range overrides {
		setBinding(&keys, o.field, o.bound)
	}
	return keys, nil
}

// parseKeyList reads the value side of an entry: one key as a scalar, or
// several as a sequence. Every key is canonicalised, so an entry written
// "shift+alt+x" binds the "alt+shift+x" a terminal actually reports.
func parseKeyList(node *yaml.Node) ([]string, error) {
	var raw []string
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			return nil, errors.New("no key given; remove the entry to keep the default")
		}
		raw = []string{node.Value}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if item.Kind != yaml.ScalarNode {
				return nil, errors.New("a key must be written as text")
			}
			raw = append(raw, item.Value)
		}
	default:
		return nil, errors.New("expected a key or a list of keys")
	}

	if len(raw) == 0 {
		return nil, errors.New("no key given; remove the entry to keep the default")
	}

	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		canonical, err := canonicalKeystroke(entry)
		if err != nil {
			return nil, err
		}
		out = append(out, canonical)
	}
	return out, nil
}

// setBinding replaces one field of a keymap, keeping the shipped description
// and rebuilding the help label from the new keys - which is what makes the `?`
// overlay and the status-bar hints show the effective bindings rather than the
// defaults.
func setBinding(k *KeyMap, field string, keys []string) {
	v := reflect.ValueOf(k).Elem().FieldByName(field)
	old, _ := v.Interface().(key.Binding)

	label := strings.Join(keys, "/")
	if chordActions[field] {
		// The chord's keys are only ever pressed after the prefix, so the
		// label says so - the same shape the defaults carry.
		label = prefixKeystroke + " " + label
	}
	v.Set(reflect.ValueOf(key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(label, old.Help().Desc),
	)))
}

// actionField resolves a written action name to the [KeyMap] field it names,
// case-insensitively: a user typing "retile" means Retile.
func actionField(name string) (string, bool) {
	field, ok := actionFields()[strings.ToLower(strings.TrimSpace(name))]
	return field, ok
}

// actionFields is the lookup behind [actionField]: every binding field of
// [KeyMap] by its lower-case name.
func actionFields() map[string]string {
	t := reflect.TypeOf(KeyMap{})
	out := make(map[string]string, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Type != reflect.TypeOf(key.Binding{}) {
			continue
		}
		out[strings.ToLower(field.Name)] = field.Name
	}
	return out
}

// KeyMapActions returns the action names a keymap file may bind, in
// alphabetical order, with the keys and the description the shipped keymap
// gives them. It is what `lazycssh -list-key-actions` prints: the names are
// [KeyMap] field names, and a configuration surface whose vocabulary cannot be
// listed is a configuration surface nobody can write.
func KeyMapActions() []KeyMapAction {
	defaults := bindingFields(DefaultKeyMap())
	out := make([]KeyMapAction, 0, len(defaults))
	for name, binding := range defaults {
		_, fixed := fixedActions[name]
		out = append(out, KeyMapAction{
			Name:        name,
			Keys:        binding.Keys(),
			Description: binding.Help().Desc,
			Fixed:       fixed,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// KeyMapAction is one bindable action: what a keymap file names on the left of
// a colon, and what it is bound to without one.
type KeyMapAction struct {
	// Name is the action name, matched case-insensitively in the file.
	Name string
	// Keys are the shipped bindings.
	Keys []string
	// Description is the help text the `?` overlay shows for it.
	Description string
	// Fixed reports that the action may not be rebound: the ctrl+a prefix and
	// its literal escape are part of how the program is left, not preferences.
	Fixed bool
}

// keyNames are the keys that are not one character, by the name a keymap file
// writes them with. The canonical name is what bubbletea reports back for a
// press; the aliases are what a user is likely to type instead.
var keyNames = buildKeyNames()

// buildKeyNames assembles [keyNames]. The function keys are regular enough to
// generate, and generating them keeps the table readable.
func buildKeyNames() map[string]rune {
	names := namedKeys()
	for i := range 24 {
		names[fmt.Sprintf("f%d", i+1)] = tea.KeyF1 + rune(i)
	}
	return names
}

// namedKeys is the hand-written half of [keyNames].
func namedKeys() map[string]rune {
	return map[string]rune{
		"up":        tea.KeyUp,
		"down":      tea.KeyDown,
		"left":      tea.KeyLeft,
		"right":     tea.KeyRight,
		"home":      tea.KeyHome,
		"end":       tea.KeyEnd,
		"pgup":      tea.KeyPgUp,
		"pgdown":    tea.KeyPgDown,
		"pageup":    tea.KeyPgUp,
		"pagedown":  tea.KeyPgDown,
		"insert":    tea.KeyInsert,
		"ins":       tea.KeyInsert,
		"delete":    tea.KeyDelete,
		"del":       tea.KeyDelete,
		"enter":     tea.KeyEnter,
		"return":    tea.KeyEnter,
		"tab":       tea.KeyTab,
		"esc":       tea.KeyEscape,
		"escape":    tea.KeyEscape,
		"space":     tea.KeySpace,
		" ":         tea.KeySpace,
		"backspace": tea.KeyBackspace,
		"begin":     tea.KeyBegin,
		"find":      tea.KeyFind,
		"select":    tea.KeySelect,
	}
}

// keyMods are the modifier names, in any order in the file: the canonical
// order bubbletea reports is restored by [canonicalKeystroke].
var keyMods = map[string]tea.KeyMod{
	"ctrl":    tea.ModCtrl,
	"control": tea.ModCtrl,
	"alt":     tea.ModAlt,
	"opt":     tea.ModAlt,
	"option":  tea.ModAlt,
	"meta":    tea.ModMeta,
	"shift":   tea.ModShift,
	"super":   tea.ModSuper,
	"cmd":     tea.ModSuper,
	"hyper":   tea.ModHyper,
}

// canonicalKeystroke renders a written key name the way bubbletea reports that
// press, which is what [key.Matches] compares against: modifiers in their fixed
// order, the canonical name for a special key. An unpressable name is an error
// rather than a binding that would never fire.
func canonicalKeystroke(written string) (string, error) {
	msg, err := parseKeystroke(written)
	if err != nil {
		return "", err
	}
	return tea.Key(msg).Keystroke(), nil
}

// parseKeystroke builds the key press a written keystroke describes:
// "ctrl+shift+left", "alt++", "?" - modifiers first, in any order, then one
// key name or one character.
func parseKeystroke(written string) (tea.KeyPressMsg, error) {
	var msg tea.KeyPressMsg

	// Not trimmed: " " and "alt+ " are the space key, which the shipped keymap
	// binds alongside "space" because terminals disagree about which of the two
	// they report.
	name := written
	if name == "" {
		return msg, errors.New("empty key")
	}

	var mod tea.KeyMod
	for {
		plus := strings.Index(name, "+")
		// A leading or trailing "+" is the key itself, not a separator:
		// "alt++" is alt and the plus key.
		if plus <= 0 || plus == len(name)-1 {
			break
		}
		found, ok := keyMods[strings.ToLower(name[:plus])]
		if !ok {
			return msg, fmt.Errorf("%q is not a key: %q is not a modifier "+
				"(ctrl, alt, shift, super, meta, hyper)", written, name[:plus])
		}
		mod |= found
		name = name[plus+1:]
	}

	code, named, err := keyCode(name, written)
	if err != nil {
		return msg, err
	}
	// A terminal reports shift+a as "A" - the shifted character - so a binding
	// written "shift+a" could never fire. Say so instead of accepting it.
	if !named && mod == tea.ModShift {
		return msg, fmt.Errorf("%q is not a key: write the shifted character "+
			"itself, for example %q", written, strings.ToUpper(name))
	}
	// A chord is reported with the unshifted character, so "ctrl+Q" is the
	// ctrl+q a terminal sends; shift in a chord is written as a modifier.
	if !named && mod != 0 {
		code = unicode.ToLower(code)
	}

	msg = tea.KeyPressMsg{Code: code, Mod: mod}
	if mod == 0 && unicode.IsPrint(code) {
		// A plain character press carries its text, and that text is what
		// [tea.Key.String] - and so key.Matches - compares. Space carries it
		// too, although it is a named key: a terminal reports it as text, and
		// an input that did not receive it would swallow every space typed.
		msg.Text = string(code)
	}
	return msg, nil
}

// keyCode resolves the key part of a keystroke - everything after the
// modifiers - reporting whether it was a named key rather than a character.
func keyCode(name, written string) (code rune, named bool, err error) {
	if code, ok := keyNames[strings.ToLower(name)]; ok {
		return code, true, nil
	}
	runes := []rune(name)
	if len(runes) == 1 {
		return runes[0], false, nil
	}
	if strings.Contains(name, "+") {
		return 0, false, fmt.Errorf("%q is not a key: %q is not a modifier "+
			"(ctrl, alt, shift, super, meta, hyper)", written, strings.SplitN(name, "+", 2)[0])
	}
	return 0, false, fmt.Errorf("%q is not a key: %q is not a key name", written, name)
}

// where names the source of a keymap document in an error, for the messages
// that have no line to point at.
func where(source string) string {
	if source == "" {
		return "keymap document"
	}
	return source
}

// at names the source and the line an error came from.
func at(source string, line int) string {
	if line <= 0 {
		return where(source)
	}
	return fmt.Sprintf("%s:%d", where(source), line)
}
