# Log

## 2026-07-29

- Mouse support: cell-motion reporting is on. Clicking a pane focuses it (typing starts there),
  the new `[x]` button at the right of every pane header closes a live host or removes a dead
  one, clicking a sidebar box selects its panel and a row moves that panel's cursor, and
  clicking the broadcast bar gives it the keyboard. The wheel scrolls the pane under the
  pointer — not the focused one, without stealing focus — and moves sidebar cursors.
  Hit-testing is pure arithmetic over the existing layout rects (`internal/ui/hittest.go`,
  `mouse.go`), table-tested without a terminal. `core/tui.md` and `core/keys.md` updated.
  Version 0.7.0.

- Live broadcast bar: an always-visible input line under the grid (`Layout.Broadcast`, degrades
  to a bare line and then disappears on short terminals). `6` focuses it, tab reaches it after
  the last panel, `ctrl+]` leaves. Every keystroke fans out live through the broadcast scope —
  ctrl+c to every target, tab completion on every target — with a local echo line; enter sends
  a carriage return and records the assembled line in the command log once. The title carries
  the live target count, the status bar says BROADCASTING while the bar has the keyboard, and
  the grid is no longer a tab stop (alt+arrows or enter lead into a pane's terminal).
  `core/tui.md`, `core/keys.md`, `core/broadcast-scope.md` and `core/command-log.md` updated.
  Version 0.6.0.

- Terminal-input focus model: a focused pane is a terminal. Every plain keystroke — letters,
  enter, tab, esc, ctrl+c — is encoded (`internal/ui/keystroke.go`) and written directly to that
  one host through the new `PaneWriter` interface, bypassing the broadcast scope. lazycssh keeps
  only `ctrl+]` (back to the app level, cursor on the host just typed to) and the `alt`/`shift`
  pane-management chords: `alt+arrows` switch panes, `alt+z` full screen, `alt+n/p` pages,
  `alt+x` close/remove, `alt+r` reconnect, `shift+pgup/pgdn/home/end` scrollback, `alt+/ [ ] c`
  search — all of which also work from the app level. The status bar always says
  `TYPING <host>`. The old passthrough mode is gone, superseded; typed keys are still never
  recorded. `core/tui.md`, `core/keys.md`, `core/broadcast-scope.md` and `core/command-log.md`
  updated. Version 0.5.0.

- Close and remove single hosts: `x` is state-dependent — on a connected host it closes the
  session (the pane stays and says so), on a failed or closed host it removes the pane from the
  run. Works in the grid and, new, on the row under the cursor in the Hosts panel (`r` there
  reconnects). `Manager.Remove` and `Router.Forget` are new; removal updates the working set,
  prunes scroll offsets, and a removed ssh-config host reappears as a connect candidate.
  `core/manager.md`, `core/broadcast-scope.md`, `core/keys.md`, `core/tui.md` and
  `core/program.md` updated. Version 0.4.1.

- Connect from inside the TUI: the Hosts panel lists the concrete `~/.ssh/config` aliases as
  connect candidates under a `─ ssh config ─` divider (`Resolver.Aliases()` is new) — `enter`
  connects the one under the cursor, `space` marks several, and `n` opens a free-text prompt
  for any pattern including brace expansion and `user@host:port`. The UI emits
  `HostConnectMsg`; the program resolves, skips hosts already in the run, adds the rest with
  `Manager.Add`, and sends resolve errors back as `ConnectErrorMsg` for the panel to show. An
  argumentless start now opens on the Hosts panel, so a fresh install reaches a fleet with the
  keyboard alone. `core/tui.md`, `core/keys.md`, `core/cli.md` and `core/host-resolution.md`
  updated. Version 0.4.0.

- Lazygit-faithful shell: the sidebar is now a stack of individually bordered panels with their
  titles and number shortcuts in the top border line (`╭ Hosts [2] ─╮`), the selected panel
  expanding and the others collapsing to titled boxes (`SidebarHeights`, `titledBox`). `tab` /
  `shift+tab` walk the lazygit cycle — each panel, then the grid. The `?` help is a keybindings
  popup composited **over** the frame with lipgloss layers instead of replacing it, and the
  status bar right-aligns its key hints. Frames now fill their rects exactly (lipgloss v2 counts
  the border into `Width`/`Height`). `core/tui.md`, `core/keys.md` and `core/theme.md` updated.
  Version 0.3.0.

- Argumentless start: `lazycssh` without host arguments opens the TUI on an empty run instead
  of exiting with a usage error. The Sessions panel has focus, the empty grid renders a hint
  ("no hosts — pick a session in [4] Sessions …"), and launching a saved session connects its
  hosts into the run. `core/cli.md` updated. Version 0.2.1.

- The program runs: `internal/program` assembles the fleet, the working set, the broadcast
  router, the command log and the TUI, and `lazycssh host...` opens the interface over a live
  fleet. A wrapper model pumps transport events into the UI and acts on what the UI may only
  ask for: `r` reconnects, `x` closes, launching a saved session adds its hosts to the run
  (`Manager.Add` is new), and a terminal resize reaches every remote PTY. Interactive auth
  prompts are not wired yet — such hosts fail their pane with a clear error (#87). New
  `core/program.md`; `core/cli.md`, `core/manager.md` and `core/tui.md` updated. Version 0.2.0.

- Scrollback navigation and search: `ctrl+u`/`ctrl+d` scroll the focused pane, `g`/`G` jump to
  the oldest output and back to the tail, and the status bar warns `scrollback +N` while a pane
  is not following. `/` searches the focused pane, `[`/`]` walk the matches, and the command
  line's `/find <text>` reports which hosts printed it across the whole run. The offset is a
  render-time window - the buffer keeps receiving while scrolled. `core/tui.md` and
  `core/keys.md` updated. Version 0.1.34.

- Exit codes and failure visibility: sessions arm a prompt hook (OSC 133;D with `$?`, bash and
  zsh) and a scanner on the stdout pump records each command's status, degrading to "unknown" on
  shells without the hook. A failing pane gets a danger border and `exit N` in its header, the
  Hosts panel marks failing rows, the status bar counts `3 hosts failed`, and `!` jumps to the
  next failing host from anywhere, wrapping. `core/session.md`, `core/tui.md` and `core/keys.md`
  updated. Version 0.1.33.

- Per-pane status header: pane number, host name and connection state, read from live state at
  render time. Too-long host names truncate from the left so the distinguishing suffix survives,
  and at widths that cannot hold both, the state yields to the name. The last exit code joins the
  header with #41. `core/tui.md` updated. Version 0.1.32.

- Panes render their session's scrollback: tail-following, hard-wrapped at the pane width with
  colours kept across the break, and a `~ N lines dropped ~` marker where the bounded buffer
  evicted output. SGR sequences pass through so `ls --color` looks right; cursor movement, screen
  clearing, OSC and stray control bytes are neutralized so one host emitting `clear` cannot
  corrupt the layout. `SessionOutputMsg` asks for a redraw and carries no bytes. `core/tui.md`
  updated. Version 0.1.31.

## 2026-07-28

- Added raw keystroke passthrough on `ctrl+]`: every key is encoded and written to the remote
  PTYs, lazycssh keeps only the escape binding, and the status bar names it. `ctrl+c` reaches the
  hosts without quitting, `tab` reaches the remote shell rather than cycling focus, and raw keys
  are never written to the command log. The test SSH server now completes a word on tab, so the
  round trip is asserted against a real PTY. `core/tui.md` and `core/keys.md` updated.
  Version 0.1.30.

- Selection-set operations: `a`, `i`, `c`, `u` and `d` in the Hosts panel, and `/select` /
  `/deselect` with a glob from the command line. Patterns match across the whole run rather than
  the working set, and `/`-prefixed lines are never sent to a host - `select` is a shell builtin
  and a real command must never be intercepted. `core/broadcast-scope.md` and `core/keys.md`
  updated. Version 0.1.29.

- Added the broadcast command line: `:` opens a prompt that carries the target count while the
  command is typed, `enter` sends it to the active broadcast set and reports how many hosts of the
  scope received it, and the prompt owns the keyboard so editing keys cannot leak to the remote
  shells. History with the arrow keys, and resending from the Command log takes the same path.
  `core/tui.md` updated. Version 0.1.28.

- Broadcast modes are switchable from anywhere (`b` / `B` / `s` / `ctrl+alt+b`) and the router now
  knows who is actually up: targets are the scope minus the sessions that cannot take input, the
  status bar reads `BROADCAST all (7/8 up)`, and `Router.Send` delivers to exactly those hosts and
  reports how many of the addressed scope received it. `core/broadcast-scope.md` updated.
  Version 0.1.27.

- Added `internal/commandlog` and the Command log panel: one entry per command with its target
  count and mode, never an entry per host, and never anything typed in `single` mode - that is the
  mode a sudo prompt is answered in. The log is in memory only, bounded, and says so when it drops
  older entries. New concept document `core/command-log.md`. Version 0.1.26.

- Added the Sessions panel: saved sessions with host counts, `enter` to launch and `space` to
  merge (both emitted as messages, so the UI still cannot dial), and `w` to save the current run
  with a name prompt. An existing name turns into an explicit overwrite question and nothing is
  written until it is answered. `core/tui.md` updated. Version 0.1.25.

- Added the Groups / Views panel: the all-hosts entry, every named working set and the unnamed ad
  hoc one, with the active row marked by a character as well as a style and `enter` switching to
  it in a single keystroke. `[`/`]` page the working set, distinct from paging the pane window.
  `core/tui.md` updated. Version 0.1.24.

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
