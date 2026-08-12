---
type: concept
title: TUI shell
description: The root bubbletea model, the layout arithmetic, and the rules that keep a resize from taking the program down.
resource: internal/ui/app.go
tags: [ui, bubbletea, layout, focus]
timestamp: 2026-08-12T18:00:00Z
---

# TUI shell

`App` is the root bubbletea model. It owns the layout, the focus and the panel selection, and it
draws the frame every other view renders into: a lazygit-style stack of titled panel boxes on the
left, the pane grid on the right, the always-visible broadcast bar under the grid — beside the
sidebar, which keeps its full height — a status bar along the bottom, and the [dialogs](#dialogs)
and the `?` popup composited over the frame.

Model mutation happens only in `Update`. Nothing in `internal/ui` dials a host; the transport
reports through messages — see [Session manager](./manager.md) — and the only bytes the UI
writes travel through narrow interfaces the program hands in (`PaneWriter` for the focused pane,
the sender for broadcast).

## Layout

`ComputeLayout(width, height)` is pure arithmetic, so it can be tested at every size without a
terminal. `ComputeScreenLayout(width, height, mode, focus)` is the same arithmetic for a given
[screen mode](#screen-modes); `ComputeLayout` is the normal-mode call. That matters more than it sounds: a layout that underflows to a negative width is how
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

Unselected panels are previews, not blanks (issue #186): every panel renders its body into
whatever height it was dealt, and `titledBox` clips the overflow, so an unselected Status,
Groups or Sessions box shows the top of its content. When the sidebar has height to spare
beyond the collapsed boxes, half of the surplus is split between the unselected panels — capped
at `PreviewPanelMaxHeight` (8 rows) — and the selected panel keeps the rest, so it always
dominates. The tight-sidebar degradation is unchanged: collapsed boxes, bare titles, selected
panel only. `SidebarHeightsMode(total, panels, selected, expanded)` is the same split with the
previews given up — that is what half mode does to the sidebar.

Rules:

- the **status bar is never dropped**. The broadcast target count is the one thing the user must
  always be able to see;
- the status bar's app-name segment carries the version (`lazycssh v0.10.13`), read from
  `internal/version` and passed in through `Config.Version` — `internal/ui` never imports
  `internal/version` directly, so the layering stays one-directional. `Config.Version` empty
  (as in most view tests) renders the bare name;
- the sidebar is a quarter of the width, clamped to 20–34 columns (half the width, uncapped, in
  half mode with the sidebar focused; `SidebarMinWidth` in half mode with the grid or the bar
  focused), and it disappears entirely below `SidebarMinWidth + MainMinWidth`. It gives up its share before the grid does: output
  squeezed to nothing is worse than no panel list;
- below 24×4 the interface renders a single "terminal too small" line — clipped to the actual
  width, so even the apology cannot wrap into the scrollback;
- every rect is non-negative at every size, including sizes no terminal reports. A test sweeps
  every width from −5 to 200 and every height from −5 to 60 and asserts it.

### Screen modes

Three screen modes cycle with `alt++` (lazygit's `+`, taken as a chord because a pane forwards a
plain `+` to the shell — see [Keymap and help](./keys.md)): **normal → half → full → normal**.
`alt+z` stays the direct way in and out of full screen from any mode, because zooming one pane is
what a user reaches for mid-typing and it must never cost two presses. `ScreenMode` lives in
`internal/ui/screen.go`; every mode's geometry comes out of `ComputeScreenLayout`,
`SidebarHeightsMode` and `TileGridCapped`, so there are no sizes hardcoded in a renderer.

| Mode | Sidebar focused | Grid (or bar) focused |
|------|-----------------|-----------------------|
| normal | quarter width, unselected panels preview | quarter width, every host that fits is tiled |
| half | **half the width**, unselected panels collapse to titles | sidebar shrinks to `SidebarMinWidth`, the grid shows at most `HalfScreenPanes` (2) panes, so the focused pane is about half the screen |
| full | unchanged frame | one pane fills the main area |

Half mode is about whatever has the keyboard: entering a pane hands the width back to the grid,
`ctrl+]` gives it back to the panel column, with no second keypress. The mode is a *view*
setting — it never changes who receives a keystroke — and it does not outlive the run: a run that
empties resets to normal, like every other view state (issue #168).

Half mode capping the page means hosts page out; that is announced, not implied. The status bar
carries `screen half` / `screen full` in the warning style, the page indicator and the overflow
footer say what is hidden and which key reaches it. The frame is recomputed after every message
(`syncLayout`), because the geometry now depends on focus as well as on the terminal size, and
the program re-sizes the remote PTYs whenever the pane size actually moved — a mode or focus
change resizes them just like a window resize does (issue #219).

### What a frame is allowed to cost

The render path is budgeted (issue #274): a pane draws its **window**, never its whole
scrollback. `paneBody` materializes only the visible lines through `paneContent`, the scroll
clamps count lines without building them (`virtualLineCount`), and the per-event readers — the
output filter, the diff panel's tails — use the emulator's bounded calls
(`TailText`/`TextLineCount`, see [Terminal emulation](./terminal.md)) instead of `Text`. The
last full-scrollback walk — a live search term recounting the focused pane's matches every
frame — is gone too (issue #278): the history's matches live in an incremental cache
(`searchcache.go`) keyed to the emulator's `HistoryCursor`, which says how many lines
appended and how many the cap dropped since the last look, so a frame rescans only the
appended tail plus the screen rows. The cache validates itself against the cursor on every
read — resize reflows and retention changes move its generation and force a rescan — so
there is no invalidation hook to forget, and a mismatch can only cost a rescan, never a
wrong index. Match *cursor* state (`matchAt`, `searchAnchor`) stays in the model; the cache
is a memo behind a shared pointer, filled wherever `matchLines` runs. What still scales with
the match count is the highlight itself: restyled lines carry ANSI that makes the border
render costlier, but that is bounded by the window, not the scrollback.

Two structural rules keep it that way. First, `View` plants a **one-frame memo** on its value
copy of the model (`framememo.go`): the visible host list and the tiled grid are computed once
per frame however often the renderers ask, and the memo dies with the copy — it can never leak
state into `Update`. Second, the root model must stay **cheap to copy**: it travels by value
through every method call, and Go heap-allocates implicit copies of 64 KiB or more, so the fat
singletons (`Theme`, `KeyMap`) are held by pointer — both are immutable after construction —
and every `textinput.Model` (~7.8 KiB each, nine across the model) sits in a `boxedInput`
(`boxedinput.go`, issue #279): a pointer with **copy-on-write mutators** — every write clones
the widget and repoints the field, so two model copies can never see each other's typing, and
reads go straight through the pointer. The `help.Model` is behind a pointer under the same
discipline, replaced at its two mutation sites (resize, theme change). That puts `App` at
~3.2 KiB, and `TestAppStaysCheapToCopy` pins it under the 64 KiB cliff. A new field that is
big or grows must go behind a pointer; a new input goes in a `boxedInput`, and a new mutator
on it must clone first — never write through the shared pointer.

## The main area

The main area is lazygit's detail view, with one lazycssh-specific rule on top: **the grid
outranks every preview** (issue #290). A host's output is live, so a pane leaves the screen when
its session ends — never because the user walked the cursor through another panel. Whatever has
the keyboard, the main area draws the pane grid as long as there is a pane to draw.

The read-only **preview** of a list panel's cursor row (issue #218) — what `enter` would act on
before it is pressed — is therefore the *empty* grid's tenant: it takes the main area only when
the grid has no host to show, which is the argumentless start the preview is most useful in. With
hosts on screen the sidebar keeps answering the same question: the selected panel expands to its
full body, the others [preview inline](#layout) (issue #186). The panels that preview:

| Panel | Preview |
|-------|---------|
| `[2] Groups` | the group's host count, description, whether it is already open, and its host patterns as typed |
| `[3] Sessions` | foreground or background, ending, `n/m up`, and every host with its connection state (and the failure text behind a failed one) |
| `[4] Command log` | the whole command, the timestamp, and the scope it went out in — the list row truncates a long command, this does not |
| `[6] Output diff` | the variant under the cursor whole: the command, which hosts gave this answer, and the output as the first of them printed it — see [Cross-host output diff](./output-diff.md) |

Previews are built from model state alone — the group rows the store was last read into, the
[fleet snapshot](#the-fleet-snapshot), the in-memory [command log](./command-log.md). Nothing in
`internal/ui/preview.go` dials, reads a file or touches live session state, so a preview cannot
disagree with the frame it is drawn in. A preview taller than the area says how much it hid
(`+7 more`) rather than clipping silently, and it is drawn with `titledBox` into `Layout.Main`,
so it degrades with the box at every size and the too-small guard still wins.

While a preview is showing, the main area is not a grid: a click there does not close or type
into a pane that is not on screen — it only takes the keyboard back to the grid, which is a
no-op while there is no pane to enter — and the wheel over it scrolls nothing. The switch is
`mainPreview()`, read once by `renderMain` and by both mouse handlers, so the frame and the
hit-testing cannot disagree about what the main area holds.

Empty grid does *not* mean empty session: a slot whose host left the fleet is a hole, and a grid
of nothing but holes has no output to protect, so it hands the area to the preview like an
empty one. With no hosts and no previewing panel focused, the main area is the **empty state** —
what to press to get hosts — as it always was.

### The row preview on demand

Dropping the full-area preview would have cost one panel more than the others: the
[output diff](./output-diff.md) shows the variant under the cursor *whole* only in the preview,
and it is a panel one only uses with hosts connected. So the preview did not disappear with
hosts on screen, it moved onto a key: **`p`** floats the focused panel's preview over the frame
as a popup (`previewOverlay()`), centred inside `Layout.Main` with a margin of grid showing
around it, and any key closes it again — the `?` overlay's contract, guard and all, so nothing
drives the fleet while it is being read. `p` on a panel without a preview (Status) does nothing.

That is the whole trade of issue #290: the grid is never taken away, and the detail is one key
away instead of one focus change away.

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
`ctrl+s`) compacts and tiles for the new view rather than keeping a museum of the old
one. A terminal resize reflows the cells but keeps the slots as they are: the user changed the
window, not the run. While typing, `ctrl+r` belongs to the host (readline reverse-search).

`f` full-screens the focused pane and `f` again returns to the grid. The issue proposed `1`/`2`/`3`
for this, but the epic gives the number keys to the sidebar panels; a key cannot mean both, so
full screen took the letter.

## The window

The **window** is which hosts are on screen. It is not the [working set](./working-sets.md),
which is which hosts a command is about: the working set says which machines the run is
addressing, the window says how many of them fit on the terminal right now.

**The window bounds the broadcast** (issue #199). The page on screen is pushed into the
router's [visibility limit](./broadcast-scope.md), so `all`/`selected` reach the panes the user
can see and nothing behind them — a run of ten hosts on a terminal that draws nine sends to
nine, and paging on sends to the tenth. What the panes show is what receives a keystroke; a
command that quietly went to hosts scrolled off screen is exactly the surprise this tool exists
to prevent. `fleet` stays unbounded — it is the explicit every-host escape hatch — and `single`
is already the focused pane. Full screen (`f`) keeps its page's limit rather than turning `all`
into a one-host send: it is an explicit zoom with its own way back.

- `ctrl+shift+→` / `ctrl+shift+←` are the **single navigator** for "the next screenful"
  (issue #147): they move the window a whole page — and, at a chunk boundary of an active split,
  the chunk — wrapping at both ends. Page-major: forward steps every page of the current chunk,
  then the next chunk at its first page; backward mirrors that and lands on the previous chunk's
  last page. The pane focus follows onto the new screenful, so the pane that receives a keystroke
  is one the user can see. The chords work while typing too, like the other pane-management
  chords. Plain `ctrl+arrows` were the paging keys once (issue #208) but IDEs and window managers
  swallow them before lazycssh sees them; they are never claimed now and stay keystrokes for the
  hosts in every context (readline word movement). `ctrl+shift+arrows` have the opposite problem —
  macOS Terminal.app never transmits them at all — so `ctrl+a` `→` / `ctrl+a` `←` is the portable
  way to the same `stepView` path, live in a pane, in the broadcast bar and at the app level
  (issue #273; see [the chord](#the-ctrla-chord)),
- moving the pane focus off the edge of a page turns the page rather than focusing a pane that is
  not drawn, and the broadcast limit follows the new page,
- the page indicator (`page 2/5`) appears in the status bar only when there is more than one page,
- whenever the grid is showing a *part* — more pages, more split chunks, more open sessions — an
  **overflow footer** takes the grid's bottom line and says so in place: `+12 hosts — ctrl+shift+→
  · page 1/3 · 2 more sessions — [3]`. Each part names its own navigation (page and chunk
  `ctrl+shift+→`, session panel `3`).
  The visible panes must never read as the whole run; a muted counter in the status bar alone is
  too easy to read past. Full screen skips the footer — `alt+z` is an explicit zoom with its own
  way back; half mode keeps it, because a capped page is exactly the case where the hidden hosts
  must be named,
- the page is clamped on every render: a terminal that shrinks produces more pages, and the page
  the user was on may stop existing.

### The fleet snapshot

The visible list is a view over the model's **fleet snapshot**, not over live session state: a
host that reconnects reappears without a keypress — its state change is a fleet event, the event
refreshes the snapshot inside `Update` (`App.snapshotFleet`, issue #136), and the redraw
recomputes the visible list from it.
Render helpers never read live session state: `hostIDs()` is a pure function of model fields,
so every computation inside one frame agrees. Two computations inside one render used to
disagree — a mass disconnect shrank the list between a bounds check and the
index it guarded, and `renderPane` panicked (issue #135); the snapshot removes the class of bug,
and a spy-fleet test plus a `-race` state-flip hammer pin it down.

### Split

`ctrl+s` asks for a number in a centred [dialog](#dialogs) and splits the visible hosts into
consecutive chunks of that size:
ten hosts split by five shows the first five terminals. `ctrl+shift+→`/`ctrl+shift+←` page through the
chunk and then show the next or previous chunk, wrapping at the ends (see The window above). Broadcast follows the visible chunk
through the router's [visibility limit](./broadcast-scope.md), and
the status bar carries `SPLIT 1/2 (5 hosts)` in the warning style for as long as the split
narrows anything. An empty prompt or `0` clears the split, `esc` keeps it; chunks are cut
from the session's host list, and a chunk whose hosts left the run
clamps to the last one instead of rendering an empty grid. The typing exception applies here
too: in a pane, `ctrl+s` is flow control for the host.

### Output filter

`f` asks for a pattern in a centred [dialog](#dialogs) and the grid then draws only the panes
whose **recent output** holds it (issue #255) — with forty panes, "which of these said error" is
a question the eye answers badly. The filter runs before the split, so a filtered run splits into
chunks of matches.

- **Matching is a case-insensitive substring**, not a regexp: a filter is typed in a hurry, and
  `error` should not have to be escaped while a half-typed pattern narrows the grid instead of
  failing to compile.
- The window matched is the output **since the last command-line send** — the same watermarks the
  [Output diff](./output-diff.md) compares from — so "which hosts failed *that* command" is not
  answered by an hour of older scrollback. A host no send reached has no watermark, and the last
  200 lines are read instead.
- **It is a view, not a selection.** A hidden pane still receives everything broadcast: the
  router's [visibility limit](./broadcast-scope.md) is computed as if the filter were off
  (`syncBroadcastLimit` clears it on a copy of the model). A filter that silently narrowed the
  target set would break the one promise the status bar makes.
- The state is unmissable: the status bar carries `filter: "error" (5/40)` in the warning style,
  and the overflow footer adds `+35 hosts hidden by the filter — f`.
- **New output re-evaluates the matches live.** The match set is a model field
  (`App.filterMatch`), recomputed inside `Update` on output, fleet and host-list messages and on
  every send — never in a render helper, for the reason [the fleet snapshot](#the-fleet-snapshot)
  gives: a host list read twice inside one frame must not disagree.
- The focus is kept by identity and then clamped, so it never lands on a hidden pane; clearing
  restores the full grid with the focus on a pane that exists. An empty prompt or `esc` clears —
  the key that means "get me out of here" must not leave panes hidden.

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
- `shift+alt+arrows` switch panes (they work while typing and from the app level alike; plain `alt+arrows` are the shell's word navigation and are forwarded), `alt+z`
  full-screens and `alt++` cycles the [screen modes](#screen-modes), `alt+x` closes/removes, `alt+r` reconnects; paging is `ctrl+shift+→`/`ctrl+shift+←`, while typing and at the
  app level alike,
- key presses are dispatched by area, so a key means one thing at a time — see
  [Keymap and help](./keys.md). Commands exist only while no input pane is focused,
- pane focus never wraps. Stepping off the last pane onto the first is how a user ends up typing
  into the machine at the other end of the fleet. The window navigator (`ctrl+shift+→`/`ctrl+shift+←`) does
  wrap — it changes what is on screen, never who receives a keystroke, and the page/SPLIT
  indicators say where it landed.

## Pane output

Each pane carries a one-line header — pane number, host name, connection state and the last
command's exit status (`·` / `✓` / `exit 1`) — all read from the model's fleet snapshot, which
the fleet event that changed them refreshed, so a change is on screen the moment the redraw
happens. The terminal
body is the one deliberate live read left in the render path: the buffer is internally
synchronized, and snapshotting whole terminal contents per output event
would copy far more than a redraw reads. When the width cannot hold everything the state gives up its space
first and the exit status second — a failure must outlive the state label — and the host name
truncates **from the left** (`…-1a-40.example.com`): in a fleet of near-identical names the
suffix is the distinguishing part.

### Failure visibility

The transport reports each command's exit status through a prompt hook — see
[SSH session lifecycle](./session.md). What the interface does with it is **per command, not per
session** (issue #251): a pane header answers "how did *the last command this host was sent from
the command line* end", and nothing else.

The mechanism is one map, `App.cmdExitMarks` (`internal/ui/failures.go`). A send records each
reached target's marker sequence; the header then reads:

| Header | Meaning |
|--------|---------|
| *(nothing)* | no command of ours is outstanding on this host, or its shell has never emitted a marker |
| `·` (muted) | the command is out and the shell has not reached its next prompt |
| `✓` (success) | the command's marker came back with `0` |
| `exit N` (danger, bold) | it came back non-zero |

Three shapes carry three states, so the header survives a terminal without colour. The success
mark is one character on purpose: `ok` spelled out on two hundred panes buries the three that
matter, while a failure spells out its code.

Around that:

- a pane whose last command exited non-zero gets a **danger-coloured border**, focused or not,
  and the header states `exit N` in text, because colour alone is not allowed to carry meaning;
- the status bar counts them: `3 hosts failed`, in the failure style — the last command's
  failures, not the run's;
- `!` jumps the pane focus to the next failing host, from anywhere, wrapping around — the wrap
  is deliberate, unlike pane movement, because this is a search and a failure behind the cursor
  must be as reachable as one ahead;
- a command still out is not a failure. It is counted nowhere and its pane keeps its ordinary
  border until the shell answers;
- a pane whose *connection* failed says why (issue #167): the session writes its error — DNS,
  refused, auth, host key — into its own terminal the way a plain one prints it (issue
  #180), so it scrolls with the history and is reachable like any other output. The snapshot
  (`hostState.errText`) still feeds the Status panel and the failure counts.

#### What the indicator refuses to claim

Every silence below is deliberate. A wrong green tick on a machine that just failed is worse
than no tick at all, so each case renders nothing rather than a guess:

- **No hook, no indicator.** A shell that never emitted a marker — plain POSIX `sh`, a
  restricted shell, a profile that overwrites `PROMPT_COMMAND`/`precmd` — leaves its sequence at
  zero, and a host at zero shows nothing. It does not even show the muted dot: a pane greyed out
  forever would be a claim about a command nobody can confirm ran. If the hook starts working
  later, the first marker past the send answers that send.
- **Raw keystrokes mark nothing.** The broadcast bar (`5`) and typing into a focused pane send
  bytes, not commands. lazycssh cannot tell where a command starts in a stream of typing, and a
  bare `enter` at a prompt produces a marker carrying the *previous* command's status — so
  keystroke input opens no window and changes no header. The same reasoning keeps the
  [output diff](./output-diff.md) window closed for bar keystrokes.
- **Only the hosts the command reached.** The map is replaced at every send, so a host outside
  the broadcast scope — or one the send could not reach, which the status bar already reports —
  keeps no mark. Its previous answer belonged to an older question.
- **A reconnect clears it.** The replacement session counts markers from zero, so a mark taken
  against the shell that died is dropped rather than compared with a smaller number.

Two honest limits remain:

- The mark is taken **before** the command's bytes leave (`App.exitMarksAtSend`), read from the
  live sessions rather than from the fleet snapshot. Read afterwards, a host that answered
  instantly would have its own answer counted as the state the send found, and the pane would
  sit on the dot until some later prompt produced another marker.
- A marker already in flight when the send happens — the tail of a raw `enter` a moment
  earlier — is still counted as the answer. Distinguishing it would need a per-command nonce in
  the marker, which means wrapping every command the user types
  (`cmd; printf '\033]133;D;%d\007' $?`): that rewrites what the shell records in its history,
  breaks on multi-line and interactive input, and is visible in the echo. The prompt hook is
  armed once, at login, and touches nothing the user types — the least invasive mechanism that
  works on plain bash and zsh, which is why the stray-marker window is accepted instead.

Below the header the pane renders its session's [terminal](./terminal.md) (issue #206):
following the tail it shows the emulator's screen, exactly like a real terminal; scrolled
back it shows a window over the shared virtual line space — the retained history, then the
screen rows, with a muted `~ older output dropped ~` marker on top once the retention cap has
evicted output. `SessionOutputMsg` carries no bytes, it only asks for a redraw, so a
coalesced or dropped message costs nothing.

The emulator interprets escape sequences for real — redraws, cursor movement, erase, SGR —
so `ls --color` looks like `ls --color` and a prompt rewrite renders cleanly instead of
leaving artifacts. Every window line is still clipped to the pane width before drawing, so a
misbehaving host cannot push the layout apart.

Long lines hard-wrap at the pane width with the colours kept intact across the break, and wide
characters are counted by their display width. When the buffer has evicted lines to stay within
its bound, a `~ older output dropped ~` marker sits where the missing output was, so truncated
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

### Export

`alt+w` (issue #252) is the postmortem grain: the clipboard copies above only reach the local
machine, and only until the next copy overwrites them. `alt+w` writes the focused pane's whole
retained scrollback — same content as `alt+d`, ANSI stripped the same way — to a file,
`lazycssh-<alias>-<timestamp>.log`, in the working directory the program was started from. The
write happens off the event loop, in a `tea.Cmd`, landing back as `PaneExportedMsg` (issue
#225's disk-I/O rule); the status line reports the line count and path, or the failure. This is
deliberately not [session logging](./session-logging.md#one-shot-export-vs-session-logging): that
is opt-in for the whole run and every host, this is one pane, one keypress, one file.

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

`/` opens a search prompt that owns the keyboard while it is open — the pager key, live in the UI
command scope where plain letters are commands, so it never collides with broadcasting a literal
slash: a focused pane is a terminal and types `/` to its host, where `alt+/` opens the same
prompt. `enter` commits the term and scrolls the focused pane to the **newest** match — the
reader is almost always hunting the error that just happened.

`n` and `N` (or `alt+[` and `alt+]`, which also work while typing) walk to older and newer
matches. They **do not wrap**: running out of matches in a direction stays put, like pane
movement, and the counter says which end the cursor is at. While a term is live these letters
shadow the app-level ones — a declared shadow like the Groups panel's `n`/`d`, resolved by
routing order and ended by `esc`.

Every matching line is drawn in the match style, its own colours dropped: a highlight fighting
the remote's colours would lose. The one line the cursor stands on takes the louder
current-match style, and the status bar counts it — `search "error" 3/17`, or `search "error"
no match` when that pane holds none. A term nothing matches moves no viewport at all.

`esc` (or `alt+c`) leaves the search: the term and the highlight go, and every pane the search
scrolled goes back to the offset it had before the first jump — recorded once per pane, at that
jump, so returning is exact. `esc` *inside* the input abandons the editing only, leaving whatever
term was live before it opened. While typing to a host a bare `esc` still belongs to the remote
shell.

The cursor is stored as a virtual-line index per pane, in the same coordinate space as the
scroll offset, and each step re-reads the match list — so output arriving during a hunt cannot
desynchronise the counter from what is highlighted.

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

## The caret

There is one cursor on a terminal, so exactly one thing in the frame may own it, and the frame
decides who — every render, from scratch. `View` sets `View.Cursor` and bubbletea hides the caret
for any frame whose `Cursor` is `nil`, which is what keeps a caret from being left wherever the
last frame put it (issue #292).

The order of ownership, in `App.frameCursor` and `View`:

1. an **open dialog** — the focused input's caret, at the column the next character lands in
   (see [Dialogs](#dialogs)). A dialog owns the keyboard, so it owns the caret even when the
   frame is too small to draw its box,
2. a **status-bar prompt** — the [command line](#the-command-line) and the scrollback
   [search](#search) own the keyboard wherever the focus happens to be (the guard chain in
   `handleKey`), so they own the caret: past the bar's padding, the prompt's sigil and the text
   typed before the caret, on the bar's own row,
3. the **focused pane**, when the grid has the focus and the host is connected: the host's own
   cursor, as its [emulator](./terminal.md) reports it, mapped through the same window the body
   was rendered from and offset by the pane's cell, border and header. Typing on the remote side
   is then trackable the way it is in any terminal — including a cursor an app moves with an
   escape sequence,
4. **nobody** — the help overlay is up, the sidebar or the broadcast bar has the focus, the pane
   is scrolled back into history, the remote app hid its cursor (`CSI ?25l`), the session is not
   connected, or the pane is on a page that is not showing. The caret is hidden.

Panes therefore paint **no** cursor cell of their own, on the history view or on the
[alt-screen grid](./terminal.md#grid-rendering-in-the-pane): a block drawn per pane would put a
caret in every connected pane at once, in panes the keyboard does not reach. The two carets that
remain styled cells are per-host prompts that several panes can show at the same time — the
inline [auth answer](./authentication.md) echo and the broadcast bar's `▏` marker — and neither
claims the terminal cursor.

The body and the caret are measured from one snapshot: the per-frame memo caches each pane's
content, so a session's reader goroutine writing between the two reads cannot place the caret a
row away from the text it belongs to.

Because the caret must land inside the pane it belongs to, the remotes are sized to the
**smallest** pane on the page rather than the first — see [Program](./program.md).

## Dialogs

Every confirm and every single-line prompt is a **centred modal**: a titled box composited over
the frame with `lipgloss.NewCompositor`/`NewLayer` rather than drawn into the panel it belongs
to — the way lazygit renders its popups. The layout stays visible around it, so "delete this
group" is answered with the group list still on screen, and the question always appears in the
same place instead of moving with the panel that asked it. `internal/ui/modal.go` owns the
rendering; `activeModal` walks the **same order** as the guard chain in `handleKey`, so what
floats is what listens.

- **confirms** — `enter` and `y` resolve, `esc` and `n` withdraw, and every other key is
  *ignored* rather than treated as no. These dialogs guard a file delete and a fleet-wide
  `ctrl+c`; a stray keystroke must not be able to answer either one. The box names its own keys
  in a footer (`enter/y confirms · esc cancels`),
- **prompts** — the existing bindings are unchanged (`enter` commits, `esc` abandons, `tab`
  completes where it completed before). The focused input's caret is lifted into `View.Cursor`,
  so the terminal draws a real cursor at the column the next character lands in — see
  [The caret](#the-caret) for who owns it when no dialog is open,
- **clamping** — the box grows to its content and stops at the frame: never wider than
  `width-2`, never taller than `height-1`, body lines clipped rather than wrapped so the footer
  cannot be pushed off the bottom. Below `MinWidth`/`MinHeight` the too-small guard wins and no
  dialog is composited at all.

Modal today: the new-group dialog (both questions), the delete-group confirm, the save-as prompt
and its overwrite confirm, the end-session confirm, the new-host prompt with its alias
completions, the [host picker](#the-host-picker), and the split-size prompt.

Deliberately **not** modal:

- the **command line** and the **scrollback search** keep the status-bar position. Both are
  aimed at the panes — the command line carries the broadcast scope it is about to hit, the
  search highlights matches in the output — and a box in the middle of the screen would cover
  the very thing being aimed at,
- the per-pane **auth** and **host-key** prompts stay in their panes: they are per host, several
  can be open at once, and each one belongs to the machine it is blocking — see
  [Authentication](./authentication.md).

The `?` popup uses the same compositing, so the fleet stays visible underneath it too. While it
is open it is the only thing listening: the key that closes it does not also act. A user reading
the help is not also driving the panes.

## Panels

The sidebar stacks the five panels as titled boxes; every title is always on screen and only the
**selected** panel shows its body. Five open panels on an 80-column terminal would show none of
them usefully; five visible titles cost three rows each and keep the map of the interface on
screen.

### Panel child models

Each sidebar panel is a **child model** (issue #228, `internal/ui/sidepanel.go`), lazygit
style, behind one interface the root dispatches through instead of switching over the panel
enum in every code path:

```go
type sidePanel interface {
    Update(msg tea.KeyPressMsg) tea.Cmd        // keys routed to the focused panel
    View(focused bool, width, height int) string
    Title() string
    Number() int
    MoveCursor(delta int)                      // mouse wheel browses
    SetCursorRow(row int)                      // click lands the cursor
    Preview(width, height int) (title, body string, ok bool)
}
```

The split of responsibilities:

- a panel owns its **view state** — the list cursor, its dialogs (new-group, delete, save-as,
  end-session), the rows it was last handed. That state lives in the child struct
  (`statusPanel`, `groupsPanel`, `sessionsPanel`, `logPanel`), not on `App`;
- the root keeps the **domain state** — open sessions, fleet snapshot, layout, focus — and
  reduces the domain messages. A panel that needs a domain change emits it the way the
  transport does (`GroupOpenMsg`, `CommandResendMsg`, the disk-write result messages), never
  by reaching into the root. The one synchronous exception is the Sessions panel's foreground
  switch: the panel records the chosen row and the root drains it right after routing the key,
  because the grid and the broadcast scope hang off the switch;
- what a panel reads of the domain arrives as a `panelContext` snapshot, pushed by
  `syncPanels` on every `Update` — the same discipline the fleet snapshot enforces for the
  grid, so a panel's `View` never reads the root model and cannot disagree with the frame.
  Long-lived collaborators (the group store, the command log, the broadcast router, the
  working set) are handed to the children once at construction.

The children are value structs inside `App` (`panelSet`), so the root model keeps its value
semantics: an `Update` mutates a local copy and returns it, and no pointer escapes into an
older model. The guard chain in `handleKey` still decides which dialog owns the keyboard; it
asks the children through accessors (`Saving()`, `GroupDialogOpen()`, `DeleteGroupPending()`,
`EndSessionPending()`), and `activeModal` asks each panel for its floating dialog in the same
order, so what floats is what listens.

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
  [dialog](#dialogs) accepting any host pattern — `host`, `user@host:port`, brace expansion
  like `web-{01..04}`. Below the input, a live **expansion preview** updates on every
  keystroke: up to the first three resolved names plus the total count, e.g.
  `web01, web02, web03 … (8 hosts)` (`internal/ui/hostpreview.go`, issue #249). It calls only
  [`hosts.Expand`](./host-expansion.md) — pure string expansion, never ssh-config lookup, DNS or
  a dial — so it is cheap enough to run on every keystroke; a pattern that fails to parse shows
  its `SyntaxError` there instead, and the input stays open and editable. The concrete aliases
  of `~/.ssh/config` that are not in the run yet are listed further down, filtered by what has
  been typed; `tab` completes the first match, `enter` connects, `esc` abandons. The prompt owns
  the keyboard while open: a pattern containing `b` must not switch the broadcast mode (`ctrl+q`
  still quits). A connect that fails to *resolve* — as opposed to fails to *parse* — shows its
  error in the Status panel until the fleet next changes. The UI cannot dial: it emits
  `HostConnectMsg`; the program resolves, skips hosts already in the run (double-enter must not
  mint `host-2`), and adds the rest via `Manager.Add`,
- **browse and connect** — `A` opens the [host picker](#the-host-picker), which lists what the
  run *could* connect to rather than asking for a pattern,
- **selection** — `alt+space` toggles the focused pane's host, from the grid and the app level,
  like the other pane chords; `a`/`i`/`c`/`u`/`d` (select all / invert / clear / up hosts /
  down hosts) work at the app level, and `/select` / `/deselect` cover the pattern cases.
  Selection lives in the [broadcast router](./broadcast-scope.md) and is keyed by **host
  identifier**, so it survives a reconnect and a page turn — the pane moves, the host keeps its
  name.

An argumentless start opens on the Status panel with **no** input focused: the empty grid names
the options — `A` to pick hosts, `n` to type one, the Groups panel, the CLI — and which of them
comes first is the user's call, not the program's. The status bar echoes the same nudge next to
the host count — `0 hosts — press A to add` — since it is the one line that stays on screen
whichever panel has focus (issue #247).

### The host picker

`A` opens a centred [dialog](#dialogs) listing everything the run could connect to
(`internal/ui/hostpicker.go`, issue #246). The new-host prompt answers "connect this pattern";
the picker answers "which hosts are there", which needs a list rather than five completion hints.
Both stay: the prompt is three keystrokes for a name already known, the picker is for browsing
and for connecting several machines at once.

Rows come from three merged sources, each tagged with a three-letter origin marker
(`internal/ui/hostsources.go`, issue #254):

| Tag | Source | Enter connects |
|-----|--------|----------------|
| `cfg` | the concrete aliases of `~/.ssh/config`, in file order | that alias |
| `grp` | the [saved groups](./groups-and-sessions.md), listed as `@name` | every pattern in the group |
| `rec` | the [recent hosts](./recent-hosts.md), most recent first | that host |

The tag is text in the row rather than a colour, for the same reason the mark is: it has to
survive a terminal without styling and stay readable inside the cursor's full-row highlight.

- **filter** — everything typed that is not one of the picker's own keys narrows the list by a
  case-insensitive **subsequence** match: `wb1` matches `web-01`. Subsequence is enough for
  short, structured host names, and it costs no dependency. Typing puts the cursor back on the
  first match, because the row it was on is not the row it would land on once the list moved,
- **marks** — `space` and `tab` mark the highlighted host and step down one, so a run of hosts
  is marked with a run of spaces. Marks are kept in mark order, in a slice rather than a map,
  because the picker is a value inside `App` and is copied with it — a shared map would let one
  copy write through another,
- **enter** — connects the marked hosts if there are any, otherwise the highlighted one,
  otherwise the typed text as a **literal host pattern**, brace expansion included. That last
  case is how a machine `~/.ssh/config` has never heard of is reached from here; the footer says
  which of the three `enter` would do. Only in that fallback case — nothing marked, nothing
  matching — does the "no match" line grow the same live [expansion
  preview](#connecting-and-selecting-without-a-hosts-panel) as the new-host prompt, since that is
  the only case where what is typed is what `enter` would connect (issue #249),
- **esc** — closes with nothing done, and nothing typed or marked survives into the next opening.

The picker never dials. Like every other connect path it emits `HostConnectMsg` and lets
[the program](./program.md) resolve and open the sessions, which is also why free text works
here without the UI knowing anything about [expansion](./host-expansion.md).

Its candidates come from a `HostSource` — a one-method interface returning tagged `PickerItem`
rows — read once when the picker opens, so an implementation may do real work: the group source
reads the group directory there. The default is `MergeHostSources` over the three sources above,
in that order, and the order is the preference: deduplication is by row name, first source wins,
so a host that is both an ssh-config alias and a recent connect is one `cfg` row. Group names
carry the `@` prefix the command line uses, which is what keeps a group and a host of the same
name two rows rather than a collision. A source that cannot be read — an unlistable group
directory, an unreadable recent file — costs the picker those rows and nothing else.

A row carries the patterns connecting it sends, which is how a `grp` row connects a whole group
without the picker knowing what a group is; a row without patterns connects its own name. Marks
mix freely: a marked group and a marked host connect together, in mark order, the group
contributing all of its patterns.

### [2] Groups

The saved groups — the [session files](./session-files.md) on disk — each with its host count
and description. This panel is a group's whole lifecycle; the full model is in
[Groups and open sessions](./groups-and-sessions.md).

- `enter`/`space` open the group as a session: the program resolves its patterns through
  `~/.ssh/config` and connects; a group whose session is already open is foregrounded instead
  of dialled twice. The open group's row is marked with `▸` as well as a style,
- `n` creates a group: a two-question [dialog](#dialogs) (name, then host patterns, brace
  expansion allowed) that owns the keyboard; a taken name or a malformed pattern keeps the
  dialog open with what was typed, and reports why inside the box,
- `d` asks `delete "x"?` in a confirm dialog and removes the file on `enter`/`y` — an open
  session of that group is untouched,
- `w` saves the current run as a group: a name prompt that owns the keyboard. An existing name
  is **never** replaced silently: the first `enter` turns into an `overwrite "x"?` confirm. The
  run's **patterns** are written, not the hostnames they expanded to,
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
- `x` ends the session under the cursor after an `end "name"?` [confirm](#dialogs), and a
  session whose hosts all closed ends by itself — see [Groups and open sessions](./groups-and-sessions.md).

### [4] Command log

Every command sent this run, newest last, each with its target count and mode — see
[Command log](./command-log.md). `enter` sends an entry again, to the **current** target set.

### [6] Output diff

The hosts grouped by the output they produced since the last command line send, largest group
first, every group past the consensus in the warning style — see
[Cross-host output diff](./output-diff.md). `enter` makes the variant's hosts the selection, so
`B` can broadcast a fix to exactly the machines that disagree. It sits on `6`, not `5`: the
broadcast bar had `5` first, and a new panel is not worth renumbering it.

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

The pane-management chords (`shift+alt+arrows` and friends) keep working from the bar.

### Edit and view mode

The bar is modal, vim-like in the minimal sense of having exactly two modes. **Edit mode** is
the default and everything above: keystrokes go to the hosts. **View mode** routes every key to
the app-level commands instead — broadcast scope, selection, panel numbers, `ctrl+r`,
the pane chords — and sends nothing, so commands work without leaving the input.

Mode switching is modeled on csshx, with `ctrl+a` as the escape prefix inside the bar:

- `ctrl+a` `ctrl+a` — send one literal `ctrl+a` to the targets: the GNU-screen double press,
  which is how a remote `screen` or `tmux` behind the broadcast stays reachable
  (`ctrl+a` `ctrl+a` then `c` opens a window on every host). The literal never enters the
  assembled line.
- `ctrl+a` `a` — the same literal, matching `screen`'s own `ctrl+a` `a`.
- `ctrl+a` `→` / `ctrl+a` `←` — page to the next/previous screenful, exactly like
  `ctrl+shift+arrows` (issue #273). The arrows are the two keys the prefix takes from the
  passthrough; everything else about the bar's prefix is unchanged.
- `ctrl+a` `esc` — switch to view mode.
- `enter` (in view mode) — back to edit mode; selecting the bar again (`5`, a click) also
  re-enters it in edit mode.
- `ctrl+a` *a command key* — **run that app-level command** (issue #289): the whole
  `AreaGlobal` set, so `ctrl+a` `?` opens the help and `ctrl+a` `b` switches the broadcast
  scope, without leaving the line. A plain letter stands for its ctrl chord — GNU screen's
  `ctrl+a c` ≡ `ctrl+a ctrl+c` — which is how `ctrl+a` `r` reaches `Retile`'s `ctrl+r`, a
  chord the bar would otherwise send to the hosts. The mapping is `App.chordCommandKey`, a
  pure query over the effective keymap, so a rebound command moves its chord with it.
- `ctrl+a` anything else — **forwarded to the targets** as the keystroke it is (issue #214),
  through the same `paneKeyEvents` encoding as plain typing, but kept out of the assembled
  line: a prefixed key is a control sequence, not command text.

The prefix is cleared before the second key is handled, so it cannot chain. What no command
claims still forwards, because the prefix also exists for the remote multiplexer; view mode
(issue #148's dispatch, restated as a mode) remains the way to run a run of commands rather
than one.

The mode is unmissable: the status bar carries `BROADCASTING EDIT → 7 hosts` in the warning
style, or `BROADCAST VIEW — keys are commands` in the calm typing style, and an armed prefix
shows as `ctrl+a… ←/→ page · ctrl+a/a = literal · esc = view · command keys run, the rest go to
the hosts`. Every key named in those labels is read from the binding that handles it
(`App.prefixKey`, `App.pagingLabel`, `App.escapeKey`), so a [remapped keymap](./keys.md#the-keymap-file)
cannot leave a lie on the bar. The modal state does not outlive the bar's
focus — leaving in view mode and coming back lands in edit mode.

The `ctrl+a` prefix shadows the readline start-of-line the bar used to forward while edit mode
has the keyboard. That is deliberate: the literal stays reachable as `ctrl+a` `ctrl+a` and
`ctrl+a` `a`.

### The ctrl+a chord

Since issue #273 the prefix is not the bar's alone — it is armed the same way wherever focus is,
because paging had to become reachable in terminals that never deliver `ctrl+shift+arrows`:
macOS Terminal.app does not transmit them, and Mission Control or an IDE keymap eats them
elsewhere. Both bindings stay; the chord is the second way in, not a replacement.

| After `ctrl+a` | In a pane | In the broadcast bar | At the app level |
|----------------|-----------|----------------------|------------------|
| `→` / `←` | next / previous screenful | same | same |
| `a`, `ctrl+a` | one literal `ctrl+a` to the focused host | one literal to the targets | nothing to send: the status line says where the literal goes |
| `esc` | cancel | switch to view mode | cancel |
| a command key | handled as if pressed alone (so it reaches the host) | **runs that app command** (issue #289) | dispatched as the app command it is |
| anything else | handled as if pressed alone (so it reaches the host) | forwarded to the targets | handled as if pressed alone |

A pane is deliberately not in the command column: it is one host's terminal, its own commands
are the `alt` chords, and swallowing a prefixed keystroke there would cost the user a key the
shell was waiting for. `ctrl+]` is the way from a pane to the command scope.

The armed state is one bool on the model (`prefixArmed`, mutated only in `Update`, see
`internal/ui/prefix.go`); it lasts exactly one key press and cannot chain, because every handler
clears it before resolving the second key. It is not bar state, so it survives the routing that
resets the bar's modes. While it is armed the status bar carries
`ctrl+a… — ←/→ page · ctrl+a/a = literal ctrl+a · esc cancels` in the warning style: the next
key press is a command rather than input, and the user has to be able to read that. The keys in
that label come from the bindings, not from constants, so it survives a user keymap.

`Prefix` and `PrefixLiteral` are the two bindings a [keymap file](./keys.md#the-keymap-file) may
not move: the prefix is how a command is reached from inside a terminal-like input, and the
literal is how a remote `screen`, `tmux` or readline stays reachable.

Inside a pane `ctrl+a` is
the readline beginning-of-line, which is why `screen`'s double-press convention is followed
rather than invented over: `ctrl+a` `ctrl+a` and `ctrl+a` `a` hand the literal back.

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
| `SessionOutputMsg` | redraw; the pane reads the internally synchronized emulator itself |
| `HostsChangedMsg` | replace the host list, keeping the focused host |
| `SessionsChangedMsg` | start a `tea.Cmd` that re-reads the group directory — disk I/O never runs inside `Update` (issue #225) |
| `GroupsLoadedMsg` | the directory re-read's result: replace the Groups panel's rows |
| `GroupSavedMsg` | the new-group write's result: close the dialog and re-read, or keep it open with the error |
| `GroupRemovedMsg` | the delete's result: surface an error in the panel, re-read either way |
| `SaveResultMsg` | the save-as write's result: close the prompt and re-read, ask `overwrite?` on a taken name, or report the failure |
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

`Init` returns `tea.RequestBackgroundColor` — which is what makes the palette match the
terminal rather than guessing — batched with the first group-directory read, so not even
startup touches the disk inline.

While a group or save write is in flight its dialog keeps the keyboard and swallows keys: a
second `enter` cannot start a second write, and the typed input survives until the result
message says what happened.

## Testing a TUI

There is no terminal in the tests. `Update` is driven with synthetic messages and `View().Content`
is asserted against — with the ANSI styling stripped, so the assertions are about what the user
reads rather than about how it was coloured.
