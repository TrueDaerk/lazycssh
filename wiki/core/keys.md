---
type: concept
title: Keymap and help
description: Every binding declared once, the help generated from it, and the rules that keep a key meaning one thing at a time.
resource: internal/ui/keys.go
tags: [ui, keys, help, bindings]
timestamp: 2026-08-09T23:30:00Z
---

# Keymap and help

Every binding is declared once, in `KeyMap`. The help line and the `?` overlay are generated
from that struct, so documentation cannot drift from behaviour: a binding that is not declared
is not shown, and a declared binding that is not shown fails a test.

## Areas

A key press is dispatched by focus. Each binding belongs to one area:

| Area | What it covers |
|------|----------------|
| `AreaGlobal` | works wherever focus is: help, quit, panel numbers, broadcast mode, the command line |
| `AreaSidebar` | the numbered panels down the left |
| `AreaBroadcast` | the broadcast bar under the grid — a terminal for the whole target set; kept for itself: `ctrl+]`, the pane chords, and the csshx-style `ctrl+a` escape prefix. In the bar's **view mode** every key is an app-level command instead — see [TUI shell](./tui.md#edit-and-view-mode) |
| `AreaGrid` | the host panes on the right — a focused pane is a terminal, so its bindings are all `alt`/`shift` chords plus the reserved `ctrl+]`; every plain key is forwarded to the host (a test enforces the chord rule) |
| `AreaPrompt` | the dialogs and inline prompts. **Not a focus target**: a prompt takes the keyboard from whatever had it, is resolved before any area binding is consulted, and hands it back when it closes |

The sidebar and the grid may reuse a key — they are never focused at the same time — but a
global binding may not collide with either, because the two are always live together. Both rules
are tests, not conventions. The one sanctioned exception is a **declared panel shadow**: the
Groups panel keeps `n` and `d` for itself, lazygit style, resolved by routing order before the
global bindings are consulted; the tests carry the explicit allowlist, so an undeclared
duplicate still fails.

`AreaPrompt` is deliberately outside those two collision rules. Its keys mean different things
in different boxes — `esc` cancels a prompt, answers *no* to a confirm and clears a mouse
selection — and which meaning applies is decided by *which prompt is open*, not by focus. A
prompt owns the keyboard outright while it is open, so nothing else it could collide with is
live at the same time.

## Bindings

