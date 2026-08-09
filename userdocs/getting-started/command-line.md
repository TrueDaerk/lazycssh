# Command line

```
lazycssh [flags] [<host|@session>...]
```

Full details, including exit codes, are in the
[command line reference](../reference/cli.md). This page is the practical tour.

## Hosts

An argument is `[user@]host[:port]`, resolved through `~/.ssh/config`:

```sh
lazycssh web-01
lazycssh root@srv1.example.com:2222
lazycssh '[2001:db8::1]:2222'          # IPv6 needs brackets when a port follows
```

`HostName`, `User`, `Port`, `IdentityFile` and `ProxyJump` from a matching
`Host` block apply, with anything stated on the command line winning. A pane is
labelled with the alias you typed, even when the connection goes to the
`HostName` behind it.

A missing `~/.ssh/config` is fine. An *unreadable* one is an error, because
silently ignoring a config you believe is in effect would connect as the wrong
user.

## Brace expansion

A rack of machines is one argument:

```sh
lazycssh 'srv1-{01..40}.example.com'
lazycssh '{web,db}-{1..3}.example.com'
lazycssh 'srv-{a..c}'
lazycssh 'srv{0..20..5}'
```

Expansion is done by lazycssh, never by a shell — so quoting changes nothing,
and no argument is ever handed to a shell as code. Ordering matches bash: the
leftmost brace varies slowest.

One deliberate difference from bash: a brace group that parses as neither
alternatives nor a range is an **error**, not a literal.

```
invalid host pattern "srv{abc}" at position 3: "{abc}" is neither alternatives ({a,b}) nor a
range ({1..9}); use \{ for a literal brace
```

In bash, `srv{abc}` is the string `srv{abc}` and you find out about the typo
from the machine that never got the command. One bad argument fails the whole
invocation — connecting to *some* of the intended hosts is worse than
connecting to none.

The full syntax table is in [host patterns](../guides/connecting.md#host-patterns).

## Saved groups

An argument starting with `@` names a saved group (a
[session file](../reference/session-files.md)):

```sh
lazycssh @prod-web                      # the saved group
lazycssh @prod-web extra.example.com    # the group plus one more host
lazycssh @prod-web @canaries            # two groups merged
lazycssh --list-sessions                # what is saved, with host counts
```

Order is preserved, because the order hosts appear in is the order their panes
are tiled in. When several groups are named, the last one that sets a broadcast
mode or a working set wins — the last thing you named is the thing you meant.
A name that is not saved is an error that lists the ones that are.

## Flags

| Flag | Effect |
|---|---|
| `--version` | Print the version and exit. |
| `-h`, `--help` | Print usage and exit. |
| `--insecure-ignore-host-key` | Accept any host key without checking `known_hosts`. Prints a warning on every run and stays on the status bar. |
| `--list-sessions` | List the saved sessions with their host counts and exit. |
| `--sessions-dir <dir>` | Read sessions from this directory instead of the default. |

## No arguments

```sh
lazycssh
```

opens the TUI on an empty run with nothing focused for input. ++shift+a++ opens
the host picker, ++n++ the host prompt, the Groups panel (++2++) launches a
saved group. Which of those comes first is your call, not the program's.
