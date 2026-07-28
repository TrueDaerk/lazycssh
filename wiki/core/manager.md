---
type: reference
title: Session manager
description: Owning a fleet of sessions — bounded fan-out dialling, a single event channel, per-host reconnect and close.
resource: internal/ssh/manager.go
tags: [ssh, transport, concurrency, fleet]
timestamp: 2026-07-28T00:00:00Z
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

## Identity

Session identifiers come from the host alias, because that is what the user recognises. Aliases
can repeat — the same host listed twice, or two patterns overlapping — so duplicates become
`srv1`, `srv1#2`, `srv1#3`. Order always follows what the user typed; `SortedIDs` exists for
displays that want alphabetical instead.

## Reconnect

`Reconnect` builds a *fresh* session for the same host and swaps it in under the same
identifier, keeping its position in the list, so panes and selections survive. The old session
is closed outside the lock — closing waits for reader goroutines, and holding the lock through
that would stall the UI.

`Close(id)` ends one session and leaves the rest running: one dead host is one dead pane.

## Fleet summary

`Counts()` gives the status bar its numbers — connected, pending, failed, closed — and renders
as `40/40 up`. It is computed from the sessions on demand rather than tracked incrementally,
so it cannot drift out of sync with reality.
