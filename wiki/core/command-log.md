---
type: concept
title: Command log
description: The in-memory audit trail of what this run sent, to how many hosts, in which mode — and what it deliberately never records.
resource: internal/commandlog
tags: [audit, broadcast, security, ui]
timestamp: 2026-08-09T12:00:00Z
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

## See also

[Command history](./command-history.md) is the command line's own, separate recall: what was
*typed*, persisted to disk across restarts, distinct from this in-memory audit trail of what was
*sent*.

## Resending

`enter` on an entry emits `CommandResendMsg` carrying the **command**, not the hosts it
originally went to. Resending means "run this again on what I am working on now"; replaying the
old target list would send a command to machines the user has since paged away from.

A command that went out to every host is rendered in the warning style, so the audit trail reads
the way the decision felt.

## Resending to the hosts that missed it

`m` on an entry is the complement of `enter`: it sends the command to the hosts that are up
**now** and were **not** among its targets, and to nobody else. A host that reconnects into a
fleet that already ran three commands is the case this exists for — re-sending to the whole
scope would run them a second time on the thirty-nine machines that did not miss anything.

Every entry therefore stores its **target set**, not only a count: `Entry.Hosts` is what
`broadcast.Delivery.To` reported the send actually reached. `Entry.Missing(connected)` is the
set difference, and `Router.SendTo` delivers to exactly that list — bypassing the mode, the
working set, the selection, the visibility limit and the alt-screen exclusion, because the
target list is already an explicit decision rather than a broadcast that might stray.

The resolved list and its count are shown in the panel's preview **before** the key is pressed
(`missing → 2 hosts`, then the identifiers), which is the same rule the broadcast label follows:
the number of machines about to receive a command is never a surprise. With nothing missing the
action is a true no-op and the status bar says `all hosts already received this`.

The new send leaves its own entry, recorded in the **original** entry's mode: the resend repeats
that decision, it does not make a new one.

### The rule for who counts as missing

Membership is by **session identifier**, and **a host that received the command is never missing
again**:

- A host that was down at the send and is up now — **missing**. The case the feature is for.
- A host that joined the run after the send, or a **clone** (`web-01#2`, its own identifier) —
  **missing**. It never was a target.
- A host that received it and has since **reconnected** — **not missing**, even though its fresh
  shell has lost whatever the command did. It did receive it, and guessing otherwise would
  re-run a destructive command on a machine the user did not ask about. Re-running there is the
  explicit resend (`enter`), one keypress away.
- A host that is down now — **not offered** at all: a session that cannot take input would only
  swallow the command.
