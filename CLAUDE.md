# lazycssh

Terminal UI for parallel SSH. Inspired by `lazygit` / `lazydocker`, replaces classic `cssh` (cluster ssh).

**Core purpose:** open SSH sessions to many hosts at once, broadcast keyboard input to all of them simultaneously, and watch every host's output in one screen.

Status: greenfield. No code yet — everything below is the target design, not a description of existing files.

## Stack

- Go (module `github.com/geant/lazycssh` unless decided otherwise)
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

## Conventions

- `gofmt` (or `gofumpt`) clean; `go vet` clean.
- Errors wrapped with `fmt.Errorf("...: %w", err)`. No naked `panic` outside `main`.
- Exported identifiers documented; unexported ones only where non-obvious.
- Table-driven tests. The SSH layer is behind an interface so the UI tests run without a network — fake sessions, not real dials.
- No global state. Config and dependencies passed into constructors.

## Workflow

Work is tracked in **GitHub issues**. Every feature and every bugfix starts as an issue and is closed by a merged PR. Use `gh` for all of it.

One issue = one branch. Never work on `main` directly.

```sh
gh issue list
gh issue view <n>
git switch -c <type>/<n>-<short-slug>   # feat/12-broadcast-modes, fix/34-pane-crash
```

When an issue is done, the full closing sequence runs — none of these steps are optional:

1. **Docs** — update the wiki concept document(s) the change touches, refresh `timestamp`, add a `log.md` entry. Update `README.md` if user-facing behavior or flags changed.
2. **Version bump** — see below. Commit it with the rest of the work.
3. **PR** — `gh pr create` against `main`. Body references the issue with `Closes #<n>` so it auto-closes on merge.
4. **Merge** — merge into `main` once checks pass.
5. **Cleanup** — delete the branch locally and remotely (`git branch -d`, `git push origin --delete` or `gh pr merge --delete-branch`), and `git switch main && git pull`.

### Versioning

Semver, single source of truth: a `Version` constant in `internal/version` plus a matching `v<x.y.z>` git tag on `main` after merge.

- **Patch** (`0.1.3` → `0.1.4`) — the default. Bump after every closed issue: bugfixes, small features, refactors, docs-only changes that ship.
- **Minor** (`0.1.4` → `0.2.0`) — on request, or occasionally for a large new feature. When a change feels minor-worthy, bump it and say so; the user can correct it.
- **Major** — **never** bump on your own initiative. Only on explicit request.

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

The `wiki/` directory is an **OKF (Open Knowledge Format) v0.1** bundle — hierarchical markdown organized
for progressive disclosure by humans and agents. The format is specified at
<https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md>.

Rules that matter when reading or writing the wiki:

- **Concept documents** (every `.md` that is not a reserved file) MUST have parseable YAML frontmatter
  with a non-empty `type` field. Also include the recommended fields: `title`,
  `description` (one-line summary), and where the concept is backed by source, `resource`
  (a repo-relative path to the code it documents). `tags` and `timestamp` (ISO 8601) are optional but encouraged.
- **Reserved files** are `index.md` and `log.md`:
  - `index.md` provides directory listings for progressive disclosure and contains **no frontmatter**
    (the sole exception: the root `index.md` may carry `okf_version: "0.1"`). Entries use `* [Title](url) - description`.
  - `log.md` (optional) records changes newest-first under `## YYYY-MM-DD` headings.
- **Cross-links** are bundle-relative (`/core/config.md`) or relative (`./other.md`);
  broken links are tolerated (may point at future docs).
- Consumers must tolerate unknown `type` values, unknown keys, and missing optional fields gracefully.

**Keep the wiki current.** When you change behavior the wiki documents (a feature, a subsystem, the architecture),
update the matching concept document in the same change, refresh its `timestamp`, and add a `log.md` entry. Treat the
wiki as part of the deliverable, not an afterthought.
