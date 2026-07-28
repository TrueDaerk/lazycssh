---
type: reference
title: Issue types
description: The four issue type labels used in lazycssh — epic, idea, bug, enhancement — and what each one commits to.
tags: [github, issues, labels, process]
timestamp: 2026-07-28T00:00:00Z
---

# Issue types

Every issue carries exactly **one** type label. The label answers two questions at a glance:
how big is this, and is it going to happen at all.

| Label | Meaning | Committed to ship? |
|-------|---------|--------------------|
| `epic` | Large issue with many sub-issues. New feature areas or big reworks. | Yes, as a whole |
| `idea` | A proposal. May never be implemented. | No |
| `bug` | Something is broken. | Yes |
| `enhancement` | Small new feature, or an improvement to an existing one. | Yes |

Other labels (`documentation`, `good first issue`, `help wanted`, `question`, …) are free to
combine with a type label — they describe an aspect, not the kind of work.

## `epic`

A body of work too large for one branch and one PR. An epic is not implemented directly; it is
split into sub-issues that each get their own branch and PR. Use it for a whole new feature area
(broadcast modes, session logging) or a rework that touches several packages.

An epic body holds:

- the goal and why it is worth doing,
- a task list of sub-issues (`- [ ] #12`), kept up to date as they are created and closed,
- the acceptance criteria for the epic as a whole.

The epic closes when its last sub-issue closes. An epic is never closed by a PR of its own.

## `idea`

A parking spot for something worth remembering but not agreed on. Nothing is promised: an idea
may sit open indefinitely, be closed as `wontfix`, or be reopened years later.

An idea body holds the motivation and, where known, the rough shape of a solution — but no
acceptance criteria, because there is nothing to accept yet.

Before an idea is worked on it is **converted**: relabel it to `enhancement` or `epic` and fill in
proper acceptance criteria. Never start a branch from an issue still labelled `idea`.

## `bug`

Observed wrong behavior. The body holds:

- what happened, what was expected,
- reproduction steps, including the host count and broadcast mode where they matter,
- the lazycssh version (`internal/version`) and the terminal / OS if relevant.

A bug fix branch is prefixed `fix/`. A bug that turns out to be a missing feature is relabelled
`enhancement` rather than closed and rewritten.

## `enhancement`

The default for ordinary forward work: a small feature that fits in one PR, or an improvement to
something that already exists. If an enhancement grows past one branch while being worked on,
close it in favor of an `epic` with sub-issues — do not let a single branch sprawl.

## Labels to branch prefixes

The type label describes the issue; the branch prefix describes the change. They are related but
not the same vocabulary:

| Type label | Usual branch prefix |
|------------|--------------------|
| `bug` | `fix/` |
| `enhancement` | `feat/`, or `refactor/` / `docs/` / `chore/` when nothing user-visible changes |
| `epic` | none — epics have no branch, their sub-issues do |
| `idea` | none — convert it first |

See [Development workflow](./workflow.md) for the branch naming pattern and the closing sequence.
