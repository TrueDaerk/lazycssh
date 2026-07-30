# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem.

Report it privately through GitHub's
[private vulnerability reporting](https://github.com/TrueDaerk/lazycssh/security/advisories/new)
form. That opens a draft advisory only the maintainer can see.

Include what you would want in a bug report — what you did, what happened, what
you expected — plus the impact as you see it and, if you have one, a minimal
reproduction.

## What to expect

lazycssh is a personal project maintained by one person in their spare time
(see [CONTRIBUTING.md](CONTRIBUTING.md)), so there is no guaranteed response
time. Reports are read and taken seriously; fixes land when they can. Only the
latest commit on `main` is supported — there are no maintained release branches
to backport to.

## Scope

lazycssh is an SSH client that types on many machines at once, so the things
worth reporting are the ones that break one of these promises:

- **Host key verification.** Anything that lets a connection succeed against a
  key that does not match `~/.ssh/known_hosts`, other than the explicit
  `--insecure-ignore-host-key` flag. A *changed* key must never be reachable by
  a prompt, a config file, or a struct field left nil.
- **Credential confinement.** A password, passphrase or `secret_command` output
  appearing in an error message, a session file, the command log, a pane's
  scrollback, or anywhere on disk. Secrets are memory-only by design.
- **Broadcast target integrity.** A keystroke or command reaching hosts outside
  the scope shown on the status bar — the displayed target count is the safety
  mechanism, so a count that can lie is a vulnerability, not a cosmetic bug.
- **Input treated as code.** Host patterns, session file contents and
  `secret_command` entries are never handed to a shell; a path from any of them
  to shell interpretation is in scope. So is path traversal via a session name.
- **Session file handling.** Anything that lets a session file read from disk
  weaken a default, or that leaves files with looser modes than `0600`/`0700`.

Out of scope: lazycssh sends what you type to the machines you told it to
connect to, with your credentials. A broadcast that ran a destructive command
on forty hosts is the feature working — the mitigation is the always-visible
target count, not a confirmation dialog. `--insecure-ignore-host-key` disabling
host key checks is likewise the documented purpose of that flag.
