---
type: concept
title: Keymap and help
description: Every binding declared once, the help generated from it, and the rules that keep a key meaning one thing at a time.
resource: internal/ui/keys.go
tags: [ui, keys, help, bindings]
timestamp: 2026-07-29T15:00:00Z
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
are tests, not conventions.

## Bindings

| Key | Area | Action |
|-----|------|--------|
| `?` | global | help overlay |
| `ctrl+q` | global | quit |
| `tab` / `shift+tab` | global (app level) | next / previous stop in the cycle: each sidebar panel, then the grid; forwarded while typing |
| `1`–`5` | global (app level) | status, hosts, groups, sessions, command log |
| `6` | global (app level) | focus the broadcast bar |
| `b` / `B` / `s` | global | broadcast to the working set / the selection / one pane |
| `ctrl+alt+b` | global | broadcast to **every** host |
| `:` | global | send a command |
| `!` | global | jump to the next host whose last command failed |
| `↑`/`k`, `↓`/`j` | sidebar | move |
| `enter` | sidebar | focus that host's pane |
| `space` | sidebar | toggle selection, or mark a connect candidate |
| `/` | sidebar | filter |
| `a` | sidebar | select every host |
| `i` | sidebar | invert the selection |
| `c` | sidebar | clear the selection |
| `u` | sidebar | select the hosts that are up |
| `d` | sidebar | select the hosts that are down |
| `w` | sidebar | save the working set or session |
| `[` / `]` | sidebar | previous / next chunk of hosts |
| `n` | sidebar | connect a new host: opens the free-text pattern prompt |
| `x` | sidebar | close the host under the cursor; on a dead host, remove its pane |
| `r` | sidebar | reconnect the host under the cursor |
| any plain key | panes | **forwarded to the focused host** — letters, enter, tab, esc, ctrl+c, arrows, all of it |
| `ctrl+]` | panes | stop typing: back to the app level, cursor on the host just typed to |
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
