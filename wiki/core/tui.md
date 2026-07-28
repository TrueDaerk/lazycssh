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

## Pane grid

`TileGrid(area, count)` is the tiling: the squarest arrangement that holds the hosts, bounded by
how many minimum-size panes the area actually fits. It is a pure function, so two renders of the
same state cannot disagree about where a pane is.

| Hosts | Shape |
|-------|-------|
| 1 | 1×1 |
| 2 | 2×1 |
| 3, 4 | 2×2 |
| 6 | 3×2 |
| 9 | 3×3 |
| 12 | 4×3 |
| 20 | 5×4 |

A pane is never smaller than 24×6 including its border. When the hosts do not fit at that size
the grid **pages** rather than shrinking further — four readable panes and a page indicator beat
twelve unreadable ones. `Grid.Pages`, `Grid.Page(i)` and `Grid.Cell(i)` say how many pages there
are and where a host sits on its own.

An empty slot on the last page — three hosts in a 2×2 — is drawn as an empty frame rather than
being reflowed, so the panes stay the size they were as hosts come and go.

`f` full-screens the focused pane and `f` again returns to the grid. The issue proposed `1`/`2`/`3`
for this, but the epic gives the number keys to the sidebar panels; a key cannot mean both, so
full screen took the letter.

## The window

The **window** is which hosts are on screen. It is not the [working set](./working-sets.md),
which is which hosts a command is about. Paging the window never changes who receives a
keystroke — that is the entire reason the two are separate concepts.

- `pgdn`/`n` and `pgup`/`p` move the window a whole page and put the pane focus on the first host
  of the new page, so the pane that receives a keystroke is one the user can see,
- moving the pane focus off the edge of a page turns the page rather than focusing a pane that is
  not drawn,
- the page indicator (`page 2/5`) appears in the status bar only when there is more than one page,
- the page is clamped on every render: a terminal that shrinks produces more pages, and the page
  the user was on may stop existing.

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

### Pane grid

`TileGrid(area, count)` is the tiling: the squarest arrangement that holds the hosts, bounded by
how many minimum-size panes the area actually fits. It is a pure function, so two renders of the
same state cannot disagree about where a pane is.

| Hosts | Shape |
|-------|-------|
| 1 | 1×1 |
| 2 | 2×1 |
| 3, 4 | 2×2 |
| 6 | 3×2 |
| 9 | 3×3 |
| 12 | 4×3 |
| 20 | 5×4 |

A pane is never smaller than 24×6 including its border. When the hosts do not fit at that size
the grid **pages** rather than shrinking further — four readable panes and a page indicator beat
twelve unreadable ones. `Grid.Pages`, `Grid.Page(i)` and `Grid.Cell(i)` say how many pages there
are and where a host sits on its own.

An empty slot on the last page — three hosts in a 2×2 — is drawn as an empty frame rather than
being reflowed, so the panes stay the size they were as hosts come and go.

`f` full-screens the focused pane and `f` again returns to the grid. The issue proposed `1`/`2`/`3`
for this, but the epic gives the number keys to the sidebar panels; a key cannot mean both, so
full screen took the letter.

## The window

The **window** is which hosts are on screen. It is not the [working set](./working-sets.md),
which is which hosts a command is about. Paging the window never changes who receives a
keystroke — that is the entire reason the two are separate concepts.

- `pgdn`/`n` and `pgup`/`p` move the window a whole page and put the pane focus on the first host
  of the new page, so the pane that receives a keystroke is one the user can see,
- moving the pane focus off the edge of a page turns the page rather than focusing a pane that is
  not drawn,
- the page indicator (`page 2/5`) appears in the status bar only when there is more than one page,
- the page is clamped on every render: a terminal that shrinks produces more pages, and the page
  the user was on may stop existing.

## Focus survives the host list changing

`HostsChangedMsg` replaces the host list — a session merged in, a host closed, panes paged. The
focused host is preserved **by identity**, not by position: if the machine is still in the run it
keeps focus at its new index, and only when it is gone does the focus clamp to the nearest pane
that exists. A list that shifts under the cursor must never silently move the user onto a
different machine.

While the `?` overlay is open it is the only thing listening: the key that closes it does not
also act. A user reading the help is not also driving the panes.

## Panels

The sidebar lists the five panels; only the **selected** one opens. Five open panels on an
80-column terminal would show none of them usefully.

### [1] Status

The panel that answers "what happens if I type": the session name, the broadcast scope with its
live target count, the working set, hosts up/total, and every flag that weakens a default.

Every number is read from live state **at render time**. A cached target count is a lie waiting
to be told: the fleet changes under the user, and the one moment the count matters is the moment
it changed. `FleetUpdatedMsg` carries no payload for the same reason — it asks for a redraw, and
the redraw reads the truth. That also survives the transport dropping event hints when the UI is
behind, which it does by design (see [Session manager](./manager.md)).

Flags — `HOST KEYS UNVERIFIED`, `SESSION LOGGING ON`, `BROADCASTING TO EVERY HOST` — are rendered
in the warning style **and** repeated on the status bar, so switching panels or scrolling cannot
hide a weakened default. Without a router the panel says `BROADCAST (not started)` rather than
inventing a count.

Panel bodies wrap rather than truncate: a line that silently loses its tail is worse than one
that takes a second row, because the tail is where the warnings are.

### [2] Hosts

Every host of the run: pane number, selection marker, name, connection state.

- `↑`/`k` and `↓`/`j` move the host cursor; running off either end moves to the neighbouring
  panel, so the panel list stays reachable with the same keys,
- `space` toggles selection. Selection lives in the [broadcast router](./broadcast-scope.md) and
  is keyed by **host identifier**, so it survives a reconnect, a filter and a page turn — the pane
  moves, the host keeps its name,
- `enter` focuses that host's pane and hands focus to the grid,
- `/` opens the filter, which owns the keyboard while it is open: a host called `x` must be
  typeable without closing a pane. `enter` keeps the filter, `esc` drops it.

Filtering never renumbers panes: a row shows the host's position in the **full** list, so
`3 web-02` stays pane 3 whatever the filter hides.

Only the visible rows are rendered, with a `+N more` marker for the rest, so a redraw costs the
size of the panel rather than the size of the fleet — two hundred hosts included.

Exit-code tracking is not here yet; it is [#41](https://github.com/TrueDaerk/lazycssh/issues/41).

## Messages

| Message | Effect |
|---------|--------|
| `tea.WindowSizeMsg` | recompute the layout and resize the help |
| `tea.BackgroundColorMsg` | rebuild the theme for a light or dark terminal |
| `tea.KeyPressMsg` | dispatch by focus |
| `FleetUpdatedMsg` | redraw; the panels read the fleet's live state themselves |
| `HostsChangedMsg` | replace the host list, keeping the focused host |

`Init` returns `tea.RequestBackgroundColor`, which is what makes the palette match the terminal
rather than guessing.

## Testing a TUI

There is no terminal in the tests. `Update` is driven with synthetic messages and `View().Content`
is asserted against — with the ANSI styling stripped, so the assertions are about what the user
reads rather than about how it was coloured.
