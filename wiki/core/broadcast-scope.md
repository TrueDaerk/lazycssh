---
type: decision
title: Broadcast scope
description: What `BROADCAST all` means when a working set is active, and how the target count is made unmissable.
resource: internal/broadcast
tags: [broadcast, working-set, safety]
timestamp: 2026-07-31T13:00:00Z
---

# Broadcast scope

## The question

A run has 40 hosts. The user narrows the working set to 20. They type a command in
`BROADCAST all`. Does it reach 20 machines or 40?

Guessing wrong here sends a command to twice the intended number of production machines, so the
answer is written down rather than left to whoever reads the code next.

## The decision

**`all` means the active working set.**

The working set is the user's stated subject of work. Once they have said "I am working on these
twenty", every subsequent action is about those twenty until they say otherwise. A mode called
"all" that quietly meant "all forty" would make narrowing the working set a decoration.

Reaching every host in the run is a separate mode, `fleet`, with its own binding and its own
rendering. It is the "really every host" escape hatch, not a mode the user can land in by
cycling through the others by accident.

## The modes

| Mode | Targets | Notes |
|------|---------|-------|
| `all` | the active working set | default; equals the whole run when no working set is narrowed |
| `selected` | toggled hosts ∩ working set | hosts selected outside the set are reported by `Excluded` and never receive input |
| `single` | the focused pane | deliberately ignores the working set: one host, visible, for a password prompt or `vim` |
| `fleet` | every host in the run | ignores the working set; always rendered as a warning |

`selected` intersects rather than unions. A selection made before the working set changed must
not quietly widen the blast radius; the excluded hosts stay visibly selected instead, so the
mismatch is on screen rather than in the wire.

## Scope and targets

Two different questions, deliberately kept apart:

| | Question | Method |
|---|----------|--------|
| **Scope** | who is the user addressing? | `Scope()`, `ScopeCount()` |
| **Targets** | who can actually receive right now? | `Targets()`, `Count()` |

Targets are the scope minus every host whose session cannot take input — dialling, failed,
closed. `Unreachable()` names those. A host that is down is excluded from the count *and* from
delivery: a count that included it would promise something the transport cannot do.

In `all` and `selected` mode, targets also exclude every host whose remote app is on the
alternate screen — a keystroke meant for one `vim` must not reach twenty of them.
`AltScreenSkipped()` names those hosts, and the exclusion is spelled out in `Describe`.
`single` is how one talks to the full-screen app, and `fleet` is the explicit every-host
escape hatch; neither excludes. See [terminal emulation](./terminal.md).

The router learns liveness from a `Sessions` interface, satisfied by
[`ssh.Manager`](./manager.md) via `Connected`, `Writer` and `AltScreen`. Until a transport is
attached the router answers about scope only.

## Rendering

`Router.Describe` is the status bar line. With a transport it says what will actually happen:

```
BROADCAST all (7/8 up)
BROADCAST all (6/8 up, 1 alt-screen skipped)
BROADCAST set:front-half (19/20 up)
BROADCAST selected (3/3 up)
BROADCAST single web-01 (1/1 up)
BROADCAST EVERY HOST (38/40 up)
```

Without one there is nothing that could say a host is down, so it reports the scope alone rather
than claiming everything is up: `BROADCAST all (8 hosts)`.

Rules the tests enforce:

- the first number is always `len(Targets())` — the label and the reality cannot drift,
- a narrowed working set is named in the label (`set:...`), so `all` never appears while fewer
  than every host is addressed,
- `fleet` renders as `EVERY HOST` and sets `Router.Warning`, which the status bar draws in the
  warning style.

## The visibility limit

`SetLimit(ids)` restricts `all` and `selected` to the given hosts; `nil` lifts the limit, and
an empty non-nil limit means "nothing is visible", not "no limit". The UI pushes it — it is the
only layer that knows which panes are on screen — whenever the foreground
[session](./groups-and-sessions.md) changes; the router only enforces that a keystroke cannot
reach past it.

