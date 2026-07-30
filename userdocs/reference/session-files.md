# Session files

A session file names a set of hosts plus how to reach them, so `lazycssh
@prod-web` reproduces a run. In the TUI these files are the **groups**: the
Groups panel lists, creates, deletes and opens them. One file is both a command
line argument and a panel row.

## Location

```
$XDG_CONFIG_HOME/lazycssh/sessions/<name>.yaml
~/.config/lazycssh/sessions/<name>.yaml      # when XDG_CONFIG_HOME is unset
```

The directory is created on the first save with mode `0700`; files are written
`0600`. They hold no secret, but they are an inventory of what this machine can
reach, which is not something to hand to every account on the box.

A name may contain letters, digits, `.`, `-` and `_`, may not start with a dot,
and is at most 64 characters. That is a path-traversal guard as much as a style
rule — the name comes from the command line and becomes a filename.

Saving is atomic (a temporary file, then a rename), so an interrupted save
cannot leave half a session behind.

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
|---|---|
| `version` | schema version; a file newer than the build is refused, not half-read |
| `name` | must match the filename — `lazycssh @a` never opens the session called `b` |
| `description` | free text, shown in the Groups panel |
| `defaults` | connection options for every entry that does not override them |
| `hosts[].pattern` | a host argument, **unexpanded** |
| `hosts[].user`, `.port`, `.identity_file`, `.jump_host` | per-entry overrides |
| `secret_command` | argv that prints the credential on stdout |
| `broadcast` | the mode the run starts in: `all`, `selected`, `single`, `fleet` |
| `working_set` | the starting working set: `first 20`, `21-40`, `web-*` |

Host patterns stay in the form you typed them and are expanded at load time, so
a file written when the fleet had 40 machines still means "all of them" after it
grows. `working_set: selection` is rejected — there is no selection before the
run starts.

## Strictness

Unknown keys are an **error**. A file with `patern:` instead of `pattern:` would
otherwise open a run with a host silently missing, and you would find out from
the machine that never got the command.

Everything checkable without a network is checked at load: schema version, name,
at least one host, every pattern expandable, ports in range.

## No credentials, ever

A session file never carries a password or a passphrase. Keys named `password`,
`passphrase`, `secret` or `key_passphrase` — at any depth — are refused with an
error that does not quote the value it found. This is a refusal, not a warning:
a warning that let the run continue would make the promise untrue.

Reference the credential instead:

```yaml
defaults:
  secret_command: [pass, show, prod/deploy]
hosts:
  - pattern: h1
  - pattern: h2
    secret_command: [op, read, "op://vault/h2/password"]
```

`secret_command` is an **argv, never a shell line** — the program is executed
directly, so a password entry whose name contains shell metacharacters cannot
turn into a command. Per-host entries override the session default.

The rules it follows:

- **stdout is the credential** — exactly one trailing newline is stripped,
  because `pass show` prints one and a password with `\n` glued to it fails
  authentication in a way that is very hard to see;
- **stderr is the diagnostics** — a failure quotes stderr, never stdout;
- **output is capped** at 64 KiB and drained past the cap;
- **a timeout always applies** (30 s by default) — a hung password manager fails
  one host instead of pinning its session forever;
- **empty output is a failure**, not an empty password.

A failing secret command never falls through to the interactive prompt: with
forty hosts you would be asked forty times, having been told nothing about why.

See [Security model](../concepts/security.md) for the rest.
