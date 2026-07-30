---
type: reference
title: Terminal emulation
description: The per-session vt emulator — what it tracks, why it drains its own reply pipe, and how it will carry full-screen apps.
resource: internal/term
tags: [terminal, vt, emulation, alt-screen]
timestamp: 2026-07-30T00:00:00Z
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
emulator. The emulator does not change what any pane renders yet: alt-screen grid rendering is
#156, broadcast exclusion of alt-screen panes is #158.

Legacy alternate-screen mode `?47` is not implemented by the vt emulator; every terminfo in
current use emits `?1049` (or `?1047`), which are tracked.

## Cost

One emulator is a cell grid of the pane's size plus a bounded scrollback of its own (vt default
10k lines, same figure as the scrollback design default). Twenty panes cost a few megabytes;
no lazy initialization is needed.
