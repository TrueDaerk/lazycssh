# Log

## 2026-07-28

- Decided and implemented broadcast scope: `all` means the active working set, and reaching
  every host in the run is a separate `fleet` mode that renders as a warning. `internal/broadcast`
  resolves mode, working set, selection and focus into the target list, and never renders a count
  that differs from it. New decision document `core/broadcast-scope.md`. Version 0.1.11.

- Added `internal/workingset`: the current subject of work as a first-class model object,
  defined by count, range, pattern or manual selection, with single-keystroke paging by chunk
  size and named sets that survive it. New concept document `core/working-sets.md`.
  Version 0.1.10.

- Reconnect now preserves a pane's scrollback across the redial, separated by a visible marker,
  and reuses credentials already held in memory. `core/manager.md` updated. Version 0.1.9.

- Added the session manager: bounded fan-out dialling, one fan-in event channel, per-host
  reconnect and close, and fleet counts for the status bar. Documented in `core/manager.md`.
  Version 0.1.8.

- Added host key verification against `known_hosts` with an accept prompt for unknown keys and a
  hard failure for changed ones, plus the `--insecure-ignore-host-key` opt-out. Documented in
  `core/host-keys.md`. Version 0.1.7.

- Added the authentication chain in `internal/ssh/auth.go`: agent, identity files with a
  passphrase prompt, password, keyboard-interactive, with secrets cached per key file and per
  login user. Documented in `core/authentication.md`. Version 0.1.6.

- Added `ssh.Fake`, the scriptable session implementation that lets everything above the
  transport be tested without a network. Documented in `core/session.md`. Version 0.1.5.

- Added `internal/ssh` with the session lifecycle: dial, handshake, PTY, streams, resize and
  close, behind a `Session` interface. Events are explicitly hints rather than the source of
  truth, because a blocking send deadlocks `Start`. Documented in `core/session.md`.
  Version 0.1.4.

- Added `internal/scrollback`, the bounded per-session ring buffer that enforces the
  backpressure rule. Documented in `core/scrollback.md`. Version 0.1.3.

- Host arguments are now resolved through `~/.ssh/config` (`HostName`, `User`, `Port`,
  `IdentityFile`, `ProxyJump`, glob patterns), with command line overrides winning. New concept
  document `core/host-resolution.md`. Version 0.1.2.

- Added `internal/hosts` with bash-style brace expansion of host arguments, documented in
  `core/host-expansion.md`. Malformed patterns are rejected rather than silently kept as
  literals, which is a deliberate divergence from bash. Version 0.1.1.

- Bootstrapped the Go module as `github.com/TrueDaerk/lazycssh` (the remote, not the
  `github.com/geant/...` path `CLAUDE.md` guessed at). Added `internal/version` with the
  `Version` constant at `0.1.0`, the `lazycssh` entrypoint with `--version`, and a CI workflow
  running gofmt, `go mod tidy`, build, vet and `go test -race`. New concept document
  `core/cli.md`.

- `contributing/conventions.md`: split testing into "what must be tested" and "how to test".
  Added the two binding obligations (exported behavior with branches, bugfix regression test),
  the exemption for delegation and rendering glue, and the deliberate absence of a coverage
  threshold. Mirrored as a short pointer in `CLAUDE.md`.

- Documented the issue type labels (`epic`, `idea`, `bug`, `enhancement`) in
  `contributing/issue-types.md` and linked them from the workflow document.

- Created the wiki bundle. Extracted process documentation out of `CLAUDE.md` into
  `contributing/`: code conventions, development workflow, versioning policy and the
  OKF wiki format rules. `CLAUDE.md` now links here instead of restating them.
