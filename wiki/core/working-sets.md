---
type: concept
title: Working sets
description: The current subject of work — which hosts a command is about, defined by count, range, pattern or selection.
resource: internal/workingset
tags: [working-set, hosts, broadcast]
timestamp: 2026-07-29T19:00:00Z
---

# Working sets

With forty hosts the user rarely works on forty hosts. The **working set** is the answer: a
definition of which hosts are the current subject of work.

Two concepts that must not be confused:

| Concept | Question it answers | Lives in |
|---------|---------------------|----------|
| Working set | Which hosts is this command about? | `internal/workingset` |
| Render window | How many panes fit on the screen right now? | the UI grid |

They are separate types on purpose. A user who reads "20 panes" as "20 targets" while forty
hosts are being broadcast to has been given a footgun by the tool, not by their own carelessness.

## Definitions, not snapshots

A working set stores a `Selector`, never a captured list of host identifiers. Members are
recomputed from the live host list on every call, so a set defined as `web-*` still means that
after a host is added, and a set defined as `21-40` still means those positions after the fleet
is re-sorted.

| Selector | Spec the user types | Meaning |
|----------|---------------------|---------|
| `All` | `all`, `*` | every host in the run |
| `Range` | `20`, `first 20`, `21-40` | positions in the host list, 1-based and inclusive |
| `Pattern` | `web-*`, `db-01` | shell glob against the host identifier |
| `Manual` | `selection` | the hosts currently toggled with `alt+space` |

`ParseSelector` reads all of these. A malformed glob or a reversed range is an error, not a
pattern that silently matches nothing — the same rule host expansion follows
(see [Host argument expansion](./host-expansion.md)).

Bounds are clamped at evaluation, not at construction: `21-40` against a fleet that shrank to 25
hosts yields five members rather than an error.

## Paging

`Next` and `Prev` shift a positional set by exactly its own size — "show me the first 20", then
"now the next 20". The chunk size is the size of the *definition*, not of the current result, so
a short last page (`21-40` over 25 hosts, five members) still pages back to a full first page.

Paging only applies to a `Range`. A pattern or a manual selection has no next chunk, and both
`Next` and `Prev` report `false` rather than inventing one.

`Next` refuses to move past the end of the host list, and `Prev` clamps to position 1 instead of
going negative. Neither wraps around: wrapping would silently send the next command to the hosts
at the other end of the fleet.

## Named sets

`Save` names the active definition; `Activate` returns to it. Paging produces an **ad hoc** set:
the stored definition is left untouched and the active name is cleared, so a set named
`front-half` still means the first twenty hosts after the user has paged three chunks away.

`Remove` deletes a definition without moving the user: the active selector keeps working, it
merely loses its name.

## Rendering

`Describe` is what the status bar shows:

```
front-half (20/40 hosts)
21-40 (20/40 hosts)
all (40/40 hosts)
```

The size is never rendered without the fleet total. `20` on its own reads as "all twenty hosts"
to a user whose run has forty, and that misreading is exactly the one that sends a command to
the wrong machines.
