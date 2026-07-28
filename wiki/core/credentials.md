---
type: concept
title: Credential handling
description: How a session says which credential to use without ever storing it, and how a secret stays out of logs, errors and views.
resource: internal/secret
tags: [security, credentials, secrets, config]
timestamp: 2026-07-28T00:00:00Z
---

# Credential handling

A session config must be able to say *how* to authenticate as a given user without storing the
secret. Three mechanisms, in the order they are preferred:

1. **`ssh-agent`** — nothing to store at all. Still the default; see
   [Authentication](./authentication.md) for the method order at connect time.
2. **`identity_file`** — a path to a private key. The path is configuration; the key stays where
   it is, and its passphrase is asked for once per key file.
3. **`secret_command`** — an argv that prints the credential on standard output, so the user can
   delegate to `pass`, `op`, `security find-generic-password`, or anything else.

An inline password is not a fourth option. It is rejected with `ErrInlinePassword` when the file
is read — see [Session files](./session-files.md).

## `secret_command`

```yaml
defaults:
  secret_command: [pass, show, prod/deploy]
hosts:
  - pattern: h1
  - pattern: h2
    secret_command: [op, read, "op://vault/h2/password"]
```

It is an **argv, never a shell line**. The program is executed directly, so a password entry
whose name contains shell metacharacters cannot turn into a command. Per-host entries override
the session default.

The rules `secret.Command` enforces:

- **Stdout is the credential.** One trailing newline is stripped, because `pass show` prints one
  and a password with `\n` glued to it fails authentication in a way that is very hard to see.
- **Stderr is the diagnostics.** A failure quotes stderr; it never quotes stdout.
- **Output is capped** at 64 KiB and drained past the cap, so a command pointed at the wrong
  thing cannot fill memory or wedge on a full pipe.
- **A timeout always applies** (30 s by default). The process is killed rather than waited on, so
  a hung password manager fails one host's authentication instead of pinning its session forever.
- **Empty output is a failure**, not an empty password.

A failing secret command never falls through to the interactive prompter. A user who configured
one and is suddenly asked to type a password has been told nothing about why — and with forty
hosts they would be asked forty times.

## Secrets in memory

`secret.Value` holds credential material and is deliberately awkward to print:

| Operation | Result |
|-----------|--------|
| `%v`, `%s`, `%q`, `%#v`, `%x`, `String()` | `[redacted]` |
| YAML / JSON / text marshalling | `ErrNotSerialisable` |
| `Reveal()` | the credential — the one conspicuous way out |

The leak this prevents is the accidental one: a `%v` in an error, a struct dumped into a log
line, a value that ends up in the Command log panel. Those all render as `[redacted]` because
the type refuses to render any other way, not because every call site remembered.

`Wipe()` overwrites the buffer. It is honest best-effort: Go cannot promise the garbage
collector never copied the bytes, and a credential that was ever a `string` cannot be reached at
all. It removes the copy that would otherwise sit in a core dump for the rest of the run.

## Tests that guard this

- every rendered form of a `Value` is asserted not to contain the secret,
- a secret command that prints a credential and *then* fails is asserted not to have its output
  in the error,
- a hung command is asserted to fail within the timeout rather than block,
- a session file with `password:` at any depth is asserted to be refused, with the value not
  quoted in the error.
