# Security model

lazycssh holds credentials for a lot of machines and types on all of them at
once. Four rules follow from that, and they are enforced by tests rather than
by good intentions.

## Host keys are verified by default

Verification runs against the same `~/.ssh/known_hosts` and
`~/.ssh/known_hosts2` files ssh uses, hashed entries included. It cannot be
weakened by a config file; the only way out is an explicit flag.

| Situation | What happens |
|---|---|
| Key recorded and matching | connect |
| Key **unknown** | the pane asks, with the SHA256 fingerprint and key type |
| Key **known but different** | hard failure, no prompt |

The last two rows are the whole design. An unknown key is a first meeting. A
changed key is either a rebuilt machine or someone sitting in the middle of the
connection, and a tool that offers to click that away is worse than one that
refuses. The error names the file and the line to fix:

```
REMOTE HOST IDENTIFICATION HAS CHANGED for srv1: it offered a ssh-ed25519 key with fingerprint
SHA256:… , which does not match the key recorded in /home/u/.ssh/known_hosts line 12. Remove
that line only if you know the host was rebuilt
```

There is no code path from a changed key to a connection.

Accepting an unknown key appends it to your first configured `known_hosts` file
(creating the file and `~/.ssh` with `0600`/`0700` when needed) and takes effect
immediately, so other sessions dialling the same host in the same run do not ask
again. Rejecting fails **that** session only; the other thirty-nine keep
running.

`--insecure-ignore-host-key` accepts any key. It prints a warning naming
machine-in-the-middle attacks on every run and the status bar carries
`HOST KEYS UNVERIFIED` for the whole session. Nothing inside the program can
select it: a configuration that forgets to set a host key callback is *refused*
rather than defaulted to insecure.

## Credentials stay in memory

The authentication order per host:

1. **ssh-agent**, when `SSH_AUTH_SOCK` is set;
2. **identity files** resolved for that host, in order;
3. **password**;
4. **keyboard-interactive** (PAM and friends).

Callbacks are lazy — nothing prompts, reads a key file or talks to the agent
until the server actually asks for that method, so a fleet that accepts the
agent never produces a passphrase prompt for a key it was not going to use.

A cluster tool that asks forty times for one password is unusable, so answers
are cached for the run:

| Secret | Cached against | Why |
|---|---|---|
| Passphrase | the key file path | one key, however many hosts use it |
| Password | `user@addr:port` — the machine | different machines may hold different passwords; a uniform cluster is still one typing action, because every pane prompts and the broadcast line answers all of them at once |

A wrong answer is recoverable: a passphrase that fails to decrypt its key is
forgotten immediately, so the next attempt asks again instead of failing every
remaining host with the same typo.

Nothing is written to disk. No secret appears in an error string — a test drives
every failure path that has handled a secret and asserts the values appear in
none of the resulting messages, nor in any pane's scrollback.

### Never a password in a file

A [session file](../reference/session-files.md) never carries a credential. Keys
named `password`, `passphrase`, `secret` or `key_passphrase` — at any depth —
are **refused**, and the error does not quote the value it found. This is a
refusal rather than a warning, because a warning that lets the run continue
would make the promise untrue.

Authentication is referenced instead: an agent, an `identity_file` pointing at a
key, or a `secret_command` — an argv that prints the credential on stdout, so
you can delegate to `pass`, `op`, `security find-generic-password` or anything
else. It is an argv, **never a shell line**: the program is executed directly,
so a password entry whose name contains shell metacharacters cannot turn into a
command. Its output is capped, a timeout always applies, stderr is quoted in
failures while stdout never is, and empty output is a failure rather than an
empty password. A failing secret command never falls through to the interactive
prompt — with forty hosts you would be asked forty times, with nothing said
about why.

## The audit trail records commands, not keystrokes

The command log (++4++) answers "what did I just do to production":

```
14:05:09  systemctl restart nginx  → 40 hosts [all]
14:06:22  df -h                    → 3 hosts [selected]
14:09:41  reboot                   → 40 hosts [fleet]
```

One entry per command with its target count, not one per host — the log is about
what you did, not what the wire carried. It lives in memory only; nothing is
written to disk. It is bounded, and dropping is visible (`(N older entries
dropped)`), because an audit trail that quietly forgets is worse than one that
says it forgot.

What is deliberately **never** recorded:

- keystrokes typed into a focused pane (the broadcast bar records only its
  assembled line, once, on enter),
- anything sent in `single` mode.

Both are where a sudo password gets typed, and a log that captured it would be a
plaintext password file nobody asked for.

The command line's own Up/Down recall is a separate thing from the log above, and it *is*
written to disk (`~/.config/lazycssh/history`), so a command can be found and resent after a
restart — see [Command history](https://github.com/TrueDaerk/lazycssh/blob/main/wiki/core/command-history.md).
The same boundary applies:
only what is typed into the command line reaches it, never a raw broadcast keystroke, so a sudo
password answered character-by-character in a focused pane never reaches the file either.

The host names you connect to are written to disk as well
(`~/.config/lazycssh/recent`), so the host picker can offer them back in the next run. Names
only — no user, no port, no credential — with `0600` permissions, the same rule saved groups
follow: it is an inventory of what this machine reaches, not a secret, and it is not
world-readable either.

Session output only reaches disk when a run is started with `--log-dir` — see
[Reading output](../guides/output.md#logging-a-run-to-disk). The same boundary
holds there: keystrokes are never written, and output logging pauses, with a
visible marker, while `single` mode is active.

## The target count instead of a confirmation

Broadcasting a destructive command to N hosts is the entire point of the tool
and also its main footgun. The design answer is not friction — a confirmation
you click through forty times a day is not a safety mechanism — but an
unmissable, always-honest count:

- the status bar states the mode and the live target count at all times, and the
  first number is always the number of machines that will actually receive the
  next keystroke;
- the ++colon++ command prompt carries the scope on the same line you are typing
  into;
- the broadcast bar's title carries the count (`Broadcast [5] → 7 hosts`);
- every flag that weakens a default — `HOST KEYS UNVERIFIED`, `SESSION LOGGING
  ON`, `BROADCASTING TO EVERY HOST` — is rendered in a warning style on the
  Status panel **and** repeated on the status bar, so switching panels cannot
  hide it.

One consequence worth stating: a mode you can land in by accident is a bug. That
is why `fleet` mode is ++ctrl+alt+b++ and not the next letter in a cycle.

## Panic containment

One dead host is one dead pane. A session that fails — DNS, refused, auth, host
key — writes its error into its own scrollback the way a plain terminal prints
it, and the program keeps running. Nothing a single host does can tear down the
run.
