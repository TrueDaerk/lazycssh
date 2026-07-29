---
type: concept
title: Keymap and help
description: Every binding declared once, the help generated from it, and the rules that keep a key meaning one thing at a time.
resource: internal/ui/keys.go
tags: [ui, keys, help, bindings]
timestamp: 2026-07-29T12:00:00Z
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
| `AreaGrid` | the host panes on the right |

The sidebar and the grid may reuse a key — they are never focused at the same time — but a
global binding may not collide with either, because the two are always live together. Both rules
are tests, not conventions.

## Bindings

| Key | Area | Action |
|-----|------|--------|
| `?` | global | help overlay |
| `ctrl+q` | global | quit |
| `tab` / `shift+tab` | global | next / previous stop in the cycle: each sidebar panel, then the grid |
| `1`–`5` | global | status, hosts, groups, sessions, command log |
| `b` / `B` / `s` | global | broadcast to the working set / the selection / one pane |
| `ctrl+alt+b` | global | broadcast to **every** host |
| `:` | global | send a command |
| `ctrl+]` | global | raw keystrokes to the hosts, and back again |
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
| `←`/`h`, `→`/`l`, `↑`/`k`, `↓`/`j` | panes | move between panes |
| `f` | panes | full-screen this pane |
| `r` | panes | reconnect this host |
| `x` | panes | close this session |
| `pgup`/`p`, `pgdn`/`n` | panes | page through the panes |
| `ctrl+u` / `ctrl+d` | panes | scroll the focused pane back / forward |
| `g` / `G` | panes | oldest retained output / back to the tail |
| `/` | panes | search the scrollback |
| `[` / `]` | panes | older / newer match |
| `esc` | panes | clear the search |

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
