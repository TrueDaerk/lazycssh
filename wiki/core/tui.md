---
type: concept
title: TUI shell
description: The root bubbletea model, the layout arithmetic, and the rules that keep a resize from taking the program down.
resource: internal/ui/app.go
tags: [ui, bubbletea, layout, focus]
timestamp: 2026-07-29T19:00:00Z
---

# TUI shell

`App` is the root bubbletea model. It owns the layout, the focus and the panel selection, and it
draws the frame every other view renders into: a lazygit-style stack of titled panel boxes on the
left, the pane grid on the right, the always-visible broadcast bar under them, a status bar along
the bottom, and the `?` popup composited over the frame.

Model mutation happens only in `Update`. Nothing in `internal/ui` dials a host; the transport
reports through messages — see [Session manager](./manager.md) — and the only bytes the UI
writes travel through narrow interfaces the program hands in (`PaneWriter` for the focused pane,
the sender for broadcast).

## Layout

`ComputeLayout(width, height)` is pure arithmetic, so it can be tested at every size without a
terminal. That matters more than it sounds: a layout that underflows to a negative width is how
a TUI panics on a resize.

```
╭ Status [1] ─╮┌────────────────────────────┐
│ …           ││                            │
╰─────────────╯│        pane grid           │
╭ Groups [2] ─╮│                            │
╰─────────────╯│                            │
╭ Sessions [3]╮│                            │
╰─────────────╯│                            │
 status bar                      key hints
```

Every sidebar panel is its own bordered box with its title and number in the top border line —
lazygit's look. lipgloss has no border-title support, so `titledBox` assembles the top line by
hand from the border's character set; the body supplies the other three sides.
`SidebarHeights(total, panels, selected)` is the pure height split: the selected panel takes
everything the collapsed boxes (3 rows each) leave, tighter sidebars collapse the others to bare
one-line titles, and tighter still leaves only the selected panel. Sums and signs are asserted at
hostile sizes.

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

- `alt+n` and `alt+p` move the window a whole page and put the pane focus on the first host
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

**A focused pane is a terminal.** Focusing the grid — `enter` on a host row, tab-cycling into
it — means typing: every keystroke is encoded and written to that one host, per key, enter not
required. `ctrl+c`, `tab` and `esc` belong to the remote shell. lazycssh keeps exactly two kinds
of keys for itself while typing: the reserved escape `ctrl+]`, which returns to the app level
(the Status panel, which answers where the keys go now), and the `alt`/`shift` pane-management
chords, combinations the encoder never produced, so intercepting them forwards nothing a user
could previously send. Where keystrokes go is always in the status bar: `TYPING web-01 — ctrl+]
leaves · alt=app`. Typing into a host that cannot take input says so rather than dropping keys.

The write path is `PaneWriter`, the narrowest slice of the manager — one host's `io.Writer` —
bypassing the broadcast scope entirely: typing into a pane can never fan out.

- `tab` / `shift+tab` at the app level walk the lazygit cycle: every sidebar panel in order,
  then the grid; once in the grid they are keystrokes for the host and `ctrl+]` is the way back,
- `1`–`5` at the app level select a panel **and** move focus to the sidebar,
- `alt+arrows` switch panes (they work while typing and from the app level alike), `alt+z`
  full-screens, `alt+n`/`alt+p` page, `alt+x` closes/removes, `alt+r` reconnects,
- key presses are dispatched by area, so a key means one thing at a time — see
  [Keymap and help](./keys.md). Commands exist only while no input pane is focused,
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

- `alt+n` and `alt+p` move the window a whole page and put the pane focus on the first host
  of the new page, so the pane that receives a keystroke is one the user can see,
- moving the pane focus off the edge of a page turns the page rather than focusing a pane that is
  not drawn,
- the page indicator (`page 2/5`) appears in the status bar only when there is more than one page,
- the page is clamped on every render: a terminal that shrinks produces more pages, and the page
  the user was on may stop existing.

## Pane output

Each pane carries a one-line header — pane number, host name, connection state and the last exit
code (`ok` / `exit 1`) — all read from live state at render time, so a change is on screen the
moment the redraw happens. When the width cannot hold everything the state gives up its space
first and the exit code second — a failure must outlive the state label — and the host name
truncates **from the left** (`…-1a-40.example.com`): in a fleet of near-identical names the
suffix is the distinguishing part.

### Failure visibility

