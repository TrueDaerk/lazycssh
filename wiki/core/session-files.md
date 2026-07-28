---
type: concept
title: Session files
description: The on-disk format for a saved run — schema, location, strictness, and why no credential ever appears in one.
resource: internal/sessions
tags: [config, yaml, xdg, sessions, security]
timestamp: 2026-07-28T00:00:00Z
---

# Session files

Typing forty hostnames and their users on every run defeats the purpose of the tool. A **session
file** names a set of hosts plus how to reach them, so `lazycssh @prod-web` reproduces the run.

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
| `description` | free text, shown in the Sessions panel |
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

## Round trip

`Decode` and `Encode` round-trip: load, save, load again produces a byte-identical file, and a
`defaults` block that is entirely empty is left out rather than written as an empty map. Saving
is atomic — a temporary file in the same directory, then a rename — so an interrupted save
cannot leave half a session behind.
