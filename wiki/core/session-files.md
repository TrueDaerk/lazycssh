---
type: concept
title: Session files
description: The on-disk format for a saved run — schema, location, strictness, and why no credential ever appears in one.
resource: internal/sessions
tags: [config, yaml, xdg, sessions, security]
timestamp: 2026-07-29T21:00:00Z
---

# Session files

Typing forty hostnames and their users on every run defeats the purpose of the tool. A **session
file** names a set of hosts plus how to reach them, so `lazycssh @prod-web` reproduces the run.

In the TUI these files are the **groups**: the Groups panel lists, creates, deletes and opens
them, and opening one makes an [open session](./groups-and-sessions.md) out of it. Same format,
same directory — one file is both a CLI argument and a panel row.

## Location

```
$XDG_CONFIG_HOME/lazycssh/sessions/<name>.yaml
~/.config/lazycssh/sessions/<name>.yaml      # when XDG_CONFIG_HOME is unset
```

The directory is created on the first save, mode `0700`; session files are written `0600`. They
hold no secret, but they are an inventory of what this machine can reach, which is not something
to hand to every account on the box.

A session name may contain letters, digits, `.`, `-` and `_`, may not start with a dot, and is
at most 64 characters. That is a path-traversal guard as much as a style rule: the name comes
from the command line and becomes a filename.

## Schema

```yaml
version: 1
name: prod-web
description: the production web tier
defaults:
  user: deploy
  port: 2222
  identity_file: ~/.ssh/id_prod
  jump_host: bastion.example.com
hosts:
  - pattern: srv1-{01..40}.example.com
  - pattern: canary.example.com
    user: root
    port: 22
broadcast: selected
working_set: first 20
```

| Field | Meaning |
|-------|---------|
| `version` | schema version; a file newer than the build is refused, not half-read |
| `name` | must match the filename — `lazycssh @a` never opens the session called `b` |
| `description` | free text, shown in the Groups panel |
| `defaults` | connection options for every entry that does not override them |
| `hosts[].pattern` | a host argument **unexpanded**, so `srv1-{01..40}.example.com` stays readable |
| `hosts[].user`, `.port`, `.identity_file`, `.jump_host` | per-entry overrides |
| `secret_command` | argv that prints the credential on stdout; see [Credential handling](./credentials.md) |
| `broadcast` | the mode the run starts in: `all`, `selected`, `single`, `fleet` |
| `working_set` | the starting working set: `first 20`, `21-40`, `web-*` |

Host patterns stay in the form the user typed. Expansion happens at load time through
[host expansion](./host-expansion.md), so a file written when the fleet had 40 machines still
means "all of them" after it grows.

`broadcast` and `working_set` are validated by the packages that own them
([broadcast scope](./broadcast-scope.md), [working sets](./working-sets.md)) rather than by a
second, drifting copy of the rules here. `working_set: selection` is rejected: there is no
selection before the run starts.

## Strictness

Unknown keys are an **error**. A file with `patern:` instead of `pattern:` would otherwise open
a run with a host silently missing, and the user would find out from the machine that never got
the command. The same reasoning as the deliberate divergence from bash in host expansion:
silence is the expensive failure mode here.

Everything checkable without a network is checked at load: schema version, name, at least one
host, every pattern expandable, ports in range.

## No credentials, ever

A session file never carries a password or a passphrase. Keys named `password`, `passphrase`,
`secret` or `key_passphrase` — at any depth — are refused with `ErrInlinePassword`, and the
error never quotes the value it found. This is a refusal, not a warning: the security constraint
in the root `CLAUDE.md` says lazycssh does not write or read plaintext secrets, and a warning
that lets the run continue would make that untrue.

Authentication is referenced instead: an agent, an `identity_file` pointing at a key, or a
`secret_command` that prints the credential. See [Credential handling](./credentials.md) for the
mechanisms and [Authentication](./authentication.md) for the method order at connect time.

## Saving a run

`FromRun` turns the live run into a session: the host arguments **as typed**, the connection
options, the broadcast mode and the working set. Saving forty resolved hostnames would produce a
file that is unreadable and wrong the moment the fleet changes, so the patterns are kept.

What is deliberately not saved:

- a **manual** working set — it is a list of identifiers from this run, and restoring it against
  a different fleet would mean nothing,
- anything that only restates a default — a host entry says what differs,
- the defaults that are already the defaults: `broadcast: all` and an unnarrowed working set are
  left out of the file entirely.

`Store.Create` refuses to replace an existing session and returns `ErrExists`; `Store.Save`
overwrites. `Store.SaveRun(run, overwrite)` is the pair in one call. The refusal exists because
only the caller can ask the user, and overwriting a session someone built by hand should be a
decision rather than a side effect.

## Round trip

`Decode` and `Encode` round-trip: load, save, load again produces a byte-identical file, and a
`defaults` block that is entirely empty is left out rather than written as an empty map. Saving
is atomic — a temporary file in the same directory, then a rename — so an interrupted save
cannot leave half a session behind.

## Quick save

`S` at the app level opens the save prompt from anywhere — prefilled with the run's session
name, enter confirms, an existing name still asks `overwrite? y/n`. While typing or in the
broadcast bar, `S` is a keystroke for the hosts like any other letter.

The saved patterns track the run: the CLI arguments, then everything connected or launched at
runtime, deduplicated in order — a run started with `web-*` and extended with `db-01` saves
both, which it previously did not. Removing a host drops a pattern that names it exactly; a
glob or brace pattern stays, because it cannot be narrowed by one host. Saving an empty run
reports `nothing to save` and keeps the prompt and the typed name.
