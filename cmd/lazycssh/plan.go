package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// sessionPrefix marks an argument as a saved session name rather than a host.
const sessionPrefix = "@"

// plan is what a command line resolves to before anything is dialled: the host
// patterns to open, in the order they were given, plus the run settings the
// sessions asked for.
type plan struct {
	// Patterns are host arguments, unexpanded, in command line order.
	Patterns []string
	// Sessions are the saved sessions that contributed, in the order named.
	Sessions []*sessions.Session
	// Broadcast is the mode the run starts in.
	Broadcast broadcast.Mode
	// WorkingSet is the working set the run starts with, or nil for all hosts.
	WorkingSet workingset.Selector
}

// SessionNames returns the names of the sessions that contributed.
func (p plan) SessionNames() []string {
	names := make([]string, len(p.Sessions))
	for i, s := range p.Sessions {
		names[i] = s.Name
	}
	return names
}

// resolvePlan turns command line arguments into a plan.
//
// An argument starting with "@" names a saved session; everything else is a
// host pattern. They can be mixed - `lazycssh @prod-web extra.example.com`
// opens both - and the order is preserved, because the order hosts appear in is
// the order their panes are tiled in.
//
// A later session's broadcast mode and working set win over an earlier one's:
// the last thing the user named is the thing they meant.
func resolvePlan(args []string, store *sessions.Store) (plan, error) {
	p := plan{Broadcast: broadcast.ModeAll}

	for _, arg := range args {
		name, isSession := strings.CutPrefix(arg, sessionPrefix)
		if !isSession {
			p.Patterns = append(p.Patterns, arg)
			continue
		}
		if store == nil {
			return plan{}, fmt.Errorf("session %q: no session directory is available", name)
		}

		sess, err := store.Load(name)
		if err != nil {
			if errors.Is(err, sessions.ErrNotFound) {
				return plan{}, unknownSessionError(name, store)
			}
			return plan{}, err
		}

		p.Sessions = append(p.Sessions, sess)
		p.Patterns = append(p.Patterns, sess.Patterns()...)

		mode, err := sess.Mode()
		if err != nil {
			return plan{}, err
		}
		p.Broadcast = mode

		sel, err := sess.Selector()
		if err != nil {
			return plan{}, err
		}
		if sel != nil {
			p.WorkingSet = sel
		}
	}

	if len(p.Patterns) == 0 && len(p.Sessions) > 0 {
		return plan{}, fmt.Errorf("session %s: no hosts", strings.Join(p.SessionNames(), ", "))
	}
	return p, nil
}

// unknownSessionError explains what is saved instead of only what is missing: a
// user who mistypes a session name wants the list, not a second guess.
func unknownSessionError(name string, store *sessions.Store) error {
	available, err := store.List()
	if err != nil {
		return fmt.Errorf("session %q: not found (and %s could not be read: %w)", name, store.Dir(), err)
	}
	if len(available) == 0 {
		return fmt.Errorf("session %q: not found; no sessions are saved in %s", name, store.Dir())
	}
	return fmt.Errorf("session %q: not found; available sessions: %s",
		name, strings.Join(available, ", "))
}
