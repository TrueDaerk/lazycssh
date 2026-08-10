---
type: reference
title: Terminal emulation
description: The per-session vt emulator that holds everything a pane shows — screen, retained history, cursor, modes — encodes key presses per host, and reflows on resize.
resource: internal/term
tags: [terminal, vt, emulation, alt-screen, scrollback, keys, resize]
timestamp: 2026-08-10T14:00:00Z
---

# Terminal emulation

`internal/term` wraps the charmbracelet `x/vt` emulator, one instance per session. Since
issue #206 it is the **single source of pane state**: the session's reader goroutine feeds
raw output bytes in, and everything a pane renders comes back out — the visible screen, the
history that scrolled off it, the cursor, the alternate-screen flag. The former hand-rolled
line discipline (`internal/scrollback`) is gone; redraws, cursor movement and erase sequences
are handled by a real VT implementation instead of case-by-case emulation.

## Output: what the emulator holds

| Call | Answers |
|------|---------|
| `Write` | feeds session output; never fails, never blocks on a reader |
| `Render` | the visible screen as styled text, one line per row |
| `HistoryLen` / `HistoryLine(i)` | the retained lines that scrolled off the screen, oldest first |
| `HistoryFull` | the retention cap was reached — older output has been dropped |
| `Text` | history + screen as plain text: what a user would read, for tests and the clipboard |
| `TextLineCount` | how many lines `Text` would return, without building them |
| `TailText(n)` | the last n lines of `Text`, without materializing the rest |
| `IsAltScreen` | a full-screen app (vim, htop) owns the pane |
| `CursorPosition` / `CursorVisible` | where the remote cursor is and whether the app wants it drawn |
| `HasOutput` | has this session said anything yet — decides whether injected text needs a leading line break |

The history is bounded (the vt default of 10k lines, `SetHistorySize` to change); when it
overflows, the oldest lines are dropped and the writer never blocks — the backpressure rule
survives the redesign unchanged. Trailing blank cells are not retained, so a prompt's
trailing space does not survive into `Text` — terminals lose it too.

