# Command line reference

```
lazycssh [flags] [<host|@session>...]
```

## Arguments

| Form | Meaning |
|---|---|
| `host` | a host, resolved through `~/.ssh/config` |
| `user@host:port` | with an explicit login user and port |
| `[2001:db8::1]:2222` | IPv6 with a port needs brackets; a bare IPv6 address takes the default port |
| `srv1-{01..40}.example.com` | brace expansion, performed by lazycssh (quoting changes nothing) |
| `@name` | a saved [session file](session-files.md) |

Hosts and sessions may be mixed, and the order is preserved — it is the order
panes are tiled in. When more than one session is named, the last one's
broadcast mode wins, and the last one that sets a working set wins. A session
name that is not saved is an error listing the ones that are.

No arguments is not an error: it opens the TUI on an empty run.

## Flags

| Flag | Effect |
|---|---|
| `--version` | Print the version and exit. Takes precedence over every other argument. |
| `-h`, `--help` | Print usage and exit. |
| `--insecure-ignore-host-key` | Accept any host key without checking `known_hosts`. Dangerous: prints a warning naming machine-in-the-middle attacks on every run, and the status bar carries `HOST KEYS UNVERIFIED` for the whole session. |
| `--list-sessions` | List the saved sessions with their host counts and exit. |
| `--sessions-dir <dir>` | Read sessions from this directory instead of `$XDG_CONFIG_HOME/lazycssh/sessions`. |

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Success: a clean TUI exit, `--version`, `--help` or `--list-sessions`. |
| `1` | A failure during the run. |
| `2` | Usage error: an unknown flag. |

## Output streams

Anything you might pipe goes to stdout; diagnostics, usage and errors go to
stderr. A failing invocation writes nothing to stdout.

## Version string

`--version` prints the version, enriched with VCS information when the binary
was built from a git checkout:

```
0.9.37
0.9.37 (3933d0b)
0.9.37 (3933d0b-dirty)
```

The revision comes from the build information the Go toolchain embeds, so it is
present in `go build` output and absent in a build from a source tarball.

## Files

| Path | Used for |
|---|---|
| `~/.ssh/config` | host resolution — `HostName`, `User`, `Port`, `IdentityFile`, `ProxyJump` |
| `~/.ssh/known_hosts`, `~/.ssh/known_hosts2` | host key verification |
| `$SSH_AUTH_SOCK` | ssh-agent authentication |
| `$XDG_CONFIG_HOME/lazycssh/sessions/*.yaml` | saved groups (`~/.config/...` when `XDG_CONFIG_HOME` is unset) |
