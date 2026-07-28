---
type: reference
title: Command line interface
description: Flags, arguments and exit codes of the lazycssh binary.
resource: cmd/lazycssh/main.go
tags: [cli, flags, exit-codes]
timestamp: 2026-07-28T00:00:00Z
---

# Command line interface

```
lazycssh [flags] <host>...
```

Host arguments may use brace expansion, for example `srv1-{01..40}.example.com`. Expansion is
performed by lazycssh itself, so a quoted argument behaves the same as an unquoted one and no
shell is involved.

## Flags

| Flag | Effect |
|------|--------|
| `--version` | Print the version and exit. Takes precedence over every other argument. |
| `-h`, `--help` | Print usage and exit. |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success. Currently only `--version` and `--help` reach it. |
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

Connecting is not implemented yet. An invocation with host arguments reports that and exits
with code `1` rather than pretending to connect. The SSH transport and the TUI are tracked in
their own epics.
