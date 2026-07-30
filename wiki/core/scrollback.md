---
type: reference
title: Scrollback buffer
description: The bounded per-session ring buffer that keeps a chatty host from stalling the UI.
resource: internal/scrollback/scrollback.go
tags: [backpressure, concurrency, output]
timestamp: 2026-07-31T01:00:00Z
---

# Scrollback buffer

Every session writes its output into a bounded, line-oriented ring buffer. The buffer exists to
enforce one rule from the design constraints: **a chatty host must never stall the UI.**

`Write` takes a mutex, never blocks on a reader, never allocates without bound and never fails.
There is no error a session reader goroutine could usefully react to, and blocking it would
apply backpressure to the remote host — the opposite of what is wanted.

## Capacity and dropping

Default `10,000` lines per session. When full, the oldest line is evicted and a counter is
incremented, so truncation is visible in the UI rather than silent:

```go
b := scrollback.New(scrollback.DefaultCapacity)
b.Len()      // complete lines retained
b.Dropped()  // lines evicted to stay within capacity
b.Written()  // total bytes ever written, including dropped output
```

A capacity below one is raised to the default, so an unset config field cannot silently discard
all output.

## Line handling

- The line being assembled is a **logical line with a cursor** (issue #178). On the remote
  terminal it occupies `len/width` screen rows — the session keeps the buffer's width in
  lockstep with the PTY via `SetWidth` — and printable runes **overwrite** the cell under the
  cursor, exactly as a terminal would.
- `\n` commits the line when the cursor is on its last screen row. On an upper row it only
  moves the cursor down one row: that is readline stepping through a multi-row edit, not
  output. An unterminated line is returned by `Lines()` as the last element — a shell prompt
  has no trailing newline, and it must appear the moment it arrives.
- A **bare `\r`** returns the cursor to column zero of its current screen row; the following
  output overwrites in place. Progress bars and spinners redraw over themselves instead of
  filling the scrollback with intermediate frames.
- A single line is capped at 64 KiB and committed as if a newline had arrived. A binary catted
  by accident would otherwise grow one string without limit.
- The **line discipline** honours exactly what a remote readline needs to redraw a recalled
  command — including one that wraps over several screen rows — without leaving its
  intermediate states behind:
  - **backspace** moves the cursor one cell left (it erases nothing; the following overwrite
    or erase sequence does that), and it never reaches a committed line,
  - **cursor movement** — `ESC[A`/`ESC[B` (up/down, mapped through the width), `ESC[C`/`ESC[D`
    (forward/back, clamped to the row) and `ESC[G` (column) — repositions the cursor on the
    pending line; without a known width the vertical forms are dropped,
  - **`ESC[K`** erases within the cursor's screen row (`0` right of the cursor, `1` left of
    it, `2` the whole row); blanked trailing cells stop counting as content unless the cursor
    stays among them, which is what separates erase debris from spaces the host printed,
  - **`ESC[J`** with `0` erases the pending line past the cursor (the display below it).
- **OSC and the other string sequences** (title changes, the shell-integration exit markers)
  are consumed and dropped — they carry metadata, never text, and flushing their payload into
  the line is how it would corrupt the scrollback.
- **Clear-screen sequences plant a marker** (issue #131). `ESC[2J` / `ESC[3J` (erase display),
  `ESC[1J` (erase above), the alternate-screen switches `ESC[?1049h/l`, `ESC[?1047h/l`,
  `ESC[?47h/l`, a full reset (`ESC c`, RIS) and the minimal-termcap clear idiom `ESC[H`
  immediately followed by `ESC[J` — cursor-home + erase-below, what busybox `clear` emits
  (issue #189) — store a `ClearMark` line where the visible area restarted; the whole-screen
  forms also discard the line being assembled, the erase-above form keeps it. Consecutive
  markers collapse, so a program clearing every frame cannot fill the ring with markers.
  The history is **preserved, not wiped** — deliberately including `ESC[3J`, whose strict
  meaning is "erase the scrollback too": on a fleet tool, history is worth more than strict
  emulation. The pane renders the marker as `~ screen cleared ~`; while following the tail it
  shows only what came after the last marker, so `clear` (or entering `screen`/`vim`) leaves
  an apparently empty pane, and scrolling up still reaches everything before it.
- SGR and every unrecognized CSI sequence are stored verbatim at the cursor's position, zero
  cells wide, including sequences split across writes. Interpreting them is the renderer's
  job; full emulation is a separate idea (issue #44).
- **The cursor is exported** (issue #190): `CursorTail` reports the cursor's wrapped row and
  column relative to the end of the snapshot. A connected pane following the tail draws the
  remote cursor from this — at the end of the prompt, inside a readline edit, or on the empty
  row after a line feed — with no emulation involved. No cursor while scrolled back, on a
  disconnected pane, or on the alternate screen, where the emulator grid draws its own.

Chunk boundaries are handled: a line split across writes, a CRLF split between two writes, an
escape sequence split anywhere, and a UTF-8 rune split mid-sequence all reassemble correctly.

## Guarantees under test

- `-race` clean with four writer goroutines and a reader taking snapshots concurrently.
- 40 MB of input through a 1,000-line buffer leaves the heap under 16 MB — the memory bound is
  asserted, not assumed.
- Benchmarks for `Write` and `Lines` guard against regressions.
