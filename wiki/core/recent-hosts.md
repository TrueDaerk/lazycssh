---
type: concept
title: Recent hosts
description: The persistent list of hosts this machine connected to, and how the host picker offers it back.
resource: internal/recent
tags: [ui, persistence, hosts]
timestamp: 2026-08-10T00:00:00Z
---

# Recent hosts

Every session that reaches `StateConnected` is written to a file, `recent` under
`$XDG_CONFIG_HOME/lazycssh` (falling back to `~/.config/lazycssh`), one host per line, **most
recent first**. The [host picker](./tui.md#the-host-picker) offers the list back as its `rec`
rows, so a machine reached once by typing it does not have to be typed again in the next run
(issue #254).

That is the whole feature: a memory for the hosts `~/.ssh/config` does not know about. A host
that *is* in the config is already a `cfg` row and the merge drops the duplicate.

## The file

`internal/recent.Store` follows [`internal/sessions.Store`](./session-files.md) and
[`internal/history.Store`](./command-history.md): an atomic write (temp file in the same
directory, then rename), `0600` on the file and `0700` on the directory it creates. A missing
file is not an error — it is a machine that has not connected to anything yet.

- **Dedup on both sides.** `Add` moves an existing host to the front instead of appending a
  second copy, and `Load` collapses repeats and blank lines as well, so a hand-edited file
  cannot produce two picker rows for one host.
- **Capped at 200** (`DefaultCapacity`); what falls off is what was reached longest ago.
- **A repeat of the host that is already at the front is not written at all.** A flapping
  session reports connected over and over; the file must not be rewritten each time.

The list names hosts only — no user, no port, no credential — the same rule
[session files](./session-files.md) follow.

## What counts as a connect

`Model.recordRecent` in [the program layer](./program.md) runs on every transport event and
records the sessions that have reached connected since the last pass:

- **Connected, not requested.** A host that failed to dial is not a recent host. The list is
  what answered, not what was asked for.
- **The resolved alias, not the session identifier.** A clone or a repeated host on the command
  line gets a disambiguated id (`srv1#2`), which no later run could connect to; the recorded
  name is `hosts.Host.Alias`.
- **Once per session per run**, tracked by identifier, so a reconnect loop does not turn into a
  write loop.
- **Off the `Update` goroutine.** The bookkeeping happens in `Update`, the write happens in the
  returned `tea.Cmd`. A failed write is swallowed: the picker loses a row, the run continues.

A run built without a store — every test in `internal/program` that does not ask for one —
records nothing and touches no disk.
