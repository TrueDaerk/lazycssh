# Log

## 2026-08-09

- Persistent command history (issue #245). The broadcast command line's Up/Down recall now
  survives a restart: `internal/history.Store` is the same shape as `internal/sessions.Store` -
  atomic write, `0600` file, `0700` directory - reading and writing `history` under
  `$XDG_CONFIG_HOME/lazycssh` (`~/.config/lazycssh` when unset), newest last, capped at 1000
  entries, a consecutive repeat stored once. The read is disk I/O so it happens in the `tea.Cmd`
  `App.Init` returns, landing as `HistoryLoadedMsg`; the load is prepended rather than replacing
  the in-memory copy so it cannot lose a command sent while the file was still loading. Only what
  is typed into the command line ever reaches the file - a raw broadcast keystroke never does -
  the same boundary the command log already enforces. New `core/command-history.md`;
  `core/command-log.md`, `core/index.md` and the user docs updated.

- Reconnect-all binding (issue #244). `R` is the global, bulk form of the per-pane `alt+r`: it
  re-dials every session currently `StateFailed` or `StateClosed`, leaving connected and
  still-dialling sessions untouched, so a network blip across forty hosts is one keystroke
  instead of forty. `ssh.Manager.ReconnectAll` fans out over the existing `Reconnect` path, so
  it keeps the dial concurrency semaphore and the scrollback-preserving handoff, and one host's
  redial failing cannot affect the others. The status line reports the retried count up front,
  from the fleet snapshot; with nothing down it is a true no-op. `core/keys.md` and
  `core/manager.md` updated.

- Cross-host output diff (issue #46). A new `[6] Output diff` sidebar panel groups the hosts
  by the output they produced since the last command line send and lists the distinct
  answers, largest group first — the machines that disagree stand out instead of hiding
  among the panes. `internal/outdiff` is the pure comparison (normalization replaces a
  host's own names with a placeholder; timestamps deliberately untouched);
  `internal/ui/diffpanel.go` is the panel plus the comparison window the send opens
  (`App.markDiff` records each reached target's scrollback length). `enter` on a variant
  makes its hosts the selection. The panel sits on `6` — `5` stays the broadcast bar's.
  New `core/output-diff.md`; `core/keys.md`, `core/tui.md` and the user docs updated.

- The sidebar panels became child models (issue #228). Status, Groups, Sessions and the
  Command log are now structs behind a `sidePanel` interface (`internal/ui/sidepanel.go`) —
  `Update`/`View`/`Title`/`Number` plus cursor and preview accessors — and the root dispatches
  to them instead of switching over the panel enum: `panelBody`, the `handle*Key` handlers,
  `panelPreview` and the mouse cursor moves are all interface calls now. Panel view state
  (cursors, the new-group/delete/save-as/end-session dialogs, the group rows) moved off `App`
  into the children; the root keeps domain state and reduces the panels' domain messages, and
  pushes a `panelContext` snapshot into every child per `Update` so a panel renders from its
  own fields alone. Pure structural refactor — no behaviour change. `core/tui.md` gained a
  "Panel child models" section.

- The help overlay had two defects (issue #227). `q` fell through to the app-level quit binding
  while the overlay was open, so reading the help could kill the whole session; now the overlay
  swallows `q` (like any other key) to close itself, and only `ctrl+q` (`ForceQuit`) still
  quits from there. Separately, `contextHelp.Titles()` existed to label the overlay's columns
  per area but was never called; `renderHelpColumns` (`internal/ui/app.go`) now heads each
  column with its area name, matching `Titles()`'s order to `FullHelp()`'s groups, and drops
  trailing columns — title included — the same way the help bubble's own width clamp does.
  `core/keys.md` and `userdocs/reference/keybindings.md` updated.

- The prompt and confirm keys moved into `KeyMap` (issue #226). `esc`, `enter`, `tab`, the
  command-history arrows, `y`/`n`, `backspace`, `ctrl+c` and `ctrl+q` were matched by comparing
  `msg.String()` to literals at every prompt site, which kept them out of the `?` overlay and
  out of the invariants that keep the help honest. They are now bindings in a new `AreaPrompt`,
  matched with `key.Matches`, and the overlay carries a **prompts** column in every context.
  Dialog footers are generated from those bindings (`promptHint`/`does`/`note`) instead of
  being hard-coded strings, so a hint cannot name a key the handler does not take. The same
  pass moved the selection keys and the broadcast bar's literal `ctrl+a` onto their bindings.
  `TestPromptKeysAreMatchedThroughTheKeyMap` parses the package and rejects any comparison of
  `msg.String()` against a managed key. One behaviour fix fell out: `esc` on a broadcast auth
  prompt now delivers its cancel `Cmd` instead of dropping it. `core/keys.md` updated.

- Blocking I/O left `Update` (issue #225). The transport grew a per-session **stdin queue**
  (`internal/ssh/stdinqueue.go`): `Session.Write` now enqueues for a single drain goroutine and
  never blocks on the network, so every inline send path — per-keystroke `SendKey`, the
  broadcast bar, `Router.Send`, the end-session `ctrl+c`/`ctrl+d` — is non-blocking at the
  transport layer; a stalled host's full backlog refuses the write loudly. Disk I/O moved into
  `tea.Cmd`s with result messages: the group directory read (`GroupsLoadedMsg`, started by
  `Init` and `SessionsChangedMsg`), the new-group write (`GroupSavedMsg`), the delete
  (`GroupRemovedMsg`) and the save-as write (`SaveResultMsg`); dialogs stay open, swallowing
  keys, until the result lands. In `internal/program`, the store load and ssh-config resolution
  behind `GroupOpenMsg`/`HostConnectMsg` and the goroutine wait inside `RemoveHostMsg` run in
  Cmds too, with the manager mutations landing on the `Update` goroutine. Documented in
  `core/session.md`, `core/program.md`, `core/tui.md`. Version 0.10.15.

- The status bar's app-name segment now carries the version (`lazycssh v0.10.13`), read from
  `internal/version.Version` and passed in through the new `ui.Config.Version` field so
  `internal/ui` keeps not importing `internal/version` directly (issue #224). `program.Build`
  wires it from `version.Version`; an empty `Config.Version` renders the bare name, which is what
  the existing view tests still do. Documented in `core/tui.md`. Version 0.10.14.

- The sidebar's list cursor row shows the strong background highlight only when its panel is both
  selected and the sidebar holds the keyboard, lazygit style (issue #222). Every other case — a
  collapsed preview box, or the selected panel while the grid or broadcast bar has focus — keeps a
  new `Theme.CursorMuted` style instead: a coloured, bold row with no background, underlined
  rather than reverse video when colour is off, so the position never disappears. `panelBody` and
  `groupsPanel`/`sessionsPanel`/`logPanel` now take the focus flag `renderSidebar` already computed
  for the panel's own border, and thread it down to `groupLine`/`openSessionLine`/`logLine` through
  the new `Theme.ListCursor(focused)` helper. Documented in `core/theme.md`. Version 0.10.12.

- Confirms and prompts render as **centred modals** composited over the layout instead of being
  drawn into the panel that asks them (issue #221), the way lazygit renders its popups: the
  new-group dialog, the delete-group and overwrite confirms, the save-as prompt, the end-session
  confirm, the new-host prompt with its alias completions, and the split-size prompt. The
  rendering lives in `internal/ui/modal.go`, and `activeModal` walks the same order as the guard
  chain in `handleKey`, so what floats is what listens. Confirms now resolve on `enter` as well
  as `y` and withdraw on `esc`/`n`, with every other key ignored rather than treated as no — a
  stray keystroke must not answer a file delete or a fleet-wide `ctrl+c`. The focused input's
  caret is lifted into `View.Cursor`. The command line and the scrollback search keep the
  status-bar position (they are aimed at the panes a box would cover), and the per-pane auth and
  host-key prompts stay inline. Documented in `core/tui.md`, `core/keys.md`,
  `core/session-files.md` and `core/groups-and-sessions.md`. Version 0.10.11.

- `h`/`l` alias `←`/`→` in the sidebar, lazygit style, matching the existing `j`/`k` alias on
  `↑`/`↓` (issue #220). The alias lives on the existing `Left`/`Right` bindings in
  `internal/ui/keys.go`, so help and the options bar pick it up automatically; a pane and the
  broadcast bar still forward plain `h`/`l` to the host. Documented in `core/keys.md`. Version
  0.10.10.

- Screen modes cycle in three states instead of a `fullScreen` boolean (issue #219): `alt++`
  (lazygit's `+`, taken as a chord because a pane forwards a plain `+` to the shell) walks
  normal → half → full → normal, and `alt+z` remains the direct full-screen toggle from any mode.
  Half mode gives whatever has the keyboard about half the terminal: the sidebar takes half the
  width and its unselected panels drop their previews, or the grid keeps the width and shows at
  most `HalfScreenPanes` (2) panes so the focused pane is half the screen — the hosts that no
  longer fit page, and the overflow footer plus the new `screen half` status flag say so. The
  geometry is `ComputeScreenLayout`, `SidebarHeightsMode` and `TileGridCapped`; nothing is
  hardcoded in a renderer. The frame is recomputed after every message (`syncLayout`) because it
  now depends on focus as well as size, and `program` re-sizes the remote PTYs whenever the pane
  size actually moved, so a mode or focus change reaches the remotes like a window resize does.
  Documented in `core/tui.md` and `core/keys.md`. Version 0.10.9.

- The main area now previews the focused sidebar panel's cursor row (issue #218), the way
  lazygit's main view details the selected side-panel item: `[2] Groups` shows the group's
  patterns and metadata, `[3] Sessions` its hosts and their states, `[4] Command log` the whole
  command with its timestamp and scope. Grid and Status focus keep the pane grid — it is the
  fleet's detail view. Previews are pure model state (`internal/ui/preview.go`), count the rows
  they cannot fit, and render inside `Layout.Main` at every size; a click on one brings the grid
  back rather than acting on a pane that is not drawn. Documented in `core/tui.md`. Version
  0.10.8.

- Cut `-race` test wall time (issue #230): `TestRenderSurvivesConcurrentStateFlips` renders 50
  frames instead of 500 and its flip goroutines yield per iteration instead of busy-spinning —
  that one test alone took 201s under `-race`. `TestFakeFlood` floods 10 500 lines (just past
  the 10k cap) instead of 20 000; `TestFakeIsConcurrencySafe` does 4×100 mixed ops with a full
  render every fifth instead of 4×250 with one per op. The encrypted/plain test keys in
  `internal/ssh` are generated and marshaled once per package (bcrypt KDF dominated); each test
  still gets its own file in `t.TempDir()`. The slow independent tests in both packages now run
  with `t.Parallel()`. Locally under `-race`: `internal/ui` 201s → 16s, `internal/ssh` 30s →
  12s, full suite 18s wall. No test deleted, no assertion weakened. Version 0.10.7.

- Opt-in session logging to disk (issue #45): a new `internal/sessionlog` package writes one
  file per host under a per-run directory created by `--log-dir DIR` — `0600` files in `0700`
  directories, 8 MiB rotation keeping one older generation, reconnects appending with a marker.
  The transport tees its post-filter output pump into `ssh.Config.Log`; keystrokes never pass
  through. While broadcast mode is `single` the log is suppressed fleet-wide with visible
  pause/resume markers, wired by `program.loggedTargets` wrapping the router's `SetMode`. The
  pre-existing `ui.Config.Logging` status flag (`SESSION LOGGING ON`) is now set; a clean exit
  prints the run directory, a lossy log makes the exit non-zero. New `core/session-logging.md`;
  `core/cli.md`, `core/command-log.md`, user docs and README updated. Version 0.10.6.

- The broadcast bar's `ctrl+a` prefix now **forwards by default** (issue #214), superseding the
  one-shot command dispatch of issue #148. `ctrl+a ctrl+a` (freed by issue #213) and `ctrl+a a`
  send one literal `0x01` via `sendBroadcastRaw`; `ctrl+a esc` still enters view mode; every
  other key after the prefix goes to the targets through the same `paneKeyEvents` +
  `Sender.SendKey` path as plain typing, extracted into `sendBroadcastKey`/`forwardBroadcastKey`,
  and stays out of the assembled line the recorder logs. `matchesAppBinding` and the "has no
  lazycssh binding" no-op are gone — view mode is now the way to run an app command from inside
  the bar. Binding help and the pending-prefix status hint reworded; `core/tui.md`,
  `core/keys.md` and the user docs updated. Version 0.10.5.

- Removed the `ctrl+a` connected-only visibility filter (issue #213): the `ConnectedOnly`
  binding, the `connectedOnly` model field, `toggleConnectedOnly`, the `ConnectedOnly()`
  accessor, the `CONNECTED HOSTS ONLY` status flag and the "no connected hosts" empty state are
  gone. `filteredHosts` is now a pass-through of `sessionHosts()` that the `ctrl+s` split still
  chunks, so the split and its broadcast narrowing are unchanged. Outside the broadcast bar
  `ctrl+a` has no app-level meaning; in a pane it still reaches the host as `0x01`, and in the
  bar it stays the csshx escape prefix (freed for the passthrough work in issue #214).
  `core/tui.md`, `core/keys.md` and the user docs updated. Version 0.10.4.

- Up/down no longer switch the focused sidebar panel (issue #212): they moved a list cursor
  inside the Groups/Sessions/Command-log panels, but jumped to the next/previous panel at the
  first/last entry and did nothing useful on the Status panel's absent cursor — the same key
  meant two different things depending on cursor position. The boundary `movePanel` calls in
  `handleGroupsKey`, `handleSessionsKey` and `handleLogKey` are gone; up/down are now a no-op at
  the ends. `left`/`right` are the new explicit panel switch while the sidebar has focus, since
  neither key means anything else there — bounded to the panel list, unlike `tab`/`shift+tab`,
  which still reach the broadcast bar. `core/keys.md` and the user docs updated. Version 0.10.3.

## 2026-08-07

- Issue creation aligned with the ike process (issue #210): `contributing/workflow.md` now
  mandates a duplicate check (open **and** closed issues, epic task lists; reopen instead of
  re-filing), body conventions (English, `- [ ]` acceptance checklist including tests/wiki,
  `Depends on #N`, sub-issues link their epic), the one-task scope rule, one GitHub milestone
  per epic, and out-of-scope discoveries as new issues. `contributing/issue-types.md` gained the
  milestone rule under `epic`. ike's `roadmap:NNNN` stream labels were not ported — lazycssh has
  no roadmap streams. Version 0.10.2.

## 2026-08-03

- Screenful paging moved from `ctrl+arrows` to `ctrl+shift+arrows` (issue #208): plain
  `ctrl+←`/`ctrl+→` never reached lazycssh in common setups — IDEs switch editor panes on them,
  GNOME/KDE switch workspaces — leaving a navigator that silently did nothing. The new chords
  arrive through the standard xterm `CSI 1;6D/C` encoding, work on every keyboard layout, and are
  handled as pane-management chords, so paging now works while typing too. Plain ctrl+arrows are
  never claimed any more: `motionKey` translates them to readline word movement (`ESC b`/`ESC f`,
  the Linux/Windows convention) because the vt emulator's encoder drops ctrl-modified arrows.
  The overflow footer says `ctrl+shift+→` and lost the word "more" to keep fitting narrow grids.
  `core/keys.md`, `core/tui.md` and the user docs updated; troubleshooting gained a Terminal.app
  note (its default profile does not send the chord). Version 0.10.1.

## 2026-08-01

- Panes are real terminals (issue #206): the hand-rolled scrollback line discipline
  (`internal/scrollback`, 767 lines of case-by-case emulation) is gone; the per-session vt
  emulator (`internal/term`, wrapping charmbracelet `x/vt`) now holds everything a pane shows —
  screen, retained history, cursor, modes. Three consequences. **Rendering**: redraws, cursor
  movement and erase sequences render as the host meant them; `clear` empties the pane while an
  ED-3 guard keeps the history scrollable; the `~ screen cleared ~` marker and per-width
  re-wrapping at render time are gone (a width change reflows the emulator content itself,
  losslessly, via the reflow machinery ported from ike — logical-line replay, height-shrink
  scroll-out, resize reserve). **Input**: keystrokes travel as key events and each host's own
  emulator encodes them (`SendKey`), honouring that host's terminal modes — application cursor
  keys, bracketed paste — so the hand-written key table (`keystrokeBytes`) and its per-key bug
  class are gone; macOS editing chords map to the readline defaults (opt+arrows → `ESC b/f`,
  cmd+arrows → `ctrl+a/e`, opt+backspace → `ESC DEL`, cmd+backspace → `ctrl+u`); the broadcast
  bar fans out events per host through `Router.SendKey`. **Lifecycle**: the emulator outlives
  its session — `ReleaseTerminal`/`Config.Terminal` carry it across reconnects, so the pane
  keeps its history; auth prompts and failure notices inject through the same write path.
  `core/terminal.md` rewritten, `core/scrollback.md` deleted, `core/session.md`, `core/tui.md`,
  `core/keys.md` and the user docs updated. Version 0.10.0.

## 2026-07-31

- Line editing navigation works while typing to a host (issue #202): `cmd+←`/`cmd+→` (super)
  are forwarded as home/end, plain `alt+←`/`alt+→` as `ESC b`/`ESC f` (word navigation), and
  any other unbound `alt+<char>` as meta (`ESC` + character) instead of degrading to the bare
  letter. Pane movement, which sat on the bare alt+arrows, moved to `shift+alt+arrows` — same
  chord plus shift, working while typing and from the app level as before. The text inputs
  (command line, prompts, search) gained `super+left`/`super+right` on their LineStart/LineEnd
  bindings via a shared constructor. `core/keys.md`, `core/tui.md` and the user docs updated.
  Version 0.9.41.

- The exit-hook setup line no longer shows in panes (issue #201): the PTY echoes the injected
  line up to twice — kernel echo plus the line editor's redisplay at the first prompt — and both
  copies landed at the top of every pane's scrollback. The stdout pump now runs through an echo
  filter (`internal/ssh/echofilter.go`), a byte state machine matching the exact setup line
  across any read boundary; it removes at most two copies plus their line breaks, releases
  failed partial matches unchanged, flushes withheld bytes at stream end, and passes everything
  through afterwards. Shells whose echo differs (syntax highlighting) keep their echo — same
  graceful degradation as the hook. `core/session.md` updated. Version 0.9.40.

- Broadcast stops at the page on screen (issue #199): the visibility limit the UI pushes into
  the router is now the grid's **current page** — the filtered and split host list narrowed once
  more by what fits on the terminal — instead of the whole visible chunk. Ten connected hosts on
  a terminal that draws nine panes broadcast to nine; the tenth receives after `ctrl+→`.
  The page moves far more often than a filter does (a resize repages, an arrow turns a page, a
  host leaving reflows the run), so `App.Update` was split into a wrapper that resyncs the limit
  after every message and the old handler beneath it, rather than dusting `syncBroadcastLimit`
  over every paging site. Before the first size message there is no page and an empty limit
  would swallow every keystroke, so the filters-only fallback stays. `fleet` remains unbounded,
  `single` remains the focused pane, and full screen keeps its page's limit rather than turning
  `all` into a one-host send. `core/tui.md`, `core/broadcast-scope.md`,
  `core/groups-and-sessions.md` and the user docs said the opposite rule and were rewritten.
  Version 0.9.39.

- `make install` installs to `~/.local/bin` (issue #197): the target builds straight into
  `$(BINDIR)/lazycssh` — `BINDIR ?= $(HOME)/.local/bin`, created when missing — instead of
  running `go install` into whatever `GOPATH` points at, and `make uninstall` / `make clean`
  were added alongside. `README.md`, `CONTRIBUTING.md` and the installation page updated.
  Version 0.9.38.

- User documentation layer (issue #195): an MkDocs Material site under `userdocs/`
  (`mkdocs.yml`, `strict: true`, built and deployed by `.github/workflows/docs.yml`), plus
  `README.md`, `CONTRIBUTING.md` and `SECURITY.md` at the repository root. The site documents
  *using* lazycssh — getting started, the concepts that must not be confused (window vs working
  set, scope vs targets), one guide per task, a keybinding/CLI/session-file reference and
  troubleshooting — and links to this wiki for internals instead of restating it. All three
  entry points state that the project is built with heavy AI assistance and carries no support
  promise. New `contributing/documentation.md` records which layer a change belongs in;
  `contributing/workflow.md` and `contributing/index.md` updated. Version 0.9.37.

## 2026-07-30

- Broadcast drives full-screen apps when every target is one (issue #191): the alt-screen
  exclusion in `all`/`selected` mode now applies only while the scope is mixed. Open `vim`
  on the fleet through the broadcast line and the next keystrokes reach all of them; one
  shell among the editors restores the protection, and the skip label with it.
  `core/broadcast-scope.md` and `core/terminal.md` updated. Version 0.9.36.

- Host panes show the remote cursor (issue #190): a connected pane following the tail draws
  the cursor where the scrollback's line discipline says it is - prompt end, mid-edit after
  backspaces, the correct row of a wrapped pending line, or the empty row after a line feed.
  Hidden while scrolled back, on disconnected panes and on alt-screen panes (the grid keeps
  its own). `scrollback.Buffer` gains `CursorTail`. `core/scrollback.md` updated.
  Version 0.9.35.

- `clear` works on hosts whose termcap clears via cursor-home + erase-below (issue #189):
  `ESC[H` immediately followed by `ESC[J` — what busybox/minimal terminfo entries emit instead
  of `ESC[2J` — now plants the `ClearMark`, as does a full reset (`ESC c`). An `ESC[J` without
  the preceding home keeps its cleanup meaning. Verified against a captured byte stream from
  the test containers. `core/scrollback.md` updated. Version 0.9.34.

- Arrow-key history navigation no longer corrupts the scrollback (issue #178): the pending
  line carries a cursor mapped through the remote terminal's width, printable runes overwrite
  instead of append, cursor movement (`ESC[A/B/C/D/G`) and per-row erase (`ESC[K`) are
  honoured, and `\n` on an upper row of a multi-row edit moves the cursor down instead of
  committing — so a recalled command that wraps over several screen rows redraws cleanly
  instead of leaving its intermediate states behind. OSC sequences (including the exit-marker
  hook) are consumed, never flushed into the line as text. Verified against a captured bash
  PTY byte stream of the exact repro. `core/scrollback.md` updated. Version 0.9.33.

- Unselected sidebar panels preview their content (issue #186): every panel renders its body
  into the height it was dealt instead of collapsing to an empty box, and on a roomy sidebar
  `SidebarHeights` grows the unselected boxes with half the surplus, capped at 8 rows. Tight
  sidebars degrade exactly as before. `core/tui.md` updated. Version 0.9.32.

- The broadcast line answers every prompting host again (issue #184): auth keystrokes are
  mirrored against the broadcast *scope*, not `Targets()` — the liveness filter in `Targets()`
  drops exactly the sessions that are waiting at a password prompt, so with a transport
  attached the answer reached nobody. `Targeter` gains `Scope()`; the regression test runs
  with the transport attached. `core/broadcast-scope.md` updated. Version 0.9.31.

- Auth prompts are concurrent and per pane (issue #182): every dialling session shows its own
  question in its own scrollback at once — routed by session id, exact, so ten dials of
  `test@localhost` on ten ports prompt ten panes instead of piling onto the first alias match.
  The answer is typed into the focused pane (per-host passwords) or into the broadcast line,
  which mirrors keystrokes into every prompting target and submits them all on enter; a line
  no live host received is never recorded in the command log. `ssh.Prompter` and
  `ssh.HostKeyPrompter` carry the session id; the password cache is keyed per machine
  (user@addr:port) instead of per login user. The modal question keyboard is gone.
  `core/authentication.md` and `core/host-keys.md` updated. Version 0.9.30.

- Auth prompts and connect errors behave like a plain terminal (issue #180): the question is
  written into the blocked host's scrollback in ssh's own wording — `test@db1's password: `,
  `Enter passphrase for key '…': `, the known-hosts block ending in `(yes/no)? ` — and the
  answer is typed inline after it; the host key question now takes a typed `yes`/`no` + enter.
  Masked answers echo nothing and write only the newline; echoing answers and the yes/no stay
  in the history. A failed session prints its error into its own scrollback instead of the
  styled overlay at the pane bottom. The `AUTH <host>` status bar segment and the Status-panel
  fallback stay. `core/host-keys.md`, `core/authentication.md` and `core/tui.md` updated.
  Version 0.9.29.

- Auth questions render in the host's own pane (issue #177): an unknown-host-key question or a
  secret prompt focuses the affected pane and draws the question at the bottom of its body, so
  it cannot be missed and its host is never in doubt; the status bar says `AUTH <host>` for as
  long as it is open. The Status panel remains the fallback for a host without a visible pane.
  `Prompter.Passphrase` now takes the host whose dial hit the encrypted key, so every secret
  question can name a pane. `core/host-keys.md` and `core/authentication.md` updated.
  Version 0.9.28.

## 2026-07-31

- Passwords, passphrases and keyboard-interactive answers are asked for in the TUI (issue
  #175, closing the interactive half of #87): a masked input in the Status panel, one question
  at a time over the same channel bridge as the host key question; enter submits, esc cancels
  and fails that attempt. The transport's caches keep it to one password per login user and one
  passphrase per key file for the whole fleet; nothing is logged or written to disk.
  `core/authentication.md` updated. Version 0.9.27.

- Unknown host keys are asked about in the TUI (issue #173): a dialling session that meets a
  key not in known_hosts blocks while the Status panel shows the alias, key type and SHA256
  fingerprint — `y` accepts and appends to `~/.ssh/known_hosts`, `n`/`esc` rejects and fails
  that pane, one question at a time per host. A changed key stays a hard refusal. The transport
  side (`HostKeyPrompter`) existed; this adds the `keyPrompter` channel bridge in
  internal/program and the question modal in internal/ui. `core/host-keys.md` updated.
  Version 0.9.26.

- A closed host leaves a hole, not a reflow (issue #169): the departed pane's slot renders as
  an empty frame exactly where it was — held as a `""` marker in the session's host list — and
  the survivors keep their positions. Focus, clicks and broadcast skip holes (`nonHoles`);
  `ctrl+r` and every explicit view change (session switch, filter, split) compact them.
  `core/tui.md` and `core/groups-and-sessions.md` updated. Version 0.9.25.

- Failed panes say why (issue #167): a pane whose connection failed renders the session's
  error — DNS, refused, auth, host key — at the bottom of its body in the failure style,
  wrapped and capped at half the pane height. The error is snapshotted into the model
  (`hostState.errText`) so View keeps its hands off live session state. `core/tui.md` updated.
  Version 0.9.24.

- The emptied run stays open (issue #168): losing the last host no longer quits the program —
  the TUI falls back to the neutral argumentless start (sidebar focus, no kept grid shape, no
  filters) and waits for the next group or connect. Quitting stays what `q`/`ctrl+q` are for.
  `core/groups-and-sessions.md` updated. Version 0.9.23.

- Broadcast bar declutters (issue #164): the bar no longer mirrors typed text — the panes carry
  each host's own echo, and the line is still assembled internally for the command log — and it
  no longer stretches under the sidebar: the panel column runs down to the status bar and the
  bar sits under the grid only, full-width only when the sidebar is hidden. `core/tui.md`
  updated. Version 0.9.22.

- Makefile added (issue #163): `make build` / `make install` / `make test` / `make vet` wrap
  the go commands from CLAUDE.md; `make install` runs `go install ./cmd/lazycssh`.
  Version 0.9.21.

- Broadcast keeps its hands off full-screen apps (issue #158, closing epic #44): `all` and
  `selected` mode exclude hosts whose remote app is on the alternate screen from the target
  set — a keystroke meant for one `vim` must not reach twenty of them — while `single` still
  types into the app and `fleet` stays the literal every-host escape hatch. The router's
  `Sessions` interface gained `AltScreen`, `AltScreenSkipped()` names the excluded hosts, and
  `Describe` spells the skip out in the status bar ("6/8 up, 1 alt-screen skipped").
  `core/broadcast-scope.md` and `core/terminal.md` updated. Version 0.9.20.

- Terminal query replies reach the host (issue #157, epic #44): both session implementations
  wire the emulator's reply handler to their own stdin, so device-attribute and
  cursor-position answers go to exactly the session that asked — never through broadcast.
  A session that is not connected drops the reply instead of queueing it. Full-screen apps
  that wait for these answers now start cleanly. `core/terminal.md` updated. Version 0.9.19.

- Alt-screen panes render the live emulator grid (issue #156, epic #44): a session whose
  emulator reports the alternate screen draws the emulator's screen — clipped to the pane
  body, with the remote app's cursor drawn and `?25l` respected — instead of scrollback text.
  Scroll, search and text selection are no-ops while the grid is active; leaving the alternate
  screen returns to the scrollback view with the pre-app history still scrollable.
  `internal/term` gained `CursorVisible()`. `core/terminal.md` updated. Version 0.9.18.

- Per-session terminal emulator (issue #155, first slice of epic #44): new `internal/term`
  wraps the charmbracelet `x/vt` emulator; every session — real and fake — owns one, fed by
  the same output pump as the scrollback and resized in lockstep with the PTY, exposed via
  `Terminal()`. The wrapper drains the emulator's blocking reply pipe unconditionally (a
  `vim` device-attributes query would otherwise freeze the reader goroutine) and hands
  replies to an optional handler for #157. No rendering change yet. New concept document
  `core/terminal.md`; `core/session.md` updated. Version 0.9.17.

- Mouse text selection in panes (issue #149): drag with the left button highlights pane text
  (reverse video, stream-shaped, clamped to the owning pane's body), and `ctrl+c` copies it
  over OSC 52 - ANSI stripped, trailing whitespace trimmed - clears it and sends no interrupt,
  with the status line saying so; without a selection `ctrl+c` is unchanged. The selection is
  screen-cell anchored and clears on click-without-drag, esc, focus/grid/page/chunk changes and
  scrolling; new output redraws beneath it. `core/tui.md` and `core/keys.md` updated.
  Version 0.9.16.

- The broadcast bar's `ctrl+a` prefix hands the next key to lazycssh (issue #148):
  `resolveBroadcastEscape` dispatches any key other than `a`/`esc` through the app keymap as a
  one-shot command - `ctrl+a ctrl+a` toggles connected-only, `ctrl+a ?` opens help - instead of
  cancelling with an error. An unbound key is a named no-op; `ctrl+a a` still sends exactly the
  literal 0x01 and the prefix never chains. Status-bar hint and help text updated.
  `core/tui.md` and `core/keys.md` updated. Version 0.9.15.

- One navigator for "the next screenful" (issue #147): `ctrl+→`/`ctrl+←` now step pages inside
  the current split chunk, then the chunk, wrapping at both ends; with the split off they are
  plain page navigation with wrap. `alt+n`/`alt+p` are removed from the keymap and the overflow
  footer names `ctrl+→` instead. While a pane or the bar has the keyboard the chords stay
  keystrokes for the hosts. `core/tui.md` updated. Version 0.9.14.

- Closed two silent-drop holes in the broadcast path (issue #133): a target whose writer
  vanished between target resolution and the write now fails the delivery with an error instead
  of being skipped quietly, and the broadcast bar reports zero-delivery keystrokes
  ("no host can take input right now") instead of accepting typing that reaches nobody. No
  reproduction of the original observation existed; these are the two code paths where typed
  input could vanish while looking accepted, and both now surface in the status line.
  `core/broadcast-scope.md` updated. Version 0.9.13.

- Failed panes no longer block shutdown, closed panes close themselves (issue #146): a clean
  shell logout removes its own pane from the run (grid shape kept — no retile), the owning
  sessions remember the logout (`SawClose`), and a session is over once every host is done and
  a logout was seen — so a dead-on-arrival host cannot keep the run alive after everyone else
  logged out. An all-failed session with no logout still stays listed as an outage. A run that
  held hosts and emptied quits the program; a run that started empty keeps waiting on the
  picker. `core/groups-and-sessions.md` updated. Version 0.9.12.

## 2026-07-31

- Deduplicated `core/tui.md` (issue #144): the "Pane grid" and "The window" sections existed
  twice — a nested copy under Focus and a second window section without the overflow-footer
  material. One copy of each remains, the fuller one; no content was unique to the deleted
  copies. Issues #136 and #139 had to edit both copies, which is the drift this removes.
  Version 0.9.11.

## 2026-07-30

- Session state renders from the model, not the transport (issue #136): `FleetUpdatedMsg` makes
  `Update` re-read the whole fleet into a model snapshot (`App.snapshotFleet` — host list,
  per-session state, last exit, counts in one consistent pass), and every render helper reads
  those fields. No `Session.State()`/`LastExit()`/`Counts()` call is reachable from `View`; a
  spy-fleet test and a `-race` state-flip hammer pin that down. The connected-only filter stays
  a live view — the fleet event refreshes the snapshot and the redraw follows. The scrollback
  body stays a live read on purpose (internally synchronized buffer, documented). The per-frame
  `frameHosts` freeze from #135 is gone: `hostIDs()` is now a pure function of the model.
  `core/tui.md` and `core/program.md` updated. Version 0.9.10.

## 2026-07-30

- Raised the pane floor to a 45×16 terminal per host (issue #139): the grid never hands a host
  less content than that — a 47×19 cell with border and header — and pages the overflow as
  before. The value is a guideline tuned in one place (`MinPaneContentWidth`/`Height` in
  `internal/ui/grid.go`); the PTY floor follows automatically because the remote is sized to
  the same content arithmetic. `core/tui.md` updated. Version 0.9.9.

## 2026-07-30

- Text can be copied out of a pane (issue #134): `alt+y` puts the focused pane's visible text
  into the system clipboard, `alt+d` the whole retained scrollback, both over OSC 52 so they
  work through SSH and degrade silently on terminals without support — the status line reports
  the attempt either way. Clipboard text is ANSI-stripped, clear markers excluded. Both chords
  are in the `?` overlay. `core/keys.md` and `core/tui.md` updated. Version 0.9.8.

## 2026-07-30

- The Command log panel budgets its window in visual lines (issue #132): a command wrapping
  over several lines no longer pushes the cursor row past the box's clip, so up/down stays one
  entry per keypress with the cursor always on screen. The dropped-entries notice yields its
  line before the cursor entry does. `core/command-log.md` updated. Version 0.9.7.

## 2026-07-30

- `clear`, `screen` and friends now clear the pane (issue #131): erase-display sequences and
  alternate-screen switches plant a `ClearMark` in the scrollback; the pane following the tail
  starts after the last marker, so the view is empty while the history stays scrollable above
  a `~ screen cleared ~` marker. `ESC[3J` deliberately does not wipe the ring.
  `core/scrollback.md` updated. Version 0.9.6.

## 2026-07-30

- Fixed the render crash under the connected-only filter (issue #135): a mass disconnect could
  shrink the visible host list between two `hostIDs()` computations of the same frame, and
  `renderPane` indexed past the end. `View` now freezes the list once per frame
  (`App.frameHosts`); guard-then-recompute call sites (`renderPane`, `paneHeader`,
  `FocusedHost`, `refocus`) fetch the list once. Follow-up for moving session state fully into
  the model is filed separately. `core/tui.md` updated. Version 0.9.5.

## 2026-07-30

- The broadcast bar is modal, csshx style: edit mode (default) sends every keystroke to the
  targets; `ctrl+a` `esc` switches to view mode, where keys are app-level commands and nothing
  is sent, and `enter` returns to edit. `ctrl+a` `a` sends a literal `ctrl+a` for remote
  `screen`/`tmux`; an unknown key after the prefix cancels it audibly. The status bar carries
  `EDIT`/`VIEW` and the armed prefix. The prefix shadows the global connected-only `ctrl+a`
  only inside the bar's edit mode; view mode reaches the toggle again. `core/tui.md` and
  `core/keys.md` updated. Version 0.9.4.

## 2026-07-30

- A host leaving the run no longer reflows the grid: the shape is kept and the freed cell
  renders empty; `ctrl+r` re-tiles on request and resizes the PTYs. Joins still grow the grid
  immediately, and explicit view changes (session switch, `ctrl+a`, `ctrl+s`) tile for the new
  view. `core/tui.md` and `core/keys.md` updated. Version 0.9.3.

## 2026-07-30

- The grid announces its own overflow: when panes are hidden — more pages, more split chunks,
  more open sessions — a footer line under the panes says what is hidden and which key reaches
  it (`+12 more hosts — alt+n · page 1/3 · 2 more sessions — [3]`). The footer costs the grid
  one row and is skipped in full screen. `core/tui.md` updated. Version 0.9.2.

## 2026-07-30

- `x` in the Sessions panel ends a session after `end "name"? y/n`: every connected terminal
  receives `ctrl+c` then `ctrl+d` via the pane path (never logged), the row shows `(ending)`,
  and the session leaves the list once its hosts are done. A session whose hosts all reach
  `closed` — `ctrl+d` broadcast, say — ends by itself, its hosts leaving the run unless another
  open session holds them; an all-failed session stays listed. `core/groups-and-sessions.md`,
  `core/tui.md`, `core/keys.md` updated. Version 0.9.1.

## 2026-07-29

- `ctrl+s` splits the grid into chunks of a chosen size — ten hosts split by five shows the
  first five terminals — and `ctrl+→`/`ctrl+←` step between chunks without wrapping. Broadcast
  `all`/`selected` reach only the visible chunk via the visibility limit; the status bar says
  `SPLIT 1/2 (5 hosts)`. Empty or `0` clears, `esc` keeps, composes with `ctrl+a`, and the
  chords stay keystrokes for the hosts while typing. `core/tui.md` and `core/keys.md` updated.
  Version 0.9.0 — the minor bump closes the groups/sessions/visibility epic (#116).

- `ctrl+a` toggles a connected-only view: panes of hosts that cannot take input leave the grid,
  broadcast `all`/`selected` follow the visible set via the router's visibility limit, and the
  status bar says `CONNECTED HOSTS ONLY` for as long as it narrows anything. While typing into a
  pane or the broadcast bar the chord stays a keystroke for the hosts. `core/tui.md` and
  `core/keys.md` updated. Version 0.8.3.

- Groups and open sessions. The Groups panel now manages the saved session files as **groups**:
  `n` creates one in a two-question dialog (name, then host patterns), `d` deletes after `y/n`,
  `enter`/`space` opens one as a session — resolved through `~/.ssh/config` as every connect is.
  Opening while another session is on screen backgrounds it without dropping connections; the
  Sessions panel lists the open sessions and `enter`/`space` foregrounds one. The grid and the
  broadcast scope (`all`/`selected`, via the router's new visibility limit) follow the
  foreground session; `fleet` mode stays the every-host escape hatch. `w`/`S` save prompt moved
  to the Groups panel. New `core/groups-and-sessions.md`; `core/tui.md`, `core/keys.md`,
  `core/broadcast-scope.md`, `core/session-files.md` updated. Version 0.8.2.

- An argumentless start no longer opens the host prompt: it starts on the Status panel with no
  input focused, and the empty-grid hint names the options. Forcing the first action was wrong —
  connect, launch a session or just look around is the user's call. `n` opens the prompt as
  before. `core/tui.md` and `core/cli.md` updated. Version 0.8.1.

- The Hosts panel is gone; the pane grid is the host list. The sidebar is Status `1`, Groups
  `2`, Sessions `3`, Command log `4`, and the broadcast bar moved to `5`. `n` now works from
  anywhere: the free-text host prompt opens in the Status panel with the not-yet-connected
  `~/.ssh/config` aliases listed beneath it, filtered as you type, `tab` completing the first
  match. `alt+space` toggles the focused pane's host in the broadcast selection (grid and app
  level); `a`/`i`/`c`/`u`/`d` are app-level selection keys; `/select` globs are unchanged.
  `ctrl+]` lands on the Status panel, connect errors render there too, and `ctrl+q` now quits
  from inside every text input — the auto-opened prompt of an argumentless start must not trap
  the user. Sidebar-only `x`/`r`/`/`(filter) died with the panel; `alt+x`/`alt+r` and the mouse
  `[x]` cover close and reconnect. `core/tui.md`, `core/keys.md`, `core/cli.md`,
  `core/broadcast-scope.md`, `core/host-resolution.md`, `core/working-sets.md` and
  `core/authentication.md` updated. Version 0.8.0.

- Plain `q` quits, lazygit style, wherever no input has the keyboard — sidebar, app level, the
  help overlay. While typing to a host, in the broadcast bar or in any text input, `q` stays a
  letter and is forwarded as before; `ctrl+q` keeps working everywhere it did. `core/keys.md`
  updated. Version 0.7.5.

- Minimal line discipline in the scrollback buffer: backspace removes the last rune of the line
  being assembled and `ESC[K` erase-line sequences are interpreted (right-of-cursor silently
  consumed, `1K`/`2K` discard the line), with escape sequences reassembled across write
  boundaries. Recalling a command with arrow-up in a remote shell now replaces the visible line
  in the pane instead of appending to it. Cursor stays modelled at end of line; full emulation
  remains issue #44. `core/scrollback.md` updated. Version 0.7.4.

- Focus borders match lazygit: the focused panel and pane keep the same border weight and only
  change colour, and `Focus` is now green (`#9ece6a` dark, `#2e7d32` light) instead of blue.
  The thick border survives only as the `NoColor` fallback, where colour cannot carry focus.
  `core/theme.md` updated. Version 0.7.3.

- Simplification sweep for the three-destination model: the Status panel opens with a literal
  `keys go to:` line naming the focused host, the broadcast targets, or lazycssh; the empty-run
  hint explains typing and the `6` bar; tab's help text says "next panel". `core/tui.md` and
  `core/cli.md` updated. Version 0.7.2.

- Quick save and truthful patterns: global `S` opens the prefilled save prompt from anywhere at
  the app level (forwarded to the hosts while typing, by design). The program now tracks the
  run's live pattern list — CLI arguments plus runtime connects and session launches, deduped;
  exact-name patterns dropped on removal — and hands it to the UI with every `HostsChangedMsg`,
  fixing the bug where a run extended at runtime saved only its original patterns. Saving an
  empty run reports the error without discarding the prompt or the typed name.
  `core/session-files.md`, `core/program.md` and `core/keys.md` updated. Version 0.7.1.

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
