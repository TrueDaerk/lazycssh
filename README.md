# lazycssh

**lazycssh** is a terminal UI for parallel SSH — a `lazygit`-shaped replacement
for classic `cssh`. It opens SSH sessions to many hosts at once, broadcasts your
keystrokes to all of them, and shows every host's output in one screen.

Built with [bubbletea v2](https://github.com/charmbracelet/bubbletea) and
`golang.org/x/crypto/ssh` — no `ssh` subprocesses, no shell in the middle.

📖 **[Documentation](https://truedaerk.github.io/lazycssh/)** — installation,
first run, keybinding reference, and the safety model behind broadcasting.

> [!NOTE]
> lazycssh is a personal project: built by one person, to that person's taste,
> with heavy AI assistance. There is no support promise. It is public on
> purpose, though — use it if it suits you, and
> [pull requests](CONTRIBUTING.md) that improve it are genuinely welcome.

## Installation & usage

lazycssh is a single Go binary. You need [Go 1.26+](https://go.dev/dl/).

```sh
git clone https://github.com/TrueDaerk/lazycssh.git
cd lazycssh
make install                        # installs to ~/.local/bin/lazycssh
make install BINDIR=/usr/local/bin  # or pick another directory
```

(Or build without installing: `make` produces `./lazycssh`. `make uninstall`
removes an installed binary.)

Start it with hosts, with a saved group, or with nothing at all:

```sh
lazycssh                                # empty run; press A to pick hosts, n to type one
lazycssh web-01 web-02 db-01            # three hosts
lazycssh 'srv1-{01..40}.example.com'    # brace expansion, done by lazycssh
lazycssh @prod-web                      # a saved group
lazycssh @prod-web canary.example.com   # a group plus one more host
```

Aliases work because lazycssh reads the same `~/.ssh/config` ssh does —
`HostName`, `User`, `Port`, `IdentityFile` and `ProxyJump` all apply.

### The first five keys

| Key | What it does |
|---|---|
| `n` | connect a host (any pattern; `~/.ssh/config` aliases complete with `tab`) |
| `5` | focus the broadcast bar — every keystroke goes to the whole target set |
| `enter` / click on a pane | type into that one host; `ctrl+]` gives the keyboard back |
| `:` | send one command to the broadcast set |
| `?` | the full keybinding overlay |

The status bar always says where your keys go and how many machines will
receive them — `BROADCAST all (7/8 up)`, `TYPING web-01 — ctrl+] leaves`.

## Features

- **Broadcast with an honest target count** — four scopes (`all` = the working
  set, `selected`, `single`, and the explicit every-host `fleet` mode). The
  number of machines about to receive a keystroke is on the status bar at all
  times, which is why there is no confirmation dialog.
- **A pane per host** — auto-tiled grid, the squarest arrangement that keeps
  every pane readable; when the hosts stop fitting the grid *pages* rather than
  shrinking. A host leaving does not reflow the survivors.
- **Live terminals, not a log view** — focus a pane and type into that host,
  keystroke by keystroke: `ctrl+c`, `tab`, `esc` and arrows all belong to the
  remote shell.
- **Full-screen apps** — a per-session vt emulator carries `vim`, `htop` and
  `less`; a pane on the alternate screen renders the live grid, and mixed
  scopes exclude those hosts from a broadcast so one editor does not swallow
  keystrokes meant for forty shells.
- **Working sets** — narrow forty hosts to "the first twenty" or `web-*`, page
  through them, and give the definition a name. `all` means the working set,
  never more.
- **Groups and open sessions** — saved host lists on disk, opened into named
  runtime sessions; several can be open at once, one is in the foreground, and
  backgrounding never drops a connection.
- **Per-command exit status** — after a broadcast command each pane header says
  how it ended on that host: `·` while it runs, `✓` for zero, `exit N` for a
  failure plus a danger border. The status bar counts the failures and `!` jumps
  between them. A shell that reports no exit codes shows nothing rather than a
  tick it cannot back.
- **Scrollback per host** — bounded ring buffer (10,000 lines), search with `/`
  (`alt+/` while typing to a host), `n`/`N` between matches with a `3/17`
  counter, `esc` back to where you were, or `/find <text>` for "which of my
  hosts printed this"; keyboard and mouse copy over OSC 52, `alt+w` exports a
  pane's whole scrollback to a file for a postmortem.
- **Host key verification on by default** — an unknown key asks in the pane,
  ssh-style; a *changed* key is a hard failure with no click-through. The
  opt-out is an explicit flag that stays on the status bar.
- **Credentials that stay in memory** — ssh-agent first, then identity files,
  then a prompt in the pane that asked. Session files never carry a password;
  a `secret_command` (argv, never a shell line) delegates to `pass`, `op` and
  friends.
- **Command log** — what this run sent, to how many hosts, in which mode.
  Keystrokes and `single`-mode input are deliberately never recorded, because
  that is where a sudo password is typed.
- **Opt-in session logging** — `--log-dir DIR` writes each host's output to
  its own file (rotated, `0600`), and the status bar says `SESSION LOGGING ON`
  for the whole run. Keystrokes are never written, and logging pauses — with a
  visible marker — while `single` mode is answering a password prompt. Off by
  default: session bytes only reach disk when this run asked for it.
- **Configurable keys** — an optional `~/.config/lazycssh/keys.yaml` remaps any
  action; `--list-key-actions` prints the vocabulary, an unknown action or an
  impossible key name fails at startup rather than silently, and the `?`
  overlay follows the effective bindings. `ctrl+a` stays the command prefix and
  `ctrl+a ctrl+a` the literal pass-through, whatever the file says.
- **Mouse support** — click a pane to type into it, drag to select text, wheel
  to scroll the pane under the pointer.

The [`wiki/`](wiki/index.md) directory holds the architecture documentation —
one concept document per subsystem.

## Development

- Planning lives in [GitHub issues](https://github.com/TrueDaerk/lazycssh/issues);
  the workflow is in [`wiki/contributing/workflow.md`](wiki/contributing/workflow.md).
- `make build` / `make test` / `make vet` — CI runs `go test -race ./...`.
- Contributor guide: [`CONTRIBUTING.md`](CONTRIBUTING.md). Security reports:
  [`SECURITY.md`](SECURITY.md).
- The documentation site is built from [`userdocs/`](userdocs/index.md) with
  MkDocs Material and deployed to GitHub Pages on every push to `main`.
  Preview it with `pip install -r userdocs/requirements.txt && mkdocs serve`.

## License

[MIT](LICENSE).
