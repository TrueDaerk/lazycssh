# Connecting to hosts

## From the command line

```sh
lazycssh web-01 db-01
lazycssh 'srv1-{01..40}.example.com'
lazycssh @prod-web
```

See [Command line](../getting-started/command-line.md).

## Mid-run: picking a host

++shift+a++ opens the **host picker**: a box listing everything you could
connect to, whatever has focus. Every row says where it came from:

| Tag | Row |
|---|---|
| `cfg` | a concrete alias from your `~/.ssh/config` |
| `grp` | a [saved group](saving.md), shown as `@name` — connecting it connects all of its hosts |
| `rec` | a host you connected to in an earlier run |

Type to filter. The match is fuzzy and case-insensitive — `wb1` finds `web-01` —
and it runs across all three kinds of row at once, so a long config is narrowed
in two or three keystrokes. ++up++ and ++down++ move through what is left.

| Key | Action |
|---|---|
| ++enter++ | connect the highlighted row and close the picker |
| ++space++ / ++tab++ | mark a row and step down; ++enter++ then connects everything marked |
| ++esc++ | close, having done nothing |

Marks mix: mark a group and two hosts, press ++enter++, and you get all of them.

If what you typed matches no row, ++enter++ connects it as a host pattern
instead — brace expansion and `user@host:port` included — so a machine that is
not in your config is still one ++enter++ away. It shows up as a `rec` row next
time.

The recent list lives in `~/.config/lazycssh/recent` (or
`$XDG_CONFIG_HOME/lazycssh/recent`), most recent first, capped at 200 hosts. It
records hosts that actually connected, and a host that is already an alias in
your `~/.ssh/config` stays a single `cfg` row rather than appearing twice.

## Mid-run: typing a pattern

++n++ works from anywhere: it selects the Status panel and opens a free-text
prompt that accepts any host pattern — `host`, `user@host:port`, brace
expansion like `web-{01..04}`.

While the prompt is open, the concrete aliases from `~/.ssh/config` that are not
already in the run are listed beneath it, filtered by what you have typed.
++tab++ completes the first match, ++enter++ connects, ++esc++ abandons.

The prompt owns the keyboard while it is open: a pattern containing `b` does not
switch the broadcast mode. ++ctrl+q++ still quits.

A host that is already in the run is skipped rather than dialled twice — pressing
++enter++ twice cannot mint a second `web-01`. A pattern that fails to resolve
shows its error in the Status panel.

New hosts join the foreground session — they go where you are looking — and
their panes are appended after the existing slots.

## Host patterns

Brace expansion is performed by lazycssh, so quoting an argument changes
nothing and nothing is ever handed to a shell:

| Form | Example | Result |
|---|---|---|
| Alternatives | `srv-{a,b,c}` | `srv-a srv-b srv-c` |
| Empty branch | `srv{,-backup}` | `srv srv-backup` |
| Numeric range | `srv{1..4}` | `srv1 … srv4` |
| Descending | `srv{3..1}` | `srv3 srv2 srv1` |
| Zero-padded | `srv{08..11}` | `srv08 … srv11` |
| Step | `srv{0..20..5}` | `srv0 srv5 srv10 srv15 srv20` |
| Letters | `srv-{a..e}` | `srv-a … srv-e` |
| Nesting | `srv-{a,{b,c}}` | `srv-a srv-b srv-c` |
| Product | `{web,db}-{1..3}` | `web-1 web-2 web-3 db-1 db-2 db-3` |
| Literal brace | `srv\{1\}` | `srv{1}` |

Ordering matches bash — the leftmost brace varies slowest — and duplicates are
kept, also as in bash. Zero padding switches on when either endpoint carries a
leading zero, and the width is the wider of the two. The sign of a step is
ignored; direction comes from the endpoints.

A brace group that is neither alternatives nor a range is an **error**, unlike
bash, which would leave it as a literal and connect you to a nonsense hostname.
Unbalanced braces, mismatched endpoint types (`{a..5}`), a zero step and a
trailing backslash are all rejected the same way.

A single argument may expand to at most 100,000 entries, and so may the
arguments together — brace expansion is multiplicative, and
`{1..1000}-{1..1000}` should fail rather than build a million strings.

## What `~/.ssh/config` contributes

lazycssh reads the same file ssh does. Precedence is ssh's:

1. what the command line states,
2. what the matching `Host` block states,
3. the built-in default (port 22, your OS user).

Directives read: `HostName`, `User`, `Port`, `IdentityFile` (repeatable, order
preserved) and `ProxyJump` (`none` means no jump host). `~` in identity paths is
expanded, and glob patterns in `Host` lines match as in ssh — `Host web-*`
applies to `web-1`.

A pane is labelled with the **alias** you typed, because that is what you
recognise; the connection goes to the `HostName` behind it.

## Reconnect, close, remove

| Key | Effect |
|---|---|
| ++alt+r++ | reconnect the focused host |
| ++alt+x++ | close the focused host — on a dead host, remove its pane from the run |
| click `[x]` in a pane header | the same as ++alt+x++ |

These work while you are typing into a pane as well as at the app level, because
they are ++alt++ chords: combinations the keystroke encoder never produced, so
intercepting them takes nothing away from the remote shell.

Closing a host leaves a hole in the grid — the survivors keep their positions —
until you re-tile with ++ctrl+r++. See
[The grid and the window](../concepts/grid-and-window.md#departures-leave-a-hole).

## When a connection fails

A pane whose connection failed prints the reason into its own scrollback the way
`ssh` would — DNS, connection refused, authentication, host key. It scrolls with
the history and is reachable like any other output. The Status panel and the
failure counts pick it up too, so a fleet with three broken hosts says so on the
status bar.

Nothing about a failed host affects the others: one dead host is one dead pane.
