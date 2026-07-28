---
type: reference
title: Host argument expansion
description: Bash-style brace expansion of host arguments, performed by lazycssh itself, and where it deliberately differs from bash.
resource: internal/hosts/expand.go
tags: [hosts, cli, brace-expansion]
timestamp: 2026-07-28T00:00:00Z
---

# Host argument expansion

A rack of machines is one argument:

```sh
lazycssh srv1-{01..40}.example.com
lazycssh '{web,db}-{1..3}.example.com'
lazycssh srv-{a..c}
```

Expansion is performed by lazycssh, never by a shell. Two consequences that matter:

- a quoted argument behaves exactly like an unquoted one, so users do not have to reason about
  whether their shell already expanded something;
- no argument is ever handed to a shell as code, which removes the injection surface entirely.

## Supported syntax

| Form | Example | Result |
|------|---------|--------|
| Alternatives | `srv-{a,b,c}` | `srv-a srv-b srv-c` |
| Empty branch | `srv{,-backup}` | `srv srv-backup` |
| Numeric range | `srv{1..4}` | `srv1 … srv4` |
| Descending range | `srv{3..1}` | `srv3 srv2 srv1` |
| Zero-padded range | `srv{08..11}` | `srv08 … srv11` |
| Range with a step | `srv{0..20..5}` | `srv0 srv5 srv10 srv15 srv20` |
| Letter range | `srv-{a..e}` | `srv-a … srv-e` |
| Nesting | `srv-{a,{b,c}}` | `srv-a srv-b srv-c` |
| Product | `{web,db}-{1..3}` | `web-1 web-2 web-3 db-1 db-2 db-3` |
| Escape | `srv\{1\}` | `srv{1}` |

Ordering matches bash: the leftmost brace varies slowest. Duplicates are kept, also as in bash.

Zero padding is switched on when *either* endpoint carries a leading zero, and the width is the
wider of the two endpoints — `{08..100}` yields `008 … 100`. A negative value keeps its sign in
front of the padding: `{-02..1}` yields `-02 -01 00 01`.

The sign of a step is ignored; the direction comes from the endpoints, so `{9..1..4}` counts down.

## Deliberate divergence from bash

Bash leaves a brace group it cannot parse as a literal. `srv{abc}` is simply the string
`srv{abc}`, and a typo in a hostname pattern therefore turns into an attempt to connect to a
nonsensical host.

lazycssh rejects it instead. A malformed pattern is a `SyntaxError` that names the offending
argument and the position within it:

```
invalid host pattern "srv{abc}" at position 3: "{abc}" is neither alternatives ({a,b}) nor a
range ({1..9}); use \{ for a literal brace
```

Rejected: unbalanced braces, a brace group with neither a comma nor a range, mismatched range
endpoint types (`{a..5}`), a non-integer or zero step, and a trailing backslash. Use `\{` when a
literal brace is really intended.

One bad argument fails the whole invocation. Connecting to some of the intended hosts because a
later pattern had a typo is worse than connecting to none.

## Limits

A single argument may expand to at most 100,000 entries, and so may the arguments taken
together. Brace expansion is multiplicative, so `{1..1000}-{1..1000}` would otherwise build a
million strings before anyone noticed the typo.

## Parity testing

The bash parity claim is verified against bash itself rather than against hand-written
expectations: `TestExpandMatchesBash` shells out and compares. It requires bash 4 or newer and
skips otherwise, because bash 3.2 — still the version macOS ships — supports neither range steps
nor zero padding. CI runs on Linux with bash 5, so the claim is checked on every pull request.