The transport reports each command's exit status through a prompt hook — see
[SSH session lifecycle](./session.md). What the interface does with it:

- a pane whose last command exited non-zero gets a **danger-coloured border**, focused or not,
  and the header states `exit N` in text, because colour alone is not allowed to carry meaning;
- the pane headers mark failing hosts with `exit N` — and only those; `ok` on two hundred panes
  would bury the three that matter;
- the status bar counts them: `3 hosts failed`, in the failure style;
- `!` jumps the pane focus to the next failing host, from anywhere, wrapping around — the wrap
  is deliberate, unlike pane movement, because this is a search and a failure behind the cursor
  must be as reachable as one ahead;
- a shell that never ran the hook reports nothing, and the interface shows nothing rather than
  a made-up zero.

Below the header the pane renders its session's [scrollback](./scrollback.md), following the
tail: the newest output is what the user is watching for. Rendering is a pure
function of the buffer's current content — `SessionOutputMsg` carries no bytes, it only asks for
a redraw, so a coalesced or dropped message costs nothing.

The buffer stores escape sequences verbatim; the renderer decides what they may do:

- **SGR sequences pass through.** `ls --color` looks like `ls --color`.
- **Everything else is neutralized.** Cursor movement, screen clearing, OSC titles, charset
  selection and stray control bytes are removed before the line is drawn — a pane renders
  scrollback text, not a terminal, and one host emitting `clear` must not corrupt the layout
  around it. Full VT emulation is a separate idea issue, deliberately.
- A line that still carries a colour is closed with a reset, so an unbalanced SGR from one host
  cannot bleed into the border or the neighbouring pane.
- Tabs expand to 8-column stops; an escape sequence the remote never finished drops the tail of
  its line rather than being half-rendered.

Long lines hard-wrap at the pane width with the colours kept intact across the break, and wide
characters are counted by their display width. When the buffer has evicted lines to stay within
its bound, a `~ N lines dropped ~` marker sits where the missing output was, so truncated
scrollback is visible rather than silent.

### Scrollback navigation

A focused pane scrolls back through its buffer: `shift+pgup` / `shift+pgdn` move half a pane at
a time — the terminal-emulator convention, chosen because plain keys belong to the remote shell —
`shift+home` jumps to the oldest retained output — where the dropped marker is — and `shift+end`
returns to the tail and resumes following it.

The offset is anchored at the **bottom** and counted in wrapped lines, so new output slides the
window rather than the reader's position in it; a top-anchored offset would drift every time the
bounded buffer drops a line. Scrolling is a render-time window into a snapshot: the buffer keeps
receiving at full speed, which is the issue's acceptance criterion. A pane that is not following
its tail says `scrollback +N` in the status bar in the warning style, because fresh output
landing behind a frozen window must not look like a quiet host.

### Search

`alt+/` opens a search prompt that owns the keyboard while it is open. `enter` commits the term
and scrolls the focused pane to the **newest** match — the reader is almost always hunting the
error that just happened; `alt+[` and `alt+]` walk to older and newer matches without wrapping.
Matching lines are drawn in the match style, their own colours dropped: a highlight fighting the
remote's colours would lose. `alt+c` clears the term; a bare `esc` belongs to the remote shell.

One term is shared by every pane, because "which of my hosts printed this" is a question about
the run. The cross-pane form is the command line's `/find <text>`: it sets the shared term and
reports directly — `"disk full" found on 2/8 hosts: web-03, web-07` — searching each host's raw
scrollback, so the answer does not depend on pane widths or pages. Like `/select`, it is a meta
command: nothing reaches a remote shell. Matching is case-insensitive substring.

## Focus survives the host list changing

`HostsChangedMsg` replaces the host list — a session merged in, a host closed, panes paged. The
focused host is preserved **by identity**, not by position: if the machine is still in the run it
keeps focus at its new index, and only when it is gone does the focus clamp to the nearest pane
that exists. A list that shifts under the cursor must never silently move the user onto a
different machine.

The `?` popup is **composited over** the frame with lipgloss layers rather than replacing it, so
the fleet stays visible underneath — the way lazygit's menus behave. While it is open it is the
only thing listening: the key that closes it does not also act. A user reading the help is not
also driving the panes.

## Panels

The sidebar stacks the five panels as titled boxes; every title is always on screen and only the
**selected** panel shows its body. Five open panels on an 80-column terminal would show none of
them usefully; five visible titles cost three rows each and keep the map of the interface on
screen.