**Retention is compact (issue #277).** Only a small working depth (128 lines) of scrolled-off
content stays in the vt scrollback as full cell grids; everything older is drained on the
write path into a bounded ring of pre-rendered styled strings (`internal/term/compact.go`) —
one string per line, the exact string `HistoryLine` returns, rendered once at drain time
instead of on every read. The cell representation costs ~128 B per rune; the string costs
the bytes of the line. Measured as in the #274 audit, one host at the 10k-line cap retains
~8 MB instead of ~75 MB, and because the vt scrollback never reaches its own cap, its
O(lines) oldest-line eviction (a memmove of the whole line-header array per scrolled line)
never runs — the allocator no longer dominates the ingest profile. The cap stays exact:
overflow beyond it is trimmed at every write and resize boundary by dropping compact
strings, an O(1) eviction each. The trade: compact lines are frozen at the width they
scrolled off with (see the resize section below). `HistoryLen`/`HistoryLine`/`Text`/
`TailText` read across the boundary transparently, and a reconnect hands the whole emulator
— ring included — to the next session as before.

**`Text` is expensive; per-event readers use the bounded calls.** Rendering the retained
history costs milliseconds and megabytes per call at the cap — the performance audit (issue
#274) measured ~15 ms and ~4 MB of allocations for one full `Text` — so anything that runs
per frame or per output event asks for what it actually needs: `TextLineCount` for a
watermark, `TailText(n)` for a bounded window. `Text` itself remains for the real
whole-content consumers: the clipboard, the export, tests.

**Clear keeps the history.** `ED 2` pushes the visible rows into the retention before
clearing (the xterm behaviour), so a remote `clear` empties the pane while scrolling up still
reaches everything. The `ESC[3J` many terminfos append — erase scrollback — is filtered out
of the byte stream by a boundary-safe guard in `Write`: on a fleet tool the history is worth
more than strict emulation. The guard is the one deliberate deviation from a real terminal.

## Input: SendKey and Paste

`SendKey(KeyEvent)` encodes one key press **the way this terminal's current modes demand** —
application cursor keys, keypad mode — and `Paste` honours bracketed-paste mode. The encoded
bytes come out of the reply pipe, the same path that carries the emulator's answers to
terminal queries (DA1, DSR); the session wires that pipe to its own stdin. This replaces the
hand-written key table the UI used to maintain: what reaches the shell is decided by the
emulator that knows the host's state, so the `opt+arrow`/`cmd+backspace` class of bugs
(issues #202, #206) cannot recur key by key.

## The reply pipe must always be drained

The reply pipe is unbuffered and the emulator's `Write` blocks until it is consumed, so `New`
starts a drain goroutine unconditionally — without it the first `vim` would freeze the
session's reader mid-`Write`. With no handler registered, replies are drained and dropped.
`SetReplyHandler` hands them to a callback, called from the drain goroutine, which owns the
slice it passes. Both session implementations wire the handler to their own stdin — and only
their own: a reply (or an encoded keystroke) answers the one host it belongs to and never
travels through broadcast. Rebinding the handler is how a reconnected pane's emulator points
at the new session's stdin.

`Close` shuts the reply pipe down directly instead of calling the emulator's own `Close`,
which flips an unsynchronized flag the drain goroutine is concurrently reading — an upstream
data race the race detector flags.

## Resize: reflow, not truncate

Upstream `vt.Resize` hard-truncates: columns beyond the new width and rows beyond the new
height are simply gone. A fleet grid resizes on every host join, leave and window change, so
`internal/term/resize.go` (ported from ike's integrated terminal) fixes the semantics:

- **width change** (primary screen): the whole content is reconstructed as logical lines —
  soft-wraps detected by the occupied-final-column heuristic, hard breaks remembered by a
  reflow cache so repeated shrink/grow cycles stay lossless — and replayed through the parser
  at the new width, as if the terminal had always been that size. The shell's live edit line
  replays verbatim so its own redraw finds the geometry it remembers.
- **height shrink**: rows above the cursor scroll into the history (what a real terminal
  does) instead of the bottom rows being truncated; a later grow pulls them back while they
  are still the newest history lines.
- **height grow**: a reserve of the fullest known row contents restores cells a plain
  truncate clipped, guarded by content prefix-matches so rewritten rows are never corrupted.

The alt screen never reflows: its apps repaint themselves on the window change the session
forwards. Dimensions below one cell are clamped on `New` and `Resize`.

Reflow covers the vt working depth plus the screen — not the compact history. Compact lines
are frozen at the width they scrolled off with: re-materializing cells on resize would
resurrect exactly the retention cost the drain removed, so a width reflow is O(working
depth), never O(retention cap). A frozen line wider than the pane is clipped at render time
(the pane truncates every window line); one narrower simply stays narrower. Real terminals
without reflow behave like this for their entire scrollback — here only lines older than
the working depth do.

## Where it sits in the session

Every `Session` — real and fake — owns an emulator and exposes it via `Terminal()`. The
output pump feeds it after the exit-marker scanner and the setup-echo filter have seen the
raw bytes; `Resize` resizes both the remote PTY and the emulator. The
[broadcast router](./broadcast-scope.md) reads `IsAltScreen` through the manager's
`AltScreen` method, and delivers key events through the manager's `SendKey` — encoded per
host, by that host's emulator.

The emulator outlives its session: `Session.ReleaseTerminal` detaches it, the reconnect
separator is written into it, and the replacement session adopts it (`Config.Terminal`) — the
pane keeps its history across reconnects. Auth prompts and failure notices are injected as
output through the same `Write` path (`internal/program/authecho.go`, the manager's failure
notice).

## Grid rendering in the pane

A pane whose emulator reports alt-screen renders the live grid instead of the history view:
the emulator screen clipped to the pane body, with the remote app's cursor drawn where it
says it is — and hidden when the app hides it (`CSI ?25l`). No tail, no scroll offset, no
search, no text selection: the remote app owns the whole screen. Scrolling is a no-op while
the grid is active, so the offset cannot jump when the app exits.

Leaving the alternate screen returns to the history view; what was on the primary screen
before the app is restored by the emulator, and the history stays reachable by scrolling.

Legacy alternate-screen mode `?47` is not implemented by the vt emulator; every terminfo in
current use emits `?1049` (or `?1047`), which are tracked.

## Concurrency and cost

All multi-step reads (a resize snapshotting rows, a history line being built) take the
wrapper's own grid mutex against `Write`; the vt `SafeEmulator` lock only covers single
calls. The reply drain never takes that mutex, so a query answered mid-`Write` cannot
deadlock.

One emulator is a cell grid of the pane's size, a cell working depth and a compact string
history — ~8 MB at the full 10k-line cap, most of it the working depth's cells. Forty panes
at the cap fit in a few hundred megabytes where they previously cost ~3 GB; no lazy
initialization is needed.
