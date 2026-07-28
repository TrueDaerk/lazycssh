---
type: reference
title: Scrollback buffer
description: The bounded per-session ring buffer that keeps a chatty host from stalling the UI.
resource: internal/scrollback/scrollback.go
tags: [backpressure, concurrency, output]
timestamp: 2026-07-28T00:00:00Z
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

- `\n` ends a line; a preceding `\r` is removed, so CRLF is one ending.
- A **bare `\r`** discards the line assembled so far. This is how progress bars and spinners
  redraw; keeping every frame would fill the scrollback with intermediate states of one line.
- An unterminated line is returned by `Lines()` as the last element. A shell prompt has no
  trailing newline, and it must appear the moment it arrives rather than when the next line
  completes.
- A single line is capped at 64 KiB and committed as if a newline had arrived. A binary catted
  by accident would otherwise grow one string without limit.
- ANSI escape sequences are stored verbatim. Interpreting them is the renderer's job.

Chunk boundaries are handled: a line split across writes, a CRLF split between two writes, and a
UTF-8 rune split mid-sequence all reassemble correctly.

## Guarantees under test

- `-race` clean with four writer goroutines and a reader taking snapshots concurrently.
- 40 MB of input through a 1,000-line buffer leaves the heap under 16 MB — the memory bound is
  asserted, not assumed.
- Benchmarks for `Write` and `Lines` guard against regressions.
