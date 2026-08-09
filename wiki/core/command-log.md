---
type: concept
title: Command log
description: The in-memory audit trail of what this run sent, to how many hosts, in which mode — and what it deliberately never records.
resource: internal/commandlog
tags: [audit, broadcast, security, ui]
timestamp: 2026-08-09T00:00:00Z
---

# Command log

The answer to "what did I just do to production". A tool that types on forty machines at once
has to be able to answer that.

## What is recorded

One entry per command, with the broadcast mode and the number of hosts it reached:

```
14:05:09  systemctl restart nginx  → 40 hosts [all]
14:06:22  df -h                    → 3 hosts [selected]
14:09:41  reboot                   → 40 hosts [fleet]
```

A command sent to forty hosts is **one** entry with a count of forty, not forty entries. The log
is about what the user did, not about what the wire carried.

## What is never recorded

Keystrokes typed into a focused pane or the broadcast bar (the bar records only its assembled
line, once, on enter). Typing is where a sudo password is entered, and a log that
captured it would be a plaintext password file nobody asked for; the typing path never calls
`Record` at all.

Input sent in `single` mode. Single is the mode used to answer a sudo prompt — see
[Broadcast scope](./broadcast-scope.md) — and the same reasoning applies. `Record` returns
`false` for it, so a caller cannot quietly assume the entry was made.

Empty commands are dropped too, and a trailing newline is stripped so the same command sent twice
reads the same twice.

## In memory only

Nothing here writes to disk. Session logging is a separate, opt-in feature — see
[Session logging](./session-logging.md); the log type takes no path and the panel offers no
way to name one.

The log is bounded (1000 entries by default) and drops the oldest first. Dropping is **visible**:
the panel renders `(N older entries dropped)` at the top, because an audit trail that quietly
forgets is worse than one that says it forgot.

The panel's window is budgeted in **visual lines**, not entries: a command longer than the panel
wraps, and counting a wrapped entry as one row pushed the rows below it — the cursor among them —
past the box's clip (issue #132). Entries are wrapped individually, the window grows from the
cursor outward while it fits, and up/down always moves exactly one entry; the dropped-entries
notice gives up its line before the cursor entry ever does.

## Resending

`enter` on an entry emits `CommandResendMsg` carrying the **command**, not the hosts it
originally went to. Resending means "run this again on what I am working on now"; replaying the
old target list would send a command to machines the user has since paged away from.

A command that went out to every host is rendered in the warning style, so the audit trail reads
the way the decision felt.
