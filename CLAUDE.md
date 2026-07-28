# lazycssh

Terminal UI for parallel SSH. Inspired by `lazygit` / `lazydocker`, replaces classic `cssh` (cluster ssh).

**Core purpose:** open SSH sessions to many hosts at once, broadcast keyboard input to all of them simultaneously, and watch every host's output in one screen.

Status: early. Only the module skeleton exists (`cmd/lazycssh`, `internal/version`, CI). Everything below beyond that is the target design, not a description of existing files.

## Stack

- Go (module `github.com/TrueDaerk/lazycssh`, matching the remote)
- TUI: **bubbletea v2** — `github.com/charmbracelet/bubbletea/v2`
- Styling: `github.com/charmbracelet/lipgloss/v2`
- Widgets: `github.com/charmbracelet/bubbles/v2` (viewport, textinput, list, help, key)
- SSH: `golang.org/x/crypto/ssh` preferred over shelling out to `/usr/bin/ssh`. Gives per-session PTY control, window resize, and clean stdin fan-out. Fall back to spawning `ssh` subprocesses only if agent-forwarding / jump-host / `~/.ssh/config` parity becomes blocking.
- SSH config parse: `github.com/kevinburke/ssh_config` for `~/.ssh/config` host lookup and glob patterns.

**bubbletea v2 matters.** Do not copy v1 examples. Differences that bite:

- `Init()` has signature `Init() (Model, Cmd)` — returns the model too.
- Key messages are `tea.KeyPressMsg` / `tea.KeyReleaseMsg`, not `tea.KeyMsg`. Match with `msg.String()` or `msg.Key()`.
- `tea.Model.View()` may return `fmt.Stringer`-ish content / cursor info depending on the program type — check the actual v2 API before writing view code, don't guess.
- Program options and mouse handling were renamed. When unsure, read the vendored source under `$GOPATH/pkg/mod/github.com/charmbracelet/bubbletea/v2@*/` rather than web memory.

## Layout (planned)

```
cmd/lazycssh/main.go     entrypoint, flag parsing
internal/ui/             bubbletea models
  app.go                 root model, layout, focus routing
  pane.go                per-host pane model (viewport + status)
  keys.go                keymap + help bindings
  theme.go               lipgloss styles
internal/ssh/            transport layer
  session.go             one host: dial, PTY, stdin writer, output reader
  manager.go             fan-out/fan-in over N sessions
  config.go              ~/.ssh/config + known_hosts + auth (agent, key, password)
internal/hosts/          host list resolution: args, groups, globs, files
internal/broadcast/      input router: all / selected / single
```

## Design rules

**Concurrency.** Each SSH session runs its own goroutines (read stdout, read stderr, write stdin). They never touch the bubbletea model directly. They emit output over a channel; the UI drains it with a `tea.Cmd` and converts to messages (`OutputMsg{HostID, []byte}`, `SessionStateMsg`, `SessionErrMsg`). Model mutation happens only inside `Update`.

**Backpressure.** A chatty host must not stall the UI. Per-session ring buffer with a bounded scrollback (default ~10k lines); drop oldest, never block the reader goroutine.

**Broadcast semantics.** Input goes to the *active broadcast set*, not blindly to all. Modes:
- `all` — every connected session (default)
- `selected` — user-toggled subset
- `single` — only focused pane (for interactive prompts, `sudo` password, vim)

Any keystroke going out must be visibly indicated — the user must always know how many hosts will receive it. Show it in the status bar, e.g. `BROADCAST all (7/8 up)`.

**Panic containment.** One dead host = one dead pane. Never tear down the program. Reconnect is a per-pane action.

## Interaction model (lazygit-like)

- Panes on a grid; `tab` / arrows move focus.
- Single-letter commands act on focus (`r` reconnect, `x` close, `space` toggle selection).
- `:` or a dedicated input line for the broadcast command — typed once, sent to N hosts.
- `?` opens the help overlay, generated from the keymap (`bubbles/key` + `bubbles/help`) so docs cannot drift from bindings.
- Layout auto-tiles by host count; `1`/`2`/`3` switch to full-screen focus of a pane.

## Security constraints

- Host key verification **on** by default against `~/.ssh/known_hosts`. Unknown key = pane blocks with a prompt, not a silent accept. An opt-out flag may exist but must be explicit (`--insecure-ignore-host-key`) and shown in the status bar while active.
- Never log passwords, key passphrases, or session bytes to disk by default. Session logging is opt-in per run.
- Prefer `ssh-agent` auth; ask for passphrases via a masked `textinput`, keep them in memory only.
- Broadcasting a destructive command to N hosts is the whole point of the tool and also its main footgun — do not add "confirm every command" friction, but do make the target count unmissable before send.

## Process — read before changing code

Conventions, workflow and versioning live in the wiki, not here. They are binding, not optional reading:

- [`wiki/contributing/conventions.md`](wiki/contributing/conventions.md) — gofmt/vet, error wrapping, tests, no global state
- [`wiki/contributing/workflow.md`](wiki/contributing/workflow.md) — issue-driven work, one issue = one branch, PR closing sequence
- [`wiki/contributing/versioning.md`](wiki/contributing/versioning.md) — semver policy, `internal/version` is the source of truth
- [`wiki/contributing/wiki-format.md`](wiki/contributing/wiki-format.md) — OKF v0.1 rules for the `wiki/` bundle

Short version: never commit on `main`; every change starts from a GitHub issue and ends in a merged PR that also updates the wiki and bumps the version.

**Tests are not optional.** Every exported behavior with branches or error paths gets tests, and every bugfix ships with a test that fails without the fix. Pure delegation and rendering glue are exempt — this is not a test-per-function rule, and there is no coverage threshold. Details in `conventions.md`.

## Commands

```sh
go build ./cmd/lazycssh
go run ./cmd/lazycssh host1 host2 host3
go test ./...
go vet ./...
```

TUI cannot be verified from stdout alone. To check a change visually, run it in a real terminal; for automated checks, test the model's `Update`/`View` in isolation with synthetic messages.

## Non-goals

- Not a config management tool. No inventory sync, no playbooks.
- Not a terminal multiplexer replacement. tmux integration is out of scope.
- No full VT100 emulation ambitions at the start — render output as scrollback text. Full-screen remote apps (`vim`, `htop`) in a pane are a later, deliberate feature, not an accident.

## Wiki

`wiki/` is an OKF v0.1 bundle. Start at [`wiki/index.md`](wiki/index.md); the format rules are in
[`wiki/contributing/wiki-format.md`](wiki/contributing/wiki-format.md).

**Keep it current.** When you change behavior the wiki documents, update the matching concept document
in the same change, refresh its `timestamp`, and add a `log.md` entry. The wiki is part of the deliverable.
