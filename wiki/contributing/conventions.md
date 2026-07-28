---
type: guide
title: Code conventions
description: Formatting, error handling, documentation, testing and state rules for lazycssh Go code.
tags: [go, conventions, style, testing]
timestamp: 2026-07-28T00:00:00Z
---

# Code conventions

## Formatting and static checks

- `gofmt` (or `gofumpt`) clean.
- `go vet` clean.

Both must pass before a PR is opened.

## Errors

- Wrap with context: `fmt.Errorf("dial %s: %w", host, err)`.
- No naked `panic` outside `main`. A failing SSH session degrades one pane, it never
  tears down the program — see the panic containment rule in the root `CLAUDE.md`.

## Documentation

- Exported identifiers carry doc comments.
- Unexported identifiers are documented only where the intent is non-obvious.

## Testing

- Table-driven tests.
- The SSH layer sits behind an interface so UI tests run without a network: fake sessions,
  never real dials.
- The TUI cannot be verified from stdout alone. For automated checks, drive the model's
  `Update` / `View` with synthetic messages; for visual checks, run the binary in a real terminal.

## State

- No global state. Config and dependencies are passed into constructors.
