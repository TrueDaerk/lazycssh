---
type: reference
title: Session manager
description: Owning a fleet of sessions — bounded fan-out dialling, a single event channel, per-host reconnect and close.
resource: internal/ssh/manager.go
tags: [ssh, transport, concurrency, fleet]
timestamp: 2026-08-10T05:00:00Z
---

# Session manager

The manager owns one [session](./session.md) per host and funnels everything into a single
channel the UI drains with one command.

## Dialling

Every host is dialled in its own goroutine, bounded by a semaphore — `MaxParallelDials`,
default 16. Two hundred hosts must not mean two hundred simultaneous sockets, file descriptors
and key exchanges.

`Start` returns immediately. It never waits for the fleet, because the UI must draw while hosts
are still connecting. `Wait` exists for tests and shutdown, not for the UI.

**One slow host delays only itself.** A hung machine holds its own goroutine and its own
semaphore slot, and nothing else. The test for this hangs one host for thirty seconds and
asserts the other nineteen connect anyway.

**One failing host fails only itself.** `Start` errors are not propagated to the fleet: the
session has already recorded the error and reported it, and a fleet-wide error return would say
nothing about which host failed. `Counts()` and `ByState(StateFailed)` are how failures surface.

**A running fleet can grow.** `Add` appends a session for one more host and dials it, without
touching the existing sessions. Merging a saved session into a run lands here; its identifier
goes through the same disambiguation as everyone else's. So does **clone** (`alt+shift+c` on a
focused pane, issue #253): the UI reads the focused session's already-resolved `Host` — same
Addr/User/Port, no second round through `~/.ssh/config` — and hands it straight back to `Add`.
The clone is a second, fully independent session under its own identifier: its own input,
scrollback, close and reconnect, and the broadcast router picks it up like any other session
because it reads the fleet live rather than a set captured at connect time.

## Identity

Session identifiers come from the host alias, because that is what the user recognises. Aliases
can repeat — the same host listed twice, two patterns overlapping, or a clone of a host already
in the run — so duplicates become `srv1`, `srv1#2`, `srv1#3`. Order always follows what the user
typed; `SortedIDs` exists for displays that want alphabetical instead.

## Reconnect

`Reconnect` builds a *fresh* session for the same host and swaps it in under the same
identifier, keeping its position in the list, so panes and selections survive. The old session
is closed outside the lock — closing waits for reader goroutines, and holding the lock through
that would stall the UI.

**The scrollback survives.** The pane that just died is exactly the pane whose last lines the
user wants to read, so the buffer is handed to the new session rather than replaced. A separator
is written into it first, so the two connections cannot be read as one continuous stream:

```
error: disk full
── reconnecting to srv1 at 2026-07-28T12:00:00Z ────────
Welcome to srv1
```

Reconnecting works from every end state — failed, closed, a remote shell exit — and from a
session that is still connected. It touches no other session.

**No re-prompt.** Credentials live in the [`Credentials`](./authentication.md) cache held by the
factory closure, not in the session, so redialling reuses a password already in memory instead
of asking again. Three reconnects, one prompt — asserted against the real transport, since a
fake never touches the credential cache.

**Reconnecting the whole fleet at once.** `ReconnectAll` re-dials every session currently
`StateFailed` or `StateClosed` and leaves everything else — connected, still dialling — untouched
(issue #244). It is the bulk form of `Reconnect`: a network blip or a jump-host restart can drop
dozens of hosts together, and reconnecting them one pane at a time does not scale. Each host goes
through the same `Reconnect` call a single redial uses, so it keeps the dial semaphore and the
scrollback handoff, and one host's redial failing is recorded on that host alone. It returns the
identifiers it redialed; with nothing down, it redials nothing.

`Close(id)` ends one session and leaves the rest running: one dead host is one dead pane.

`Remove(id)` takes a session out of the fleet entirely — closed and no longer listed, so its
pane disappears. Close leaves a dead pane on screen to be read; Remove is the user saying they
are done with it. The session is closed outside the lock, like Reconnect, so waiting for its
reader goroutines cannot stall the UI. Whoever removes must also tell the working set
(`SetHosts`) and the broadcast router (`Forget`) — the program does.

## Fleet summary

`Counts()` gives the status bar its numbers — connected, pending, failed, closed — and renders
as `40/40 up`. It is computed from the sessions on demand rather than tracked incrementally,
so it cannot drift out of sync with reality.
