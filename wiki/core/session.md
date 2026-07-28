---
type: reference
title: SSH session lifecycle
description: One host, end to end — dial, handshake, PTY, streams, resize and close — and the event contract the UI depends on.
resource: internal/ssh/session.go
tags: [ssh, transport, concurrency, lifecycle]
timestamp: 2026-07-28T00:00:00Z
---

# SSH session lifecycle

A `Session` is one host: dial, authenticate, request a PTY, run a login shell, pump output into
the [scrollback buffer](./scrollback.md), accept keystrokes, resize, close.

Nothing in the transport touches the bubbletea model. Sessions report over an event channel; the
UI drains it with a command and converts events into messages, so model mutation stays inside
`Update`.

## States

```
pending -> dialing -> authenticating -> connected -> closed
                 \          |               |
                  +---------+---------------+--> failed
```

`closed` is the ordinary end, including when the remote shell exits with a non-zero status —
that is a shell ending, not a transport failure. `failed` means the session could not be
established or the transport broke. A session that has failed does not move on.

## The event contract

Two event types, and one rule that governs both:

| Event | Meaning |
|-------|---------|
| `OutputEvent` | new output is available in the session's scrollback |
| `StateEvent` | the session changed state; `Err` is set for `failed` |

**Events are hints, never the source of truth.** They are delivered with a non-blocking send and
dropped when the consumer is behind. A consumer that receives any event must read `State()`,
`Err()` and `Scrollback()` rather than reconstructing state from the event stream.

This is not a convenience. A blocking send deadlocks `Start` against a full channel: `Start`
emits `dialing` and `authenticating` back to back, and with nobody draining, the second send
never returns. `TestSessionSurvivesAnUndrainedEventChannel` exists to keep that from coming
back.

Dropped events are counted, so a UI that appears stuck can be diagnosed instead of guessed at.

Output bytes never travel in events. They go into the scrollback, and the event says only that
there is something new — otherwise a chatty host would push backpressure through the event
channel into the UI.

## Host key verification cannot be skipped by omission

A `Config` without a `HostKeyCallback` is refused with `ErrNoHostKeyCallback` before any socket
is opened. Defaulting a missing callback to "accept anything" would turn a forgotten struct
field into a silent security downgrade. Opting out has to be an explicit callback that accepts
everything.

## Streams

stdout and stderr land in the same scrollback buffer, interleaved in arrival order, because that
is how they appear on a terminal.

`Close` is idempotent, safe on a session that never started, and waits for the reader goroutines
before returning — which is what makes "no goroutine leaks" testable rather than aspirational.

`Resize` before `Start` is remembered and applied when the PTY is requested.

## The fake

`ssh.Fake` implements the same interface without opening a socket. Everything above the
transport uses it, so no test in this repository needs a network or a fixture host.

Scripted before `Start` — `ConnectDelay`, `DialErr`, `AuthErr`, `Banner`, `EchoInput`,
`Responses` — and driven afterwards:

```go
f := ssh.NewFake("s1", host, events)
f.Responses = map[string]string{"hostname": "srv1\r\n"}
f.Start(ctx)
f.Emit("...")            // output as if from the remote
f.Flood(20000)           // overwhelm the scrollback
f.Disconnect(err)        // drop mid-session
f.ExitWithStatus(3)      // remote shell exits non-zero
f.Written()              // what a broadcast actually delivered
f.Resizes()              // that a terminal resize reached this session once
```

A response only fires on a completed line, so typing a command character by character behaves
the way a real shell does. The fake mirrors the real session's refusal to block on an undrained
event channel; a fake that blocked would hide the deadlock the real one is built to avoid.

## Testing

The real implementation is tested against an in-process SSH server on the loopback interface
(`testserver_test.go`): a real handshake, a real PTY request, real window-change requests, a
real shell. No test in this repository reaches the network, and none needs a fixture host.

The test server echoes input as a PTY does, translating a bare carriage return into CRLF. Any
other choice would be misleading: the scrollback treats a bare `\r` as a line redraw, so an
echo without the newline would vanish exactly as a progress bar frame does.
