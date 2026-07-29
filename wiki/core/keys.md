---
type: concept
title: Keymap and help
description: Every binding declared once, the help generated from it, and the rules that keep a key meaning one thing at a time.
resource: internal/ui/keys.go
tags: [ui, keys, help, bindings]
timestamp: 2026-07-28T00:00:00Z
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
| `tab` / `shift+tab` | global | next / previous area |
| `1`–`5` | global | status, hosts, groups, sessions, command log |
| `b` / `B` / `s` | global | broadcast to the working set / the selection / one pane |
| `ctrl+alt+b` | global | broadcast to **every** host |
| `:` | global | send a command |
| `↑`/`k`, `↓`/`j` | sidebar | move |
| `enter` | sidebar | focus that host's pane |
| `space` | sidebar | toggle selection |
| `/` | sidebar | filter |
| `a` | sidebar | select every host |
| `i` | sidebar | invert the selection |
| `c` | sidebar | clear the selection |
| `u` | sidebar | select the hosts that are up |
| `d` | sidebar | select the hosts that are down |
| `w` | sidebar | save the working set or session |
| `[` / `]` | sidebar | previous / next chunk of hosts |
| `←`/`h`, `→`/`l`, `↑`/`k`, `↓`/`j` | panes | move between panes |
| `f` | panes | full-screen this pane |
| `r` | panes | reconnect this host |
| `x` | panes | close this session |
| `pgup`/`p`, `pgdn`/`n` | panes | page through the panes |

The fleet broadcast mode is `ctrl+alt+b` on purpose. It is the one mode that ignores the working
set, so it is not a single letter and not reachable by cycling through the others — see
[Broadcast scope](./broadcast-scope.md). A test asserts both the chord and that its help text
says `EVERY`.

## Help

`KeyMap.For(area)` returns a `help.KeyMap` describing what is live right now: the area's own
bindings plus the global ones. The short line along the bottom lists the handful a user needs
where they are; `?` opens the full overlay, which leads with the focused area and then lists the
others, with column titles from `Titles()`.

Styles come from the [theme](./theme.md) rather than the help bubble's defaults, so the overlay
matches the rest of the interface.
