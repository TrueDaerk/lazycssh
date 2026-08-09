# Installation

lazycssh is a single Go binary with no runtime dependencies. It talks SSH
itself (`golang.org/x/crypto/ssh`) rather than spawning `ssh` processes, so
there is nothing else to install.

## Build from source

You need [Go 1.26+](https://go.dev/dl/).

```sh
git clone https://github.com/TrueDaerk/lazycssh.git
cd lazycssh
make install                        # installs to ~/.local/bin/lazycssh
make install BINDIR=/usr/local/bin  # or pick another directory
```

`BINDIR` is created when it does not exist. `make uninstall` (with the same
`BINDIR`) removes the binary again.

Or build without installing:

```sh
make                  # produces ./lazycssh
go build ./cmd/lazycssh
```

Make sure the install directory is on your `PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

## Check what you built

```sh
lazycssh --version
```

```
0.9.37 (3933d0b)
0.9.37 (3933d0b-dirty)
```

The revision comes from the build information the Go toolchain embeds, so it is
present in a `go build` binary and absent when the source came from a tarball.

## Terminal requirements

Any reasonably modern terminal works — lazycssh uses standard truecolor SGR and
cell-motion mouse reporting, not the Kitty keyboard protocol. Two features are
nice to have:

- **OSC 52 clipboard support**, for ++alt+y++ / ++alt+d++ / ++ctrl+c++ copy.
  Terminals without it ignore the sequence; lazycssh still reports what it
  attempted. OSC 52 is also what makes copy work when lazycssh itself is
  running over SSH.
- **A reasonably large window.** A pane never shows its host less than a 45×16
  terminal; when the hosts stop fitting the grid pages instead of shrinking
  further. Below 24×4 the whole interface degrades to a single "terminal too
  small" line.

## What lazycssh reads from your setup

| Path | Used for |
|---|---|
| `~/.ssh/config` | host aliases, `HostName`, `User`, `Port`, `IdentityFile`, `ProxyJump` |
| `~/.ssh/known_hosts`, `~/.ssh/known_hosts2` | host key verification, hashed entries included |
| `$SSH_AUTH_SOCK` | ssh-agent authentication, tried first |
| `$XDG_CONFIG_HOME/lazycssh/sessions/` | saved groups (`~/.config/lazycssh/sessions/` when unset) |
| `$XDG_CONFIG_HOME/lazycssh/history` | command line history (`~/.config/lazycssh/history` when unset) |

Nothing is written outside the sessions directory and the history file, and
nothing written there ever contains a credential — see
[Security model](../concepts/security.md).