### [1] Status

The panel that answers "what happens if I type": first a literal `keys go to:` line — the
focused host, the broadcast targets, or lazycssh itself — then the session name, the broadcast
scope with its live target count, the working set, hosts up/total, and every flag that weakens a
default.

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

### Connecting and selecting without a Hosts panel

The pane grid is the host list: names, states and `exit N` markers live in the pane headers,
and `!` jumps to the next failure. What a dedicated Hosts panel used to add lives elsewhere:

- **connect** — `n` works from anywhere: it selects the Status panel and opens a free-text
  prompt accepting any host pattern — `host`, `user@host:port`, brace expansion like
  `web-{01..04}`. While it is open, the concrete aliases of `~/.ssh/config` that are not in the
  run yet are listed beneath it, filtered by what has been typed; `tab` completes the first
  match, `enter` connects, `esc` abandons. The prompt owns the keyboard while open: a pattern
  containing `b` must not switch the broadcast mode (`ctrl+q` still quits). A connect that
  fails to resolve shows its error in the Status panel until the fleet next changes. The UI
  cannot dial: it emits `HostConnectMsg`; the program resolves, skips hosts already in the run
  (double-enter must not mint `host-2`), and adds the rest via `Manager.Add`,
- **selection** — `alt+space` toggles the focused pane's host, from the grid and the app level,
  like the other pane chords; `a`/`i`/`c`/`u`/`d` (select all / invert / clear / up hosts /
  down hosts) work at the app level, and `/select` / `/deselect` cover the pattern cases.
  Selection lives in the [broadcast router](./broadcast-scope.md) and is keyed by **host
  identifier**, so it survives a reconnect and a page turn — the pane moves, the host keeps its
  name.

An argumentless start opens on the Status panel with **no** input focused: the empty grid names
the options — `n` to connect, the Sessions panel, the CLI — and which of them comes first is the
user's call, not the program's.

### [2] Groups / Views

The working sets defined for this run — see [Working sets](./working-sets.md) — plus where the
window sits.

Rows: the built-in **all hosts** entry first (undoing a narrowing has to be one keystroke away),
then every named set with its definition, and — when the active set is neither — the **unnamed**
ad hoc set the user is actually in, so the panel can never hide where they are.

- the active row is marked with `▸` as well as a style, so it survives a terminal without colour,
- `enter` makes that row the working set: the one keystroke from "which twenty am I working on"
  to "these twenty",
- `[` and `]` page the **working set** by its own chunk size. That is a different thing from
  `alt+p`/`alt+n`, which page the pane window; the panel shows both lines for exactly that reason.

### [3] Sessions

The saved [session files](./session-files.md), each with its host count and description.

- `enter` launches the session under the cursor, `space` merges it into the current run,
- `w` saves the current run: a name prompt that owns the keyboard, pre-filled with the session
  the run was started from,
- an existing name is **never** replaced silently: the first `enter` turns into `overwrite "x"?
  y/n`, and nothing is written until it is answered,
- the run's **patterns** are written, not the hostnames they expanded to,
- one unreadable file becomes one `(unreadable)` row rather than an empty panel — hiding the
  other sessions would make a typo look like data loss.

The panel does not dial. `enter` emits a `SessionLaunchMsg`; the layer that owns the transport
acts on it, which keeps `internal/ui` unable to open a connection.

### [4] Command log

Every command sent this run, newest last, each with its target count and mode — see
[Command log](./command-log.md). `enter` sends an entry again, to the **current** target set.

## The broadcast bar

An input line under the grid, always on screen, focused with `5`, tab-cycled after the last
panel, left with `ctrl+]`. While it has the keyboard it is a terminal for the whole target set:
every keystroke is encoded and fanned out live through the broadcast scope — `ctrl+c`
interrupts every target, `tab` completes on every target, and each pane shows its own host's
echo, so a divergent completion is visible as N panes disagreeing rather than as one garbled
line.

The bar keeps a local echo line — printable text appends, backspace trims — as a reminder of
what was typed; the truth is on the hosts. `enter` sends a carriage return and records the
assembled non-empty line in the [command log](./command-log.md) once; the individual keystrokes
are never recorded, because this is where a password may be typed. The title always carries the
live target count (`Broadcast [5] → 7 hosts`), and the status bar says `BROADCASTING → 7 hosts`
in the warning style while the bar has the keyboard. On short terminals the bar degrades to a
bare line and then disappears before the grid gives up a row.

