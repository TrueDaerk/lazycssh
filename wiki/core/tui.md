---
type: concept
title: TUI shell
description: The root bubbletea model, the layout arithmetic, and the rules that keep a resize from taking the program down.
resource: internal/ui/app.go
tags: [ui, bubbletea, layout, focus]
timestamp: 2026-07-28T00:00:00Z
---

# TUI shell

`App` is the root bubbletea model. It owns the layout, the focus and the panel selection, and it
draws the frame every other view renders into: a numbered sidebar on the left, the pane grid on
the right, a status bar along the bottom, and the `?` overlay on top.

Model mutation happens only in `Update`. Nothing in `internal/ui` dials, reads or writes a host;
the transport reports through messages — see [Session manager](./manager.md).

## Layout

`ComputeLayout(width, height)` is pure arithmetic, so it can be tested at every size without a
terminal. That matters more than it sounds: a layout that underflows to a negative width is how
a TUI panics on a resize.

```
┌─────────────┬──────────────────────────────┐
│ [1] Status  │                              │
│ [2] Hosts   │        pane grid             │
│ [3] Groups  │                              │
│ [4] Sessions│                              │
│ [5] Log     │                              │
├─────────────┴──────────────────────────────┤
│ status bar                                 │
└────────────────────────────────────────────┘
```

Rules:

- the **status bar is never dropped**. The broadcast target count is the one thing the user must
  always be able to see;
- the sidebar is a quarter of the width, clamped to 20–34 columns, and it disappears entirely
  below `SidebarMinWidth + MainMinWidth`. It gives up its share before the grid does: output
  squeezed to nothing is worse than no panel list;
- below 24×4 the interface renders a single "terminal too small" line — clipped to the actual
  width, so even the apology cannot wrap into the scrollback;
- every rect is non-negative at every size, including sizes no terminal reports. A test sweeps
  every width from −5 to 200 and every height from −5 to 60 and asserts it.

## Focus

Focus is one explicit piece of model state and it is always visible: the focused frame is drawn
with the focused border from the [theme](./theme.md), which differs in thickness as well as
colour so it survives a terminal without colour.

- `tab` / `shift+tab` move between the sidebar and the grid,
- `1`–`5` select a panel **and** move focus to the sidebar, because pressing a panel number and
  landing somewhere else is a surprise,
- inside the sidebar, `↑`/`k` and `↓`/`j` move the panel selection; inside the grid the same keys
  move the pane focus, and `enter` from the sidebar hands focus to the grid,
- key presses are dispatched by area, so a key means one thing at a time — see
  [Keymap and help](./keys.md). The bindings of the area that does not have focus are not
  consulted at all,
- nothing wraps. Stepping off the last pane onto the first is how a user ends up typing into the
  machine at the other end of the fleet.

### Focus survives the host list changing

`HostsChangedMsg` replaces the host list — a session merged in, a host closed, panes paged. The
focused host is preserved **by identity**, not by position: if the machine is still in the run it
keeps focus at its new index, and only when it is gone does the focus clamp to the nearest pane
that exists. A list that shifts under the cursor must never silently move the user onto a
different machine.

While the `?` overlay is open it is the only thing listening: the key that closes it does not
also act. A user reading the help is not also driving the panes.

## Messages

| Message | Effect |
|---------|--------|
| `tea.WindowSizeMsg` | recompute the layout and resize the help |
| `tea.BackgroundColorMsg` | rebuild the theme for a light or dark terminal |
| `tea.KeyPressMsg` | dispatch by focus |

`Init` returns `tea.RequestBackgroundColor`, which is what makes the palette match the terminal
rather than guessing.

## Testing a TUI

There is no terminal in the tests. `Update` is driven with synthetic messages and `View().Content`
is asserted against — with the ANSI styling stripped, so the assertions are about what the user
reads rather than about how it was coloured.
