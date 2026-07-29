---
type: reference
title: Command line interface
description: Flags, arguments and exit codes of the lazycssh binary.
resource: cmd/lazycssh/main.go
tags: [cli, flags, exit-codes]
timestamp: 2026-07-29T00:00:00Z
---

# Command line interface

```
lazycssh [flags] <host|@session>...
```

Host arguments may use brace expansion, for example `srv1-{01..40}.example.com`. Expansion is
performed by lazycssh itself, so a quoted argument behaves the same as an unquoted one and no
shell is involved.

An argument starting with `@` names a saved session; see
[Session files](./session-files.md). Sessions and hosts can be mixed and the order is
preserved, because the order hosts appear in is the order their panes are tiled in:

```sh
lazycssh @prod-web                      # the saved session
lazycssh @prod-web extra.example.com    # the session plus one more host
lazycssh @prod-web @canaries            # two sessions merged
```

When more than one session is named, the last one's broadcast mode wins, and the last one that
sets a working set wins: the last thing the user named is the thing they meant. A session name
that is not saved is an error listing the sessions that are.

## Flags

| Flag | Effect |
|------|--------|
| `--version` | Print the version and exit. Takes precedence over every other argument. |
| `-h`, `--help` | Print usage and exit. |
| `--insecure-ignore-host-key` | Accept any host key without checking `known_hosts`. Dangerous, prints a warning on every run — see [Host key verification](./host-keys.md). |
| `--list-sessions` | List the saved sessions with their host counts and exit. |
| `--sessions-dir <dir>` | Read sessions from this directory instead of `$XDG_CONFIG_HOME/lazycssh/sessions`. |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success: a clean TUI exit, `--version`, `--help` or `--list-sessions`. |
| `1` | A failure during the run. |
| `2` | Usage error: unknown flag, or no host arguments given. |

## Output streams

Anything a user might pipe goes to stdout; diagnostics, usage and errors go to stderr. A failing
invocation writes nothing to stdout — this is covered by a test.

## Version string

`--version` prints the [`Version`](../contributing/versioning.md) constant, enriched with VCS
information when the binary was built from a git checkout:

```
0.1.0
0.1.0 (3933d0b)
0.1.0 (3933d0b-dirty)
```

The revision comes from the build info the Go toolchain embeds, so it is present in
`go build` output but absent in test binaries and in builds from a source tarball.

## Current state

An invocation with host arguments starts the TUI over a live fleet — see
[Program assembly](./program.md). Interactive authentication prompts are not wired into the
TUI yet: hosts that need a password or an unknown-host-key confirmation fail their pane with a
clear error (#87 tracks the prompt). Running without host arguments still exits with code `2`
(#86 tracks the argumentless start).
