---
type: concept
title: Command history
description: The persistent Up/Down recall of the broadcast command line, and why it is not the command log.
resource: internal/history
tags: [broadcast, ui, persistence]
timestamp: 2026-08-09T00:00:00Z
---

# Command history

`Up`/`Down` in the command line cycle through previously typed commands, most recent first, the
way a shell's history does. Unlike [Command log](./command-log.md) — the in-memory audit trail of
what was actually sent — this history survives a restart: it is a file, `history` under
`$XDG_CONFIG_HOME/lazycssh` (falling back to `~/.config/lazycssh`), one command per line, oldest
first.

## Why it is a separate thing from the command log

The command log is about proving what a host received: single-mode input and raw broadcast
keystrokes never reach it (see [Command log](./command-log.md)). History is about recalling what
was *typed* at the prompt, so it also keeps `:find` and selection commands, which are for
lazycssh itself and never reach a host at all.

The one rule both share: only what is typed into the command line ever lands in either. A raw
broadcast keystroke — the path that answers a sudo prompt or drives `vim` in a focused pane — is
never routed through the command line, and neither store ever sees it.

## The file

`internal/history.Store` follows the same shape as `internal/sessions.Store`: an atomic write
(temp file in the same directory, then rename) and `0600` permissions on the file, `0700` on the
directory it creates. A missing file is not an error — a user who has never sent a command has
nothing to load.

`Append` is where the two rules that make this behave like a shell's history live:

- A repeat of the **immediately preceding** entry is not stored again. Holding `enter` on the
  same command must not fill the file with a thousand copies of it.
- The file is capped at 1000 entries (`DefaultCapacity`); the oldest fall off first.

## Loading

Reading the file is disk I/O, so it happens the way every other disk read in the UI does — off
`Update`, in the `tea.Cmd` `App.Init` returns (issue #225). The result lands as a
`HistoryLoadedMsg`; `App.applyHistoryLoaded` prepends the loaded entries ahead of anything already
in the in-memory copy, rather than replacing it, because the load can lose a race against a
command sent (and locally recorded) before it resolves.

## Editing while browsing

`Up`/`Down` write into the command line's `textinput`, which holds its own copy of the string. A
character typed while browsing edits that copy; the stored entry is untouched until it is chosen
again. This works out of the box because Go strings are immutable and `textinput.SetValue` copies
— there is nothing to enforce here beyond not aliasing the slice.

## Failure is not fatal to a send

Both the load and the append are best-effort: a `HistoryLoadedMsg` with an error is reported on
the status line (`history: ...`) and recall simply starts empty; an `Append` failure is swallowed
by the command line, because a full disk must not stop a broadcast that otherwise succeeded. The
in-memory recall (`App.History`) is unaffected either way — it is not sourced from the file after
the first load, only seeded by it.
