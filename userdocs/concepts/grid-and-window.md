# The grid and the window

The pane grid *is* the host list. There is no separate hosts panel: names,
states and `exit N` markers live in the pane headers, ++exclam++ jumps to the
next failure, and ++n++ connects from anywhere.

## Tiling

`TileGrid` picks the squarest arrangement that holds the hosts:

| Hosts | Shape |
|---|---|
| 1 | 1×1 |
| 2 | 2×1 |
| 3, 4 | 2×2 |
| 6 | 3×2 |
| 9 | 3×3 |
| 12 | 4×3 |
| 20 | 5×4 |

A pane never shows its host less than a **45×16 terminal**, and the remote PTY
is sized to exactly that content, so remote line wrapping matches what you see.
When the hosts stop fitting at that size, the grid **pages** rather than
shrinking further — four readable panes and a page indicator beat twelve
unreadable ones.

An empty slot on the last page (three hosts in a 2×2) is drawn as an empty
frame rather than reflowed, so panes stay where they are as hosts come and go.

## Departures leave a hole

A host leaving the run does **not** reflow the grid. Its slot becomes a hole —
an empty frame exactly where the pane was — so a host closing in the middle
moves nothing: the survivors keep their positions and their numbers. Pane
movement steps over a hole, focus never rests on it, clicking it selects
nothing, and it counts for nothing in any host count.

Retiling is an explicit act:

- ++ctrl+r++ closes the holes and re-tiles for the current hosts (and resizes
  the remote PTYs),
- switching session or setting a split compacts and tiles
  for the new view,
- resizing the terminal reflows the cells but keeps the slots — you changed the
  window, not the run.

While you are typing into a host, ++ctrl+r++ belongs to the remote shell
(readline reverse-search).

## The window

The **window** is which hosts are on screen. It is not the
[working set](groups-and-sessions.md#working-sets), which is which hosts a
command is about: the working set is the machines you are addressing, the window
is how many of them fit on this terminal.

**What you see is what receives.** The page on screen bounds the
[broadcast](broadcast.md): `all` and `selected` reach the panes drawn right now
and nothing on the pages behind them, and paging moves the broadcast with it.
`fleet` (++ctrl+alt+b++) stays the explicit every-host escape hatch, and full
screen (++f++) is a zoom — it keeps its page's targets rather than silently
turning `all` into a one-host send.

- ++ctrl+shift+right++ / ++ctrl+shift+left++ are the single navigator for "the next
  screenful": a whole page, and at a chunk boundary of an active split, the next
  chunk, wrapping at both ends. Pane focus follows onto the new screenful, so
  the pane that receives a keystroke is one you can see.
- Moving the pane focus off the edge of a page turns the page rather than
  focusing a pane that is not drawn. Pane focus itself never wraps — stepping
  off the last pane onto the first is how you end up typing into the machine at
  the other end of the fleet.
- The page indicator (`page 2/5`) appears on the status bar only when there is
  more than one page.
- Whenever the grid shows only a *part* of the run, an **overflow footer** takes
  the grid's bottom line and says so, naming the navigation for each part:

  ```
  +12 hosts — ctrl+shift+→ · page 1/3 · 2 more sessions — [3]
  ```

  The visible panes must never read as the whole run.

Paging works while typing too, like the other pane-management chords. Plain
++ctrl+left++ and ++ctrl+right++ are never claimed: they stay keystrokes for the
hosts (readline word movement) in every context — IDEs and window managers tend
to swallow them before lazycssh sees them anyway.

## Narrowing what is on screen

**Split** — ++ctrl+s++ asks for a number and cuts the visible hosts into
consecutive chunks of that size. Ten hosts split by five shows the first five
terminals; ++ctrl+shift+right++ pages through the chunk and then moves on to the next.
The status bar carries `SPLIT 1/2 (5 hosts)` for as long as the split narrows
anything. An empty prompt or `0` clears it; ++esc++ keeps it. It is a view, not
a removal: chunks are cut from the session's host list, and a host that
reconnects reappears in its chunk without a keypress.

**Full screen** — ++alt+z++ zooms the focused pane to the whole main area,
++alt+z++ again returns. Full screen skips the overflow footer: an explicit zoom
has its own way back.

Inside a pane, ++ctrl+a++ is start-of-line and ++ctrl+s++ is flow control for
the remote shell. ++ctrl+s++ is an app-level command only when no pane has the
keyboard; ++ctrl+a++ has no app-level meaning at all — in the broadcast bar it
is the escape prefix (see
[Typing and broadcasting](../guides/broadcasting.md#the-ctrla-prefix)).

## Focus is always visible

The focused frame is drawn with a border that differs in **thickness** as well
as colour, so it survives a terminal without colour — the same rule applies to
every state lazycssh renders: colour is never the only carrier of meaning. A
failing pane says `exit N` in text next to its danger-coloured border.

Focus survives the host list changing. When a session merges in or a host
closes, the focused host is preserved by **identity**, not by position: if the
machine is still in the run it keeps focus at its new index, and only when it is
gone does focus clamp to the nearest pane. A list that shifts under the cursor
must never silently move you onto a different machine.
