---
type: guide
title: Code conventions
description: Formatting, error handling, documentation, testing and state rules for lazycssh Go code.
tags: [go, conventions, style, testing]
timestamp: 2026-08-10T00:00:00Z
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

### What must be tested

Two obligations. A PR that violates either is incomplete, not merely improvable:

1. **Every exported behavior with branches or error paths gets tests.** If a function can take
   more than one path — a condition, a loop with an empty case, an error return, a parse of
   untrusted input — the paths are covered.
2. **Every bugfix ships with a test that fails without the fix.** Write the test first, watch it
   fail, then fix. A bugfix PR with no new test is missing half the change.

Exempt: pure delegation (a method that forwards to one call and adds nothing) and rendering
glue whose only assertion would restate the implementation. When in doubt, write the test —
the exemption is for the cases where a test would be noise, not for the cases that are tedious.

Not a rule: a test per function. Mechanical per-function coverage produces tests for getters
while the layout arithmetic and the broadcast routing — where the real defects live — pass
review on a green percentage. There is **no coverage threshold** in this project, deliberately.

### How to test

- Table-driven tests.
- The SSH layer sits behind an interface so UI tests run without a network: fake sessions,
  never real dials.
- The TUI cannot be verified from stdout alone. For automated checks, drive the model's
  `Update` / `View` with synthetic messages; for visual checks, run the binary in a real terminal.
- Concurrency is tested under `-race`; `go test -race ./...` runs locally before every merge —
  there is no CI to catch it, so it is not an optional habit.
- Security-relevant behavior gets an explicit negative test: that no credential reaches a log
  line, that a changed host key never connects silently.

## State

- No global state. Config and dependencies are passed into constructors.