The limit deliberately does **not** bound `fleet` mode: that mode exists as the explicit
every-host escape hatch, and an escape hatch that can be silently narrowed is not one. `single`
mode needs no limit — the focused pane is always visible.

## What the scope does not cover

Typing into a focused pane goes through `PaneWriter` — one host's writer, directly — and never
through the router. The scope governs broadcast sends — the `:` command line and the live
broadcast bar (`6`) — not the terminal behaviour of a single pane; a keystroke typed into
`web-01` cannot fan out, whatever mode is active.

## Forgetting hosts

`Forget(ids...)` drops every trace of hosts that left the run: their selection entries, and the
focus if it pointed at one of them. Selection state that outlives its host over-reports counts
and prints a dead host in the single-mode label; the program calls `Forget` whenever it removes
a host.

## Switching modes

One keystroke each, from anywhere in the interface:

| Key | Mode |
|-----|------|
| `b` | `all` |
| `B` | `selected` |
| `s` | `single`, and the focused pane becomes the target |
| `ctrl+alt+b` | `fleet` |

`single` is instant and unmistakable because that is what a `sudo` prompt needs: one key, and the
status bar names the single host it now sends to.

## Building the selection

The `selected` scope is built either by hand with the keys or by pattern from the command
line, and the two do the same things. The single-letter keys work at the app level; `alt+space`
works from the grid too, like the other pane chords:

| Key | Command line | Effect |
|-----|--------------|--------|
| `alt+space` | — | toggle the focused pane's host |
| `a` | `/select all` | every host in the run |
| — | `/select set` | the active working set |
| `u` | `/select up` | the hosts that can take input |
| `d` | `/select down` | the hosts that cannot — "show me what broke" |
| `i` | `/select invert` | flip the selection |
| `c` | `/select none` | clear it |
| — | `/select web-*` | every host matching a glob |
| — | `/deselect web-1*` | the same, in reverse |

A pattern matches across the **whole run**, not only the working set: a selection is a statement
about machines, and `web-*` must not mean different things at different times. Hosts selected
outside the working set stay selected and are reported by `Excluded()` rather than silently
dropped.

Command-line instructions start with `/` on purpose. `select` is a shell builtin, and a line that
means "run this on forty machines" must never be intercepted because it happens to start with a
word this program knows. A `/`-prefixed line is never sent to a host and never recorded as a
command.

The status bar count follows the selection as it changes, so `BROADCAST selected (3/3 up)` is
always the number of machines the next keystroke reaches.

## Sending

`Router.Send` writes to exactly the targets and returns a `Delivery`: the mode, the scope size,
how many could receive, how many did, and which hosts did not.

One host that refuses a write does not stop the others — a broken pipe on one machine is one dead
pane, never a command that half the fleet missed without anyone saying so. `Delivery.String`
always reports against the **scope**:

```
sent to 40/40 hosts
sent to 7/40 hosts (33 did not receive it)
```

Two silences were closed for issue #133 — a broadcast that intermittently wrote nothing while
looking accepted:

- A target whose writer vanished between `Targets()` and the write (the session dropped in
  between) is a **failed delivery with an error**, not a skipped host: `Send` records
  `write to <host>: session lost its writer` in `Delivery.Errs`, so the caller cannot read the
  send as a success.
- The broadcast bar reports **zero-delivery** even without an error: a keystroke whose
  `Delivered` is 0 — empty scope, every host down — sets the status line to
  `sent to 0/N hosts … — no host can take input right now` instead of staying quiet. Typing
  into the void must not look like typing.

The count in the bar's title, the targets `Send` resolves and the delivered count all come from
the same `Router` inside the same `Update` pass; the status line after each keystroke is what
makes a mismatch visible the moment it happens.

## What this package does not do

The router does not own sessions, dial, or read output. It resolves scope, filters by liveness
and writes bytes it is handed — see [Working sets](./working-sets.md) for the set model and
[Session manager](./manager.md) for the sessions themselves.
