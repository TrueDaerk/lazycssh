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

## Rendering

`Router.Describe` is the status bar line. The target count and the fleet total are always shown
together:

```
BROADCAST all (40/40 hosts)
BROADCAST set:front-half (20/40 hosts)
BROADCAST set:21-40 (20/40 hosts)
BROADCAST selected (3/40 hosts)
BROADCAST single web-01 (1/40 hosts)
BROADCAST EVERY HOST (40/40 hosts)
```

Rules the tests enforce:

- the rendered count is always `len(Targets())` — the label and the reality cannot drift,
- a narrowed working set is named in the label (`set:...`), so `all` never appears while fewer
  than every host is targeted,
- `fleet` renders as `EVERY HOST` and sets `Router.Warning`, which the status bar draws in the
  warning style.

## What this package does not do

The router answers "who receives this". It holds no sessions and writes nothing. Key bindings,
the confirmation in front of `fleet`, and the actual fan-out live above it — see
[Working sets](./working-sets.md) for the set model and [Session manager](./manager.md) for the
sessions themselves.
