---
type: reference
title: Terminal emulation
description: The per-session vt emulator — what it tracks, why it drains its own reply pipe, and how a pane renders a full-screen app's live grid.
resource: internal/term
tags: [terminal, vt, emulation, alt-screen]
timestamp: 2026-07-31T00:00:00Z
---

# Terminal emulation

`internal/term` wraps the charmbracelet `x/vt` emulator, one instance per session. The session's
reader goroutine feeds it the same raw bytes that go into the
[scrollback buffer](./scrollback.md); the UI reads screen state back. This is the foundation for
running full-screen apps (`vim`, `htop`, `less`) in a pane — epic #44 — without giving up the
scrollback rendering that ordinary command output keeps.

## What it exposes

| Method | Purpose |
|--------|---------|
| `Write` | feed session output; satisfies `io.Writer`, never fails |
| `IsAltScreen` | whether the remote app switched to the alternate screen — the signal that a full-screen app is running |
| `Render` | the visible screen as styled text, one line per row |
| `CursorPosition` | cursor cell, zero-based |
| `Resize` / `Size` | kept in lockstep with the pane geometry and the remote PTY |
| `SetReplyHandler` | receives the emulator's answers to terminal queries |
| `Close` | idempotent; owned by the session |

Dimensions below one cell are clamped on `New` and `Resize`, so a pane that has not been
measured yet cannot produce a zero-size grid.

## The reply pipe must always be drained

Full-screen apps query the terminal on startup — device attributes (`CSI c`), cursor position
reports — and the vt emulator answers through an **unbuffered pipe whose `Write` blocks until
the answer is consumed**. The wrapper therefore runs its own drain goroutine from `New`: without
it, the first `vim` would freeze the session's reader goroutine mid-`Write`.

With no handler registered, replies are drained and dropped. `SetReplyHandler` hands them to a
callback (called from the drain goroutine, which owns the slice it passes) — piping them back
into the session's stdin so apps actually start cleanly is #157.

`Close` shuts the reply pipe down directly instead of calling the emulator's own `Close`, which
flips an unsynchronized flag the drain goroutine is concurrently reading — an upstream data
race the race detector flags.

## Where it sits in the session

Every `Session` — real and fake — owns an emulator and exposes it via `Terminal()`. The output
pump tees each read into scrollback and emulator; `Resize` resizes both the remote PTY and the
emulator. Broadcast exclusion of alt-screen panes is #158.

## Grid rendering in the pane

A pane whose emulator reports alt-screen renders the live grid instead of scrollback text:
the emulator screen clipped to the pane body, with the remote app's cursor drawn where it says
it is — and hidden when the app hides it (`CSI ?25l`), the way vim does while repainting. No
tail, no scroll offset, no search, no text selection: the remote app owns the whole screen,
exactly as it would in a plain terminal. Scrolling is a no-op while the grid is active, so the
offset cannot jump when the app exits.

Leaving the alternate screen returns to the scrollback view. The tail shows the post-app
screen — cleared, like a terminal after vim quits, per the
[scrollback](./scrollback.md) clear semantics — and the history from before the app stays
reachable by scrolling.

The grid clips defensively to the pane body: an emulator resize can lag one frame behind the
layout, and a too-large grid must not push the frame apart.

Legacy alternate-screen mode `?47` is not implemented by the vt emulator; every terminfo in
current use emits `?1049` (or `?1047`), which are tracked.

## Cost

One emulator is a cell grid of the pane's size plus a bounded scrollback of its own (vt default
10k lines, same figure as the scrollback design default). Twenty panes cost a few megabytes;
no lazy initialization is needed.
