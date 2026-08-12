package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// loadKeys builds the run's keymap: the shipped bindings with the user's
// overrides applied.
//
// An empty path is the default location, and a missing file there is a user who
// never wrote one - the shipped bindings answer. A path given on the command
// line is a promise that the file exists, so a missing one is an error rather
// than a silent fallback to defaults the user did not ask for.
func loadKeys(path string) (ui.KeyMap, error) {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return ui.DefaultKeyMap(), fmt.Errorf("keymap: %s does not exist", path)
			}
			return ui.DefaultKeyMap(), fmt.Errorf("keymap: %s: %w", path, err)
		}
		return ui.LoadKeyMap(path)
	}

	path, err := ui.KeyMapPath()
	if err != nil {
		return ui.DefaultKeyMap(), err
	}
	return ui.LoadKeyMap(path)
}

// printKeyActions lists what a keymap file may bind. A configuration surface
// whose vocabulary cannot be listed is one nobody can write, and the action
// names are the vocabulary.
func printKeyActions(stdout io.Writer) {
	for _, action := range ui.KeyMapActions() {
		line := fmt.Sprintf("%s\t%s\t%s", action.Name, strings.Join(action.Keys, " "), action.Description)
		if action.Fixed {
			line += " [fixed: cannot be rebound]"
		}
		fmt.Fprintln(stdout, line)
	}
}
