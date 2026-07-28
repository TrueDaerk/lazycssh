---
type: reference
title: Authentication
description: The order authentication methods are tried in, how secrets are cached across hosts, and the rules that keep them out of logs.
resource: internal/ssh/auth.go
tags: [ssh, auth, security, credentials]
timestamp: 2026-07-28T00:00:00Z
---

# Authentication

`Credentials` builds the authentication methods offered to a host and caches whatever it had to
ask the user for.

## Order

1. **ssh-agent** — when `SSH_AUTH_SOCK` is set and the agent is not disabled.
2. **Identity files** — the `IdentityFile` entries [resolved](./host-resolution.md) for the
   host, in order.
3. **Password**.
4. **keyboard-interactive** — PAM and friends.

The callbacks are lazy. Nothing prompts, reads a key file or talks to the agent until the server
actually asks for that method, so a fleet that accepts the agent never produces a passphrase
prompt for a key it was never going to use.

## Caching: ask once, connect forty times

A cluster tool that asks forty times for one password is unusable. The cache exists to prevent
that:

| Secret | Cache key | Rationale |
|--------|-----------|-----------|
| Passphrase | the key file path | one key, however many hosts use it |
| Password | the **login user** | the same account exists on every machine — that is the premise of the tool |

A wrong answer is recoverable. A passphrase that fails to decrypt its key is forgotten
immediately, so the next attempt asks again instead of failing every remaining host with the
same typo. `ForgetPassword` does the same for a password.

## Secrets never leave memory

- Nothing is written to disk. A `Credentials` value holds secrets for the lifetime of the run.
- A `Prompter` implementation must never log, wrap or store what it receives; the UI reads into
  a masked text input.
- No secret appears in an error string. `TestNoSecretEverAppearsInAnError` drives every failure
  path that has handled a secret — wrong passphrase, missing key, a real failed handshake — and
  asserts the values appear in none of the resulting messages, nor in the session scrollback.

When no prompter is available — a non-interactive run — a method that needs a secret fails with
`ErrNoPrompter` rather than hanging.

## keyboard-interactive

A single hidden question whose text mentions "password" is answered from the password cache, so
a PAM host behaves like a password host instead of prompting twice for the same thing. Anything
else is a genuine question and is passed to the user, with the server's echo flag respected.

## Which method worked

`LastMethod(sessionID)` reports the method most recently attempted for a session; after a
successful handshake that is the one that got in, and it is what the Hosts panel shows.

This is an observation, not a protocol guarantee. `golang.org/x/crypto/ssh` offers no
"this method succeeded" hook, and `ssh.AuthMethod` cannot be wrapped from outside the package —
its only method is unexported. Methods are attempted in order, so the last attempt is the
successful one; each callback records its own attempt.