| Key | Area | Action |
|-----|------|--------|
| `?` | global | help overlay |
| `q` / `ctrl+q` | global (app level) | quit — `q` only while no input has the keyboard; in any text field it is a letter, and while typing to a host both are forwarded. `ctrl+q` also quits from inside every text input |
| `tab` / `shift+tab` | global (app level) | next / previous stop in the cycle: each sidebar panel, then the grid; forwarded while typing |
| `1`–`4` | global (app level) | status, groups, sessions, command log |
| `5` | global (app level) | focus the broadcast bar |
| `b` / `B` / `s` | global | broadcast to the working set / the selection / one pane |
| `ctrl+alt+b` | global | broadcast to **every** host |
| `:` | global | send a command |
| `!` | global | jump to the next host whose last command failed |
| `S` | global (app level) | save the run as a session, prompt prefilled |
| `n` | global (app level) | connect a new host: opens the pattern prompt in the Status panel, ssh-config aliases complete with `tab` |
| `ctrl+r` | global (app level) | re-tile the grid for the current hosts (a departure keeps the shape) |
| `ctrl+s` | global (app level) | split the grid into chunks of N panes (prompt; empty or 0 clears) |
| `ctrl+shift+→` / `ctrl+shift+←` | global (works while typing too) | next / previous screenful: pages, then split chunks, wrapping at the ends. Plain `ctrl+arrows` are never claimed — they stay readline word movement for the hosts, and IDEs and window managers swallow them anyway (issue #208) |
| `a` | global (app level) | select every host |
| `i` | global (app level) | invert the selection |
| `c` | global (app level) | clear the selection |
| `u` | global (app level) | select the hosts that are up |
| `d` | global (app level) | select the hosts that are down |
| `↑`/`k`, `↓`/`j` | sidebar | move the cursor inside the focused panel only — a no-op at the top/bottom of the list, and on the Status panel, which has no list. Never switches the focused panel (issue #212) |
| `←`/`h`, `→`/`l` | sidebar | previous / next panel, stopping at Status / Command log — the explicit way to switch panels, since neither key means anything else while the sidebar has focus. `h`/`l` alias `←`/`→`, lazygit style, matching `j`/`k` on `↑`/`↓`; a pane and the broadcast bar keep plain `h`/`l` as keystrokes for the hosts (issue #220) |
| `enter` / `space` | sidebar | choose the row: open a group, foreground a session, resend a log entry |
| `w` | sidebar | save the run as a group |
| `n` | Groups panel | new group (shadows the global connect while the panel has focus) |
| `d` | Groups panel | delete the group under the cursor, after an `enter`/`y` confirm dialog (shadows the global select-down) |
| `x` | Sessions panel | end the session under the cursor, after an `enter`/`y` confirm dialog: ctrl+c and ctrl+d to its connected hosts |
| `[` / `]` | sidebar | previous / next chunk of hosts |
| any plain key | panes | **forwarded to the focused host** — letters, enter, tab, esc, ctrl+c, arrows, all of it. Each key goes through the host's own [terminal emulator](./terminal.md) (issue #206), so the bytes honour that host's modes (application cursor keys, keypad) |
| `alt+←`/`alt+→`, `ctrl+←`/`ctrl+→` | panes | word backward / forward on the remote line: `ESC b`/`ESC f` (opt+arrow on macOS, ctrl+arrow on Linux/Windows; issue #208) |
| `super+←`/`super+→` | panes | line start / end on the remote line: `ctrl+a`/`ctrl+e` (cmd+arrow on macOS) |
| `alt+backspace` / `alt+delete` | panes | kill the previous / next word: `ESC DEL` / `ESC d` (opt+backspace, opt+forward-delete) |
| `super+backspace` | panes | kill to line start: `ctrl+u` (cmd+backspace) |
| `alt+<char>` | panes (unbound chords) | meta: `ESC` + character, so `alt+b`/`alt+f`/`alt+.` reach readline |
| `ctrl+a` | broadcast bar (edit mode) | escape prefix, forwarding by default (issue #214): `ctrl+a ctrl+a` and `ctrl+a a` send a literal `ctrl+a` for a remote `screen`/`tmux` (the `BroadcastLiteral` binding), `esc` switches to view mode, and **any other key is forwarded to the targets** as a keystroke |
| `enter` | broadcast bar (view mode) | back to edit mode |
| `ctrl+]` | panes | stop typing: back to the app level, on the Status panel |
| `alt+space` | panes (and app level) | toggle the focused pane's host in the selection |
| `shift+alt+←`/`→`/`↑`/`↓` | panes (and app level) | move between panes — plain `alt+arrow` belongs to the shell (word navigation, issue #202) |
| `alt+z` | panes (and app level) | full-screen this pane, again to return — the direct toggle from any screen mode |
| `alt++` (also `alt+=`) | panes (and app level) | cycle the screen mode: normal / half / full — see [TUI shell](./tui.md#screen-modes). lazygit uses a plain `+`; here it takes `alt`, because a pane forwards `+` to the shell |
| `alt+r` | panes (and app level) | reconnect this host |
| `alt+x` | panes (and app level) | close this host; on a dead host, remove its pane |
| `alt+y` | panes (and app level) | copy this pane's visible text to the clipboard (OSC 52) |
| `ctrl+c` | with a live mouse selection | copy the selection (OSC 52) and clear it — no interrupt is sent; without a selection it stays a keystroke for the hosts |
| `alt+d` | panes (and app level) | copy this pane's whole scrollback to the clipboard (OSC 52) |
| `shift+pgup` / `shift+pgdn` | panes (and app level) | scroll the focused pane back / forward |
| `shift+home` / `shift+end` | panes (and app level) | oldest retained output / back to the tail |
| `alt+/` | panes (and app level) | search the scrollback |
| `alt+[` / `alt+]` | panes (and app level) | older / newer match |
| `alt+c` | panes (and app level) | clear the search |
| `enter` | prompts | apply what was typed: connect the pattern, save the group, run the command, jump to the newest match |
| `esc` | prompts | cancel the prompt, or clear a mouse selection |
| `tab` | the connect prompt | complete the first matching ssh-config alias |
| `backspace` | a pane's auth prompt | erase the last character — that prompt echoes into the scrollback rather than living in a text input, so it edits itself |
| `↑` / `↓` | the command line | previous / next command in this run's history |
| `enter` / `y` | confirm dialogs | answer yes |
| `esc` / `n` | confirm dialogs | answer no; every other key leaves the question standing, because these dialogs guard a file delete and a fleet-wide `ctrl+c` |
| `ctrl+c` | a pane's auth prompt | cancel that host's question (`esc` does the same) |
| `ctrl+q` | inside any prompt | quit — no prompt may trap the user, and the chord is never a character in a text input |

The fleet broadcast mode is `ctrl+alt+b` on purpose. It is the one mode that ignores the working
set, so it is not a single letter and not reachable by cycling through the others — see
[Broadcast scope](./broadcast-scope.md). A test asserts both the chord and that its help text
says `EVERY`.

## Help

`KeyMap.For(area)` returns a `help.KeyMap` describing what is live right now: the area's own
bindings plus the global ones. The short line along the bottom lists the handful a user needs
where they are, right-aligned on the status bar; `?` opens the keybindings popup, composited over
the frame, which leads with the focused area's column and then lists the others. On a terminal too
narrow for every column the help bubble drops whole columns behind an ellipsis rather than
wrapping them.

Styles come from the [theme](./theme.md) rather than the help bubble's defaults, so the overlay
matches the rest of the interface.

The overlay always carries the **prompts** column, whatever has focus. `?` cannot be pressed
*while* a box is open — the box has the keyboard — so the column is where a user reads up on
what answers a dialog before opening one.

## Dialog footers

The muted footer inside every box (`tab completes · enter connects · esc cancels`) is assembled
from the same bindings the handler matches, by `promptHint(does(binding, verb), …)` in
`internal/ui/keys.go`. Only the verb is written at the call site; the key label comes from the
binding, so a rebinding moves the footer with it and a footer cannot name a key the handler does
not take. `note("empty or 0 shows all")` is the escape hatch for the things a prompt has to say
about its *input* rather than about its keys.

## No hand-matched keys

Prompts used to compare `msg.String()` to literals, which put `esc`, `y`/`Y`, `tab`, the history
arrows and `ctrl+q` outside `KeyMap` — invisible to the overlay and to the invariant tests
above. Issue #226 moved them in. `TestPromptKeysAreMatchedThroughTheKeyMap` parses the package
and fails on any comparison of `msg.String()` against one of those keys, so the drift cannot
come back.

## Mouse

| Action | Where | Effect |
|--------|-------|--------|
| left click | pane body | focus the pane and start typing into its host |
| left click | `[x]` in a pane header | close a live host; remove a dead one |
| left click | sidebar box / row | select the panel, move its cursor to the row |
| left click | broadcast bar | give the bar the keyboard |
| wheel | over a pane | scroll that pane's scrollback, without changing focus |
| wheel | over a sidebar list | move that panel's cursor |
