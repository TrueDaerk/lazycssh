---
type: concept
title: TUI shell
description: The root bubbletea model, the layout arithmetic, and the rules that keep a resize from taking the program down.
resource: internal/ui/app.go
tags: [ui, bubbletea, layout, focus]
timestamp: 2026-07-31T19:00:00Z
---

# TUI shell

`App` is the root bubbletea model. It owns the layout, the focus and the panel selection, and it
draws the frame every other view renders into: a lazygit-style stack of titled panel boxes on the
left, the pane grid on the right, the always-visible broadcast bar under the grid — beside the
sidebar, which keeps its full height — a status bar along the bottom, and the `?` popup
composited over the frame.

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
╭ Sessions [3]╮└────────────────────────────┘
│ …           │╭ Broadcast [5] → 7 hosts ──╮
╰─────────────╯╰───────────────────────────╯
 status bar                      key hints
```

The broadcast bar shares its rows with the sidebar instead of stretching under it (issue #164):
the panel column runs down to the status bar, and the bar sits under the grid only. On a
terminal too narrow for a sidebar the bar spans the full width.

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

A pane never shows its host less than a 45×16 terminal (issue #139) — a 47×19 cell once the
border and the header line are counted in, and the remote PTY is sized to the same content, so
remote line wrapping always matches what is rendered. The guideline value is tuned in one place
(`MinPaneContentWidth`/`MinPaneContentHeight` in `internal/ui/grid.go`). When the hosts do not
fit at that size the grid **pages** rather than shrinking further — four readable panes and a
page indicator beat twelve unreadable ones. `Grid.Pages`, `Grid.Page(i)` and `Grid.Cell(i)` say how many pages there
are and where a host sits on its own.

An empty slot on the last page — three hosts in a 2×2 — is drawn as an empty frame rather than
being reflowed, so the panes stay the size they were as hosts come and go.

The same rule covers departures entirely: **a host leaving the run does not reflow the grid**.
Its slot becomes a **hole** — an empty frame exactly where the pane was, held in the session's
host list as a `""` marker (issue #169) — so a host closing in the middle of the grid moves
nothing: the survivors keep their positions and their numbers. A hole is a grid position, not a
host: pane movement steps over it, a click on it selects nothing, focus never rests on it, and
the broadcast set and every host count exclude it (`nonHoles`). `ctrl+r` closes the holes and
re-tiles on request (and resizes the PTYs). Growth is immediate — a new pane has to appear
somewhere, appended after the existing slots — and an explicit view change (session switch,
`ctrl+a`, `ctrl+s`) compacts and tiles for the new view rather than keeping a museum of the old
one. A terminal resize reflows the cells but keeps the slots as they are: the user changed the
window, not the run. While typing, `ctrl+r` belongs to the host (readline reverse-search).

`f` full-screens the focused pane and `f` again returns to the grid. The issue proposed `1`/`2`/`3`
for this, but the epic gives the number keys to the sidebar panels; a key cannot mean both, so
full screen took the letter.

## The window

The **window** is which hosts are on screen. It is not the [working set](./working-sets.md),
which is which hosts a command is about. Paging the window never changes who receives a
keystroke — that is the entire reason the two are separate concepts.

- `ctrl+→` / `ctrl+←` are the **single navigator** for "the next screenful" (issue #147): they
  move the window a whole page — and, at a chunk boundary of an active split, the chunk —
  wrapping at both ends. Page-major: forward steps every page of the current chunk, then the
  next chunk at its first page; backward mirrors that and lands on the previous chunk's last
  page. The pane focus follows onto the new screenful, so the pane that receives a keystroke is
  one the user can see. They are app-level commands: while a pane or the broadcast bar has the
  keyboard, ctrl+arrows stay keystrokes for the hosts (readline word movement),
- moving the pane focus off the edge of a page turns the page rather than focusing a pane that is
  not drawn,
- the page indicator (`page 2/5`) appears in the status bar only when there is more than one page,
- whenever the grid is showing a *part* — more pages, more split chunks, more open sessions — an
  **overflow footer** takes the grid's bottom line and says so in place: `+12 more hosts — ctrl+→
  · page 1/3 · 2 more sessions — [3]`. Each part names its own navigation (page and chunk
  `ctrl+→`, session panel `3`).
  The visible panes must never read as the whole run; a muted counter in the status bar alone is
  too easy to read past. Full screen skips the footer — `alt+z` is an explicit zoom with its own
  way back,
- the page is clamped on every render: a terminal that shrinks produces more pages, and the page
  the user was on may stop existing.

### Connected-only filter

`ctrl+a` narrows the grid to the hosts that can take input right now, and — unlike paging — it
narrows the broadcast with it: the visible set is pushed into the router's
[visibility limit](./broadcast-scope.md), so `all`/`selected` reach only what is on screen.
The filter is a view, not a removal: a host that reconnects reappears without a keypress — its
state change is a fleet event, the event refreshes the model's **fleet snapshot** inside
`Update` (`App.snapshotFleet`, issue #136), and the redraw recomputes the visible list from it.
Render helpers never read live session state: `hostIDs()` is a pure function of model fields,
so every computation inside one frame agrees. Two computations inside one render used to
disagree — a mass disconnect under the filter shrank the list between a bounds check and the
index it guarded, and `renderPane` panicked (issue #135); the snapshot removes the class of bug,
and a spy-fleet test plus a `-race` state-flip hammer pin it down. While it is on, the status bar carries `CONNECTED HOSTS ONLY`; a filter that hides
every pane renders `no connected hosts` rather than an empty run. While typing into a pane,
`ctrl+a` stays a keystroke for the hosts — readline start-of-line. In the broadcast bar it is
the csshx-style escape prefix instead: the literal is `ctrl+a a`, and the toggle is reachable
from the bar's view mode, where `ctrl+a` is a command again — see the broadcast bar section.

### Split

`ctrl+s` asks for a number and splits the visible hosts into consecutive chunks of that size:
ten hosts split by five shows the first five terminals. `ctrl+→`/`ctrl+←` page through the
chunk and then show the next or previous chunk, wrapping at the ends (see The window above). Broadcast follows the visible chunk
through the same [visibility limit](./broadcast-scope.md) the connected-only filter uses, and
the status bar carries `SPLIT 1/2 (5 hosts)` in the warning style for as long as the split
narrows anything. An empty prompt or `0` clears the split, `esc` keeps it; the split composes
with `ctrl+a` — chunks are cut from the filtered list — and a chunk whose hosts left the run
clamps to the last one instead of rendering an empty grid. The typing exception applies here
too: in a pane, `ctrl+s` is flow control for the host.

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
  full-screens, `alt+x` closes/removes, `alt+r` reconnects; paging is `ctrl+→`/`ctrl+←` at the
  app level,
- key presses are dispatched by area, so a key means one thing at a time — see
  [Keymap and help](./keys.md). Commands exist only while no input pane is focused,
- pane focus never wraps. Stepping off the last pane onto the first is how a user ends up typing
  into the machine at the other end of the fleet. The window navigator (`ctrl+→`/`ctrl+←`) does
  wrap — it changes what is on screen, never who receives a keystroke, and the page/SPLIT
  indicators say where it landed.

## Pane output

Each pane carries a one-line header — pane number, host name, connection state and the last exit
code (`ok` / `exit 1`) — all read from the model's fleet snapshot, which the fleet event that
changed them refreshed, so a change is on screen the moment the redraw happens. The scrollback
body is the one deliberate live read left in the render path: the buffer is internally
synchronized and `Lines()` returns a copy, and snapshotting whole scrollbacks per output event
would copy far more than a redraw reads. When the width cannot hold everything the state gives up its space
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
  a made-up zero;
- a pane whose *connection* failed says why (issue #167): the session's error — DNS, refused,
  auth, host key — renders in the failure style at the bottom of the body, wrapped to the pane
  and capped at half its height so the output that led up to the failure stays visible. The
  text comes from the fleet snapshot (`hostState.errText`), never from live session state.

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

### Copy

Bubbletea owns the mouse, so the terminal's native selection cannot reach the pane content
(issue #134). Copying is keyboard-first instead, per the interaction model: `alt+y` puts the
focused pane's **visible text** into the system clipboard — scroll first to aim the window —
and `alt+d` takes the **whole retained scrollback**. Both go out over **OSC 52**, so they reach
the local clipboard even when lazycssh itself runs over SSH; a terminal without OSC 52 support
ignores the sequence, and the status line reports what was attempted either way. Clipboard text
is plain: ANSI styling is stripped and clear markers are excluded, because a paste target wants
the ID or the error message, not the colours around it. Both chords appear in the `?` overlay,
generated from the keymap as ever.

**Mouse selection** (issue #149) is the finer grain: press and drag the left button over a
pane's body and the covered text highlights in reverse video, stream-shaped like a terminal's
own selection — first row from the anchor, middle rows whole, last row to the head. The pane
under the press owns the drag; leaving it clamps to that pane's body, so a neighbour pane or a
border can never be selected. `ctrl+c` with a live selection copies it (OSC 52, ANSI stripped,
trailing whitespace trimmed), clears it, and sends **nothing** — the status line says
`copied N lines from <host> … no interrupt sent`, so a user who expected an interrupt sees why
none went out. Without a selection `ctrl+c` stays what it always was: the interrupt keystroke
for the host or the broadcast targets.

The selection is anchored to the pane's **screen cells** and remembers the view it was made
over — pane, body rect, page, split chunk, zoom, scroll offset. Anything that changes that view
clears it: a click without a drag, `esc`, leaving the grid, a page or chunk turn, a retile, a
zoom, the pane closing, **scrolling** (the decided answer for scroll-under-selection). New
output under a tail-following pane redraws beneath the highlight without moving it; the
highlight is applied at render time from the model alone, so session reader goroutines are
never involved and the layout can never shift by a cell.

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

Every number is read from the model's fleet snapshot, refreshed by the fleet event that changed
it — as fresh as the redraw showing it, with no way to go stale silently. `FleetUpdatedMsg`
carries no payload: it makes `Update` re-read the whole fleet in one pass, so the counts and the
per-host states can never disagree with each other, and one surviving event repairs anything the
transport dropped while the UI was behind (see [Session manager](./manager.md)).

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
the options — `n` to connect, the Groups panel, the CLI — and which of them comes first is the
user's call, not the program's.

### [2] Groups

The saved groups — the [session files](./session-files.md) on disk — each with its host count
and description. This panel is a group's whole lifecycle; the full model is in
[Groups and open sessions](./groups-and-sessions.md).

- `enter`/`space` open the group as a session: the program resolves its patterns through
  `~/.ssh/config` and connects; a group whose session is already open is foregrounded instead
  of dialled twice. The open group's row is marked with `▸` as well as a style,
- `n` creates a group: a two-question dialog (name, then host patterns, brace expansion
  allowed) that owns the keyboard; a taken name or a malformed pattern keeps the dialog open
  with what was typed,
- `d` asks `delete "x"? y/n` and removes the file on `y` — an open session of that group is
  untouched,
- `w` saves the current run as a group: a name prompt that owns the keyboard. An existing name
  is **never** replaced silently: the first `enter` turns into `overwrite "x"? y/n`. The run's
  **patterns** are written, not the hostnames they expanded to,
- `[` and `]` page the **working set** by its own chunk size — see
  [Working sets](./working-sets.md),
- one unreadable file becomes one `(unreadable)` row rather than an empty panel — hiding the
  other groups would make a typo look like data loss.

The panel does not dial. `enter` emits a `GroupOpenMsg`; the layer that owns the transport acts
on it, which keeps `internal/ui` unable to open a connection. While this panel has focus, `n`
and `d` deliberately shadow their global meanings (connect a host, select the down hosts) —
lazygit-style panel keys, resolved by routing order before the global bindings.

### [3] Sessions

The **open** sessions — the runtime workspaces, one per opened group plus the ad hoc session of
an unnamed run — each with its up count, the foreground one marked with `▸`.

- `enter`/`space` bring the session under the cursor to the foreground: its panes replace the
  grid, the broadcast scope follows, and nothing is dialled or torn down — a background
  session keeps every connection,
- `x` ends the session under the cursor after `y/n`, and a session whose hosts all closed ends
  by itself — see [Groups and open sessions](./groups-and-sessions.md).

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

The bar never mirrors what was typed — the panes carry each host's own echo, and the bar's copy
would be a second, possibly divergent truth (issue #164). It still assembles the line internally
— printable text appends, backspace trims — but only for the audit trail. `enter` sends a
carriage return and records the
assembled non-empty line in the [command log](./command-log.md) once; the individual keystrokes
are never recorded, because this is where a password may be typed. The title always carries the
live target count (`Broadcast [5] → 7 hosts`), and the status bar says
`BROADCASTING EDIT → 7 hosts` in the warning style while the bar has the keyboard. On short
terminals the bar degrades to a bare line and then disappears before the grid gives up a row.

The pane-management chords (`alt+arrows` and friends) keep working from the bar.

### Edit and view mode

The bar is modal, vim-like in the minimal sense of having exactly two modes. **Edit mode** is
the default and everything above: keystrokes go to the hosts. **View mode** routes every key to
the app-level commands instead — broadcast scope, selection, panel numbers, `ctrl+a`, `ctrl+r`,
the pane chords — and sends nothing, so commands work without leaving the input.

Mode switching is modeled on csshx, with `ctrl+a` as the escape prefix inside the bar:

- `ctrl+a` `esc` — switch to view mode.
- `enter` (in view mode) — back to edit mode; selecting the bar again (`5`, a click) also
  re-enters it in edit mode.
- `ctrl+a` `a` — send one literal `ctrl+a` to the targets, which is how a remote `screen` or
  `tmux` behind the broadcast stays reachable. The literal never enters the assembled line.
- `ctrl+a` anything else — a **one-shot lazycssh command** (issue #148): the key is dispatched
  to the app keymap exactly as if the bar did not have the keyboard — `ctrl+a` `ctrl+a` toggles
  connected-only, `ctrl+a` `?` opens the help, `ctrl+a` `→` pages, `ctrl+a` `q` quits. The
  prefix is cleared before the second key is handled, so it cannot chain, and a key with no app
  binding is a no-op the status bar names rather than a silently swallowed keystroke. Nothing
  after the prefix reaches the hosts except the literal `a`.

The mode is unmissable: the status bar carries `BROADCASTING EDIT → 7 hosts` in the warning
style, or `BROADCAST VIEW — keys are commands` in the calm typing style, and an armed prefix
shows as `ctrl+a… next key = command · a = literal · esc = view`. The modal state does not outlive the bar's
focus — leaving in view mode and coming back lands in edit mode.

The `ctrl+a` prefix shadows the global connected-only toggle (and the readline start-of-line
the bar used to forward) while edit mode has the keyboard. That is deliberate: the global
binding itself is unchanged, and it is reachable from inside the bar as `ctrl+a` `ctrl+a` —
the prefixed key runs the app binding directly — or from view mode, which routes every key to
the app-level commands.

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
- **Drag** over a pane's body selects text for `ctrl+c` — see Copy above.

Mouse reporting is cell-motion: clicks, drags, releases and the wheel, without bare-movement chatter.
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
| `FleetUpdatedMsg` | re-read the fleet into the model snapshot, then redraw from it |
| `SessionOutputMsg` | redraw; the pane reads the internally synchronized scrollback itself |
| `HostsChangedMsg` | replace the host list, keeping the focused host |
| `SessionsChangedMsg` | re-read the group directory |
| `GroupOpenMsg` | emitted, not handled: the program resolves and connects a saved group's hosts |
| `SessionOpenedMsg` | a group's hosts are in the fleet: upsert its open session and foreground it |
| `GridChangedMsg` | emitted, not handled: the visible panes changed shape, the program resizes the PTYs |
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
