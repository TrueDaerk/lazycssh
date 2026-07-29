---
type: concept
title: Keymap and help
description: Every binding declared once, the help generated from it, and the rules that keep a key meaning one thing at a time.
resource: internal/ui/keys.go
tags: [ui, keys, help, bindings]
timestamp: 2026-07-29T23:00:00Z
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
| `AreaBroadcast` | the broadcast bar under the grid — a terminal for the whole target set; only `ctrl+]` and the pane chords are kept |
| `AreaGrid` | the host panes on the right — a focused pane is a terminal, so its bindings are all `alt`/`shift` chords plus the reserved `ctrl+]`; every plain key is forwarded to the host (a test enforces the chord rule) |

The sidebar and the grid may reuse a key — they are never focused at the same time — but a
global binding may not collide with either, because the two are always live together. Both rules
are tests, not conventions. The one sanctioned exception is a **declared panel shadow**: the
Groups panel keeps `n` and `d` for itself, lazygit style, resolved by routing order before the
global bindings are consulted; the tests carry the explicit allowlist, so an undeclared
duplicate still fails.

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
| `ctrl+a` | global (app level) | show only the connected hosts; broadcast follows the visible set |
| `ctrl+s` | global (app level) | split the grid into chunks of N panes (prompt; empty or 0 clears) |
| `ctrl+→` / `ctrl+←` | global (app level) | next / previous split chunk |
| `a` | global (app level) | select every host |
| `i` | global (app level) | invert the selection |
| `c` | global (app level) | clear the selection |
| `u` | global (app level) | select the hosts that are up |
| `d` | global (app level) | select the hosts that are down |
| `↑`/`k`, `↓`/`j` | sidebar | move |
| `enter` / `space` | sidebar | choose the row: open a group, foreground a session, resend a log entry |
| `w` | sidebar | save the run as a group |
| `n` | Groups panel | new group (shadows the global connect while the panel has focus) |
| `d` | Groups panel | delete the group under the cursor, after `y/n` (shadows the global select-down) |
| `[` / `]` | sidebar | previous / next chunk of hosts |
| any plain key | panes | **forwarded to the focused host** — letters, enter, tab, esc, ctrl+c, arrows, all of it |
| `ctrl+]` | panes | stop typing: back to the app level, on the Status panel |
| `alt+space` | panes (and app level) | toggle the focused pane's host in the selection |
| `alt+←`/`alt+→`/`alt+↑`/`alt+↓` | panes (and app level) | move between panes |
| `alt+z` | panes (and app level) | full-screen this pane |
| `alt+r` | panes (and app level) | reconnect this host |
| `alt+x` | panes (and app level) | close this host; on a dead host, remove its pane |
| `alt+p` / `alt+n` | panes (and app level) | page through the panes |
| `shift+pgup` / `shift+pgdn` | panes (and app level) | scroll the focused pane back / forward |
| `shift+home` / `shift+end` | panes (and app level) | oldest retained output / back to the tail |
| `alt+/` | panes (and app level) | search the scrollback |
| `alt+[` / `alt+]` | panes (and app level) | older / newer match |
| `alt+c` | panes (and app level) | clear the search |

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

## Mouse

| Action | Where | Effect |
|--------|-------|--------|
| left click | pane body | focus the pane and start typing into its host |
| left click | `[x]` in a pane header | close a live host; remove a dead one |
| left click | sidebar box / row | select the panel, move its cursor to the row |
| left click | broadcast bar | give the bar the keyboard |
| wheel | over a pane | scroll that pane's scrollback, without changing focus |
| wheel | over a sidebar list | move that panel's cursor |
