---
type: decision
title: Broadcast scope
description: What `BROADCAST all` means when a working set is active, and how the target count is made unmissable.
resource: internal/broadcast
tags: [broadcast, working-set, safety]
timestamp: 2026-07-28T00:00:00Z
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

The router learns liveness from a `Sessions` interface, satisfied by
[`ssh.Manager`](./manager.md) via `Connected` and `Writer`. Until a transport is attached the
router answers about scope only.

## Rendering

`Router.Describe` is the status bar line. With a transport it says what will actually happen:

```
BROADCAST all (7/8 up)
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

## What this package does not do

The router does not own sessions, dial, or read output. It resolves scope, filters by liveness
and writes bytes it is handed — see [Working sets](./working-sets.md) for the set model and
[Session manager](./manager.md) for the sessions themselves.
