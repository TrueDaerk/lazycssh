# Log

## 2026-07-28

- Added the Hosts panel: every host with its pane number, selection marker and connection state,
  a filter that owns the keyboard while open, `space` to toggle selection and `enter` to focus a
  pane. Selection is keyed by host identifier so it survives reconnects, filtering and paging, and
  only the visible rows are rendered. `core/tui.md` updated. Version 0.1.23.

- Added the Status panel: session, broadcast scope with a live target count, working set, hosts
  up/total, and the flags that weaken a default. Every number is read from live state at render
  time rather than cached, and the warning flags are repeated on the status bar so they cannot be
  switched away from. `core/tui.md` updated. Version 0.1.22.

- The pane window pages through the host list: `n`/`p` move a whole page and take the pane focus
  with them, moving focus off a page turns it, and the page is clamped when the terminal shrinks.
  The window is explicitly not the working set - paging never changes who receives a keystroke.
  `core/tui.md` updated. Version 0.1.21.

- The pane grid auto-tiles: the squarest shape that holds the hosts, bounded by a 24x6 minimum
  pane size, and paging rather than shrinking below it. Deterministic for every host count, with
  the shapes for 1, 2, 3, 4, 6, 9, 12 and 20 asserted. Full screen is `f`, because the number keys
  belong to the sidebar panels. `core/tui.md` updated. Version 0.1.20.

- Focus routing: keys are dispatched by the focused area and the other area's bindings are not
  consulted, arrows move within the sidebar and the grid, nothing wraps, and `HostsChangedMsg`
  preserves the focused host by identity rather than by position. `core/tui.md` updated.
  Version 0.1.19.

- Added the TUI shell: the bubbletea root model with the sidebar/grid/status-bar layout, focus
  cycling, panel numbers and the generated help overlay. The layout is pure arithmetic, asserted
  non-negative at every size from 1x1 up, and the status bar is never dropped. `CLAUDE.md`
  corrected against the released bubbletea v2 API. New concept document `core/tui.md`.
  Version 0.1.18.

- Added the keymap: every binding declared once in `internal/ui/keys.go`, with the short help line
  and the `?` overlay generated from it. Tests fail on a binding without a help string, on a
  binding that appears in no help group, and on a key that would mean two things at once. New
  concept document `core/keys.md`. Version 0.1.17.

- Added `internal/ui` with the theme: one palette, every style in `theme.go`, and a test that
  fails on a style literal anywhere else in the package. Colour is never the only carrier of
  meaning - focus is a thicker border, the insecure marker is reverse video. The charm v2 modules
  are now required from `charm.land/...`, which `CLAUDE.md` records. New concept document
  `core/theme.md`. Version 0.1.16.

- `lazycssh @name` launches a saved session, merging any extra hosts named on the command line
  in argument order, and an unknown name is an error that lists what is saved. Added
  `--list-sessions` and `--sessions-dir`. `core/cli.md` updated. Version 0.1.15.

- The current run can be saved as a named session: `sessions.FromRun` keeps the host arguments as
  typed, drops what only restates a default, and refuses to overwrite an existing session without
  the caller asking first. `core/session-files.md` updated. Version 0.1.14.

- Session configs can now say *how* to authenticate without storing the secret: a per-session or
  per-host `secret_command` argv whose stdout is the credential, run with a timeout and a hard
  kill so a hung password manager cannot pin a session. Added `internal/secret`, whose `Value`
  renders as `[redacted]` under every format verb and refuses to be serialised. New concept
  document `core/credentials.md`. Version 0.1.13.

- Added `internal/sessions`: the on-disk session format under `$XDG_CONFIG_HOME/lazycssh/sessions`,
  with unexpanded host patterns, a schema version, strict unknown-key rejection, an atomic
  0600 save and an explicit refusal for any inline credential key. New concept document
  `core/session-files.md`. Version 0.1.12.

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
