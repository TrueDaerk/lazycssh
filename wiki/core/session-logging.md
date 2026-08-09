---
type: concept
title: Session logging
description: Opt-in per-host output logging to disk — file layout, rotation, and why single mode pauses the pen.
resource: internal/sessionlog
tags: [logging, audit, security, cli]
timestamp: 2026-08-09T00:00:00Z
---

# Session logging

Off by default, on only when a run is started with `--log-dir DIR` (issue #45). The default
answers the security constraint the project was founded on: never log session bytes to disk
unless the user explicitly asked for this run to be logged. While it is on, the status bar
carries `SESSION LOGGING ON` for the whole run — like `HOST KEYS UNVERIFIED`, a weakened
default is worth the line it costs.

## File layout

One directory per run, one file per host:

```
DIR/
  2026-08-09_14-05-09/
    web-01.log
    web-02.log
    web-02.log.1        ← rotated once
```

The run directory is named after the moment the run started; a second run in the same second
gets a numeric suffix rather than sharing. Host file names are the session identifiers,
sanitised (path separators, colons and control bytes become `_`, a leading dot is prefixed)
and de-duplicated within the run. Directories are `0700`, files `0600`: session output is
exactly the kind of data that must not be world-readable.

A reconnect appends to the same file with a `[lazycssh: reconnected]` marker — the host did
not change, so its history should not fork. When the run ends, `lazycssh` prints the run
directory to stdout, so the logs are one shell click away.

## Rotation

Each file is bounded (8 MiB by default, `sessionlog.DefaultMaxFileSize`). A file that would
grow past the bound rotates once: the current file becomes `<name>.log.1`, replacing the
previous rotation, and a fresh file continues. A run therefore keeps at most twice the bound
per host — the same drop-oldest posture as the scrollback ring, applied to disk.

## What is written, and what never is

The log taps the transport's output pump: it sees the same bytes the pane's emulator sees
(both streams, interleaved in arrival order, ANSI sequences included), after the exit-hook
echo filter. **Keystrokes are never written.** Typing is where passwords are entered; the
input path does not pass through the logger at all, the same boundary the
[Command log](./command-log.md) draws.

Passwords in *output* are prevented by the remote side — a sane prompt disables echo — but
the tool does not rely on that alone: while broadcast mode is **single**, the mode a sudo
prompt is answered in ([Broadcast scope](./broadcast-scope.md)), output logging pauses for
every host. The gap is visible, not silent: a host whose output was dropped gets a
`[lazycssh: output not logged: single-mode input]` marker, and the first write after leaving
single mode closes it with `[lazycssh: logging resumed]`. A quiet host gets no markers — the
seam appears exactly where something was withheld.

The suppression is wired where mode changes happen: the program wraps the broadcast router's
`SetMode`, so the UI cannot switch modes without the log hearing about it, including a run
that *starts* in single mode.

## Failure containment

A log must never hurt the session it records. `HostLog.Write` never blocks on anything but
the disk, never returns an error, and after the first failure (disk full, directory removed)
drops output instead of stalling the reader goroutine — one broken log is one incomplete
file, never a dead pane. The loss is not silent either: the first error is kept, and closing
the run returns it, so a run that lost output ends with a message and a non-zero exit.

An unwritable `--log-dir` fails at startup, before the TUI takes the screen, because "you
asked for logs and there will be none" must be readable.