The pane-management chords (`alt+arrows` and friends) keep working from the bar.

## Mouse

The mouse does what it looks like it should. Hit-testing is pure arithmetic over the same
`Layout`, `SidebarHeights` and `Grid.Cells` rects the renderer draws from (`internal/ui/
hittest.go`), so a click resolves without a terminal and the tests stay table-driven.

- **Click** a pane and it takes the keyboard — clicking a terminal is focusing it, so typing
  starts there. Click the `[x]` at the right of a pane header to close a live host or remove a
  dead one, exactly like `alt+x`. Click a sidebar box to select its panel, a row to move that
  panel's cursor there, the broadcast bar to give it the keyboard.
- **Wheel** over a pane scrolls **that** pane's scrollback a few lines per notch — the pane
  under the pointer, not the focused one, and without stealing focus. Over a sidebar list it
  moves the cursor.

Mouse reporting is cell-motion: clicks, releases and the wheel, without bare-movement chatter.
Full screen is special-cased — the whole main area is the focused pane.

## The command line

`:` opens a prompt for one command sent to the whole active broadcast set.

- the prompt carries the scope itself — `:systemctl restart nginx → BROADCAST all (7/8 up)` — so
  the number of machines about to receive it is on screen at the moment of typing, which is why
  there is no confirmation dialog,
- while it is open **every** key belongs to it: a command containing `b` does not switch the
  broadcast mode, a `:` does not open a second prompt, and `ctrl+c` while editing does not reach
  forty machines,
- `enter` sends, `esc` abandons, `↑`/`↓` walk the history of this run (repeats are not stored
  twice, and walking past the newest entry returns to an empty line),
- after sending, the status bar reports against the **scope**: `sent to 2/3 hosts (1 did not
  receive it)`.

Typing a command and resending one from the [Command log](./command-log.md) take the same path,
so a resend goes to the set that is active *now* and leaves the same audit entry.

## Key encoding

What reaches a remote shell is the one thing in this program a user cannot inspect, so the
encoding (`keystrokeBytes` in `internal/ui/keystroke.go`) is explicit and table-tested:
`enter` → `\r`, `tab` → `\t`, `backspace` → `0x7f`, arrows → `ESC [ A`–`D`,
`ctrl+<letter>` → `0x01`–`0x1a`. `ctrl+]` is kept from the old passthrough mode — the telnet
escape, because a user who is stuck needs one sequence that always means "give me my keyboard
back". The passthrough mode itself is gone: typing into a focused pane replaces
passthrough-to-one-host and the broadcast bar replaces passthrough-to-all.

Typed keys are never written to the [command log](./command-log.md). This is where a password is
typed, and the audit trail is for commands.

## Messages

| Message | Effect |
|---------|--------|
| `tea.WindowSizeMsg` | recompute the layout and resize the help |
| `tea.BackgroundColorMsg` | rebuild the theme for a light or dark terminal |
| `tea.KeyPressMsg` | dispatch by focus |
| `FleetUpdatedMsg` | redraw; the panels read the fleet's live state themselves |
| `SessionOutputMsg` | redraw; the pane reads the live scrollback itself |
| `HostsChangedMsg` | replace the host list, keeping the focused host |
| `SessionsChangedMsg` | re-read the session directory |
| `SessionLaunchMsg` | emitted, not handled: the program opens or merges a saved session |
| `HostConnectMsg` | emitted, not handled: the program resolves and connects the asked-for patterns |
| `ConnectErrorMsg` | a connect request's resolve error, shown in the Status panel |
| `ReconnectHostMsg` | emitted, not handled: `r` in the grid asks the program to reconnect the focused host |
| `CloseHostMsg` | emitted, not handled: `x` on a live host asks the program to close its session |
| `RemoveHostMsg` | emitted, not handled: `x` on a dead host asks the program to drop its pane from the run |
| `CommandResendMsg` | resend a logged command to the current broadcast set |
| `CommandSentMsg` | emitted after a send, carrying the delivery report |

`Init` returns `tea.RequestBackgroundColor`, which is what makes the palette match the terminal
rather than guessing.

## Testing a TUI

There is no terminal in the tests. `Update` is driven with synthetic messages and `View().Content`
is asserted against — with the ANSI styling stripped, so the assertions are about what the user
reads rather than about how it was coloured.
