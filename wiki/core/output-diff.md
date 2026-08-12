---
type: concept
title: Cross-host output diff
description: The Output diff panel groups hosts by the output the last command produced, so the machines that disagree stand out instead of hiding among forty panes.
resource: internal/outdiff
tags: [diff, broadcast, ui]
timestamp: 2026-08-12T12:00:00Z
---

# Cross-host output diff

After running the same command on forty hosts, the real question is usually not "what did each
host say" but "which machines disagree". The Output diff panel (`6`) answers it: the hosts are
grouped by the output they produced for the last command sent, and the panel lists the distinct
answers — the consensus first, the outliers after it — instead of making the user read every
pane (issue #46).

## The comparison window

The window opens at the send. When a command goes out over the command line (`:`) or is resent
from the Command log, the model records each reached target's scrollback length
(`App.markDiff`, `internal/ui/diffpanel.go`); a host's *answer* is everything it printed past
that mark. Output from before the send — an earlier command, a chatty motd — never leaks into
the comparison. A host the send could not reach keeps no mark: its silence is a fact about
delivery, already reported by the status bar, not an answer to compare.

The next send opens a new window. There is one window, not one per log entry: the panel is a
live view of the last question asked, not an archive.

Two honesty limits, both deliberate:

- The mark is a line count. If the retention cap rewrites history under it (the pane's
  "older output dropped" case), the tail cannot be reconstructed and the host reads as
  "no output" rather than as an invented answer.
- Broadcast-bar keystrokes (`5`) open no window. The bar sends bytes, not commands; there is
  no boundary to compare from, and single-mode typing is deliberately never recorded anyway
  (see [Command log](./command-log.md)).

## Normalization

Grouping compares *normalized* output (`internal/outdiff`): trailing whitespace is trimmed,
trailing blank lines dropped, and every occurrence of a host's own name — the identifier as
given, the hostname with `user@` and `:port` stripped, and its first DNS label — is replaced
by the placeholder `«host»`, matched only at word boundaries so `web-01` never rewrites
`web-011`. That is what keeps a shell prompt (`root@web-01:~$`) or a `hostname` echo from
putting every host in its own group. Names shorter than two characters are not substituted;
mangling the output would be worse than an unequal prompt.

Timestamps are deliberately **not** normalized: there is no way to recognise a clock that does
not also eat version numbers and IP addresses. A command whose output carries the time will
simply not converge — run one that does not, or read the groups as "every host disagrees",
which for `date` is even true.

## Presentation

The panel is a numbered sidebar panel like any other, on `6` — `5` has meant the broadcast bar
since the bar existed, and the diff view is not worth renumbering it. One row per variant:
how many hosts gave the answer and its first line, largest group first, every group after the
first in the warning style — everything past the consensus is a disagreement, which is what
the panel exists to surface. `p` previews the variant under the cursor whole — the command,
which hosts gave this answer, and the output as the first of them printed it, real hostnames,
not placeholders — as a popup over the grid, which keeps the main area while there are panes
in it (issue #290, see [The main area](./tui.md#the-main-area)). With nothing connected the
same preview is simply what the main area shows.

`enter` on a variant makes its hosts the selection, so "these three disagree" turns into a
target set: `B` then broadcasts the fix to exactly the machines that need it.

Grouping re-reads the marked hosts' scrollback, so it recomputes only while the panel is
selected; unselected, the panel shows the groups as of the last time it was open, the same
"rows I was last handed" contract the Groups panel has with its store.
