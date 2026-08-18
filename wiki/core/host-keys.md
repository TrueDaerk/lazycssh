---
type: reference
title: Host key verification
description: How lazycssh verifies server keys against known_hosts, why a changed key is never a prompt, and the one explicit way out.
resource: internal/ssh/knownhosts.go
tags: [ssh, security, known-hosts]
timestamp: 2026-08-18T00:00:00Z
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
unbuffered channel into the event loop, and the pane behaves like a plain terminal running
`ssh` (issues #177, #180, #182): ssh's own question is written into that session's scrollback —
by session id, exact, so ten dials of one alias question ten panes —

```
The authenticity of host 'web-01 (10.0.0.1)' can't be established.
ssh-ed25519 key fingerprint is SHA256:… .
Are you sure you want to continue connecting (yes/no)?
```

— and the answer is typed inline after it: `yes`/`y` + enter accepts and remembers, `no`/`n` +
enter (or `esc`) rejects and fails that pane, anything else entered clears and asks again. The
typed answer echoes at the prompt and stays in the history, the way a terminal leaves it.
**Every dialling session may question at once**: the pump re-arms on receipt, the focused pane
answers its own question, and the broadcast line answers every prompting target together. The
status bar carries an `AUTH` segment while questions are open and the Status panel lists the
prompting hosts (`ctrl+q` still quits). A session closed or cancelled while its question is
open withdraws it via its context instead of leaking a goroutine.

## Seeing an external edit

`KnownHosts` re-stats every configured file before each verification and reloads if any file's
mtime, size, or existence differs from what it last loaded (issue #309). This is what makes
reconnect (`r`) see a `known_hosts` fixed by hand in another terminal — the repro this closes is
`ssh-keygen -R host` plus a fresh `ssh` acceptance, done in a second terminal while lazycssh is
still running, followed by `r` on that host's pane. Without the check, the verifier kept the
snapshot it built at startup (or at the last accepted key) for the rest of the run, and a
correctly fixed `known_hosts` still failed.

The check runs on every dial rather than watching the filesystem for changes: connection setup
is rare and expensive (a TCP handshake and an SSH key exchange) next to a handful of `stat(2)`
calls, so there is no meaningful cost to paying for freshness every time. A file that appears or
disappears between checks is also covered — the existing-file filter in `reload` already handles
that — and a concurrent reconnect of several panes at once may cause the same reload to run more
than once, which is wasted work but not a race: each reload only ever reads files.

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
