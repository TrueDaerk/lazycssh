---
type: reference
title: Host key verification
description: How lazycssh verifies server keys against known_hosts, why a changed key is never a prompt, and the one explicit way out.
resource: internal/ssh/knownhosts.go
tags: [ssh, security, known-hosts]
timestamp: 2026-07-31T20:00:00Z
---

# Host key verification

Verification is **on by default** and is checked against the same `~/.ssh/known_hosts` and
`~/.ssh/known_hosts2` files ssh uses, hashed entries included. It cannot be weakened by a config
file; the only way out is an explicit flag.

## Three outcomes, deliberately different

| Situation | What happens |
|-----------|--------------|
| Key recorded and matching | connect |
| Key **unknown** | the session blocks and the user is asked, with the SHA256 fingerprint and key type |
| Key **known but different** | hard failure, no prompt |

The middle and bottom rows are the whole design. An unknown key is a first meeting. A changed
key is either a rebuilt machine or someone sitting in the middle of the connection, and a tool
that offers to click that away is worse than one that refuses. `HostKeyChangedError` names the
file and the line to fix:

```
REMOTE HOST IDENTIFICATION HAS CHANGED for srv1: it offered a ssh-ed25519 key with fingerprint
SHA256:… , which does not match the key recorded in /home/u/.ssh/known_hosts line 12. Remove
that line only if you know the host was rebuilt
```

There is no code path from a changed key to a connection.

## The question in the TUI

The prompt is live since issue #173. A dialling session that meets an unknown key blocks in
`keyPrompter.ConfirmHostKey` (`internal/program/hostkey.go`); the question travels over an
unbuffered channel into the event loop, the UI shows it in the **Status panel** — host alias,
key type, the full SHA256 fingerprint — and it owns the keyboard while open: `y` accepts and
remembers, `n`/`esc` rejects and fails that pane, every other key is swallowed (`ctrl+q` still
quits). Questions arrive **one at a time, per host**: the pump re-arms only after the answer,
so twenty new hosts ask twenty times in order. A session closed or cancelled while its question
is open withdraws it via its context instead of leaking a goroutine.

## Accepting an unknown key

Accepting appends the key to the first configured known_hosts file, creating the file and its
directory with `0600` / `0700` when needed — the case of a fresh machine with no `~/.ssh`. The
acceptance takes effect immediately, so other sessions dialling the same host in the same run
are verified from the file instead of asking again.

Rejecting fails **that session only**. The other thirty-nine keep running.

If nothing can ask — a non-interactive run with no prompter — an unknown key is refused with
`ErrNoHostKeyPrompter`, never accepted. The error still names the fingerprint so the user can
decide out of band.

## The opt-out

`--insecure-ignore-host-key` accepts any key. It is explicit, it prints a warning naming
machine-in-the-middle attacks on every run, and once the TUI exists the status bar carries it
for the whole session.

Nothing else in the program may select it. A `Config` with a nil `HostKeyCallback` is
[refused](./session.md) rather than defaulted to insecure, so a forgotten struct field cannot
become a silent downgrade.
