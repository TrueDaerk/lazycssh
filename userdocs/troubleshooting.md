# Troubleshooting

## "My keystrokes are not going where I expect"

Read the status bar — it always states where they go:

| It says | Your keys go to |
|---|---|
| `TYPING web-01 — ctrl+] leaves · alt=app` | that one host, and only that host |
| `BROADCASTING EDIT → 7 hosts` | all seven targets, live |
| `BROADCAST VIEW — keys are commands` | nowhere — the broadcast bar is in view mode |
| `BROADCAST all (7/8 up)` and no typing line | lazycssh itself; ++colon++ or ++5++ to send something |
| `AUTH web-01` | an authentication prompt is waiting |

++ctrl+bracket-right++ always returns the keyboard to lazycssh. It is the
reserved escape for exactly this situation.

## "A key does nothing / does the wrong thing"

Keys are dispatched by focus, and a focused pane is a terminal. While you are
typing into a host, ++q++ is a letter, ++ctrl+a++ is start-of-line, ++ctrl+r++
is reverse-search and ++ctrl+s++ is flow control — all of them belong to the
remote shell. The lazycssh equivalents are the ++alt++ chords, which work while
typing, or leave the pane first with ++ctrl+bracket-right++.

Inside the broadcast bar, ++ctrl+a++ is the escape prefix: ++ctrl+a++
++ctrl+a++ runs the global connected-only toggle, ++ctrl+a++ ++question++ opens
the help, ++ctrl+a++ ++a++ sends a literal ++ctrl+a++ to the hosts.

++ctrl+q++ quits from anywhere, including out of a text field.

## "The command went to fewer hosts than I expected"

The first number on the status bar is always the number of hosts that will
actually receive the next keystroke. Reasons it is smaller than your run:

- **a narrowed working set** — the label says so: `BROADCAST set:front-half
  (19/20 up)`. `all` means the working set, by design;
- **hosts that are down** — a host that cannot take input is excluded from the
  count and from delivery;
- **the connected-only filter** (`CONNECTED HOSTS ONLY`) or an active **split**
  (`SPLIT 1/2 (5 hosts)`) — both narrow the broadcast to what is on screen;
- **a background session** — `all` and `selected` stop at the foreground
  session's edge;
- **full-screen apps** — `1 alt-screen skipped` in the label; see
  [Full-screen apps](guides/full-screen-apps.md).

++ctrl+alt+b++ (`fleet`) ignores all of these except liveness. It is the
explicit every-host escape hatch and always renders as a warning.

## "It says the command went to 0 hosts"

`sent to 0/N hosts … — no host can take input right now` means exactly that:
empty scope, or every target down. Typing into the void is reported rather than
looking like success.

## "A pane says the connection failed"

The reason is printed in the pane itself, the way `ssh` prints it. The common
ones:

- **host key changed** — a hard failure with no prompt, by design. The message
  names the `known_hosts` file and line. Remove that line only if you know the
  host was rebuilt.
- **authentication failed** — check the method order in
  [Security model](concepts/security.md#credentials-stay-in-memory): agent
  first, then identity files, then a password prompt in the pane.
- **no prompter available** — a non-interactive context cannot ask, so a method
  needing a secret fails instead of hanging.

++alt+r++ reconnects one host, ++alt+x++ removes a dead pane.

## "Panes stopped appearing when I added hosts"

The grid never shrinks a pane below what its host needs (45×16). Past that
point it **pages** instead, and the overflow footer says so:

```
+12 more hosts — ctrl+→ · page 1/3 · 2 more sessions — [3]
```

++ctrl+right++ / ++ctrl+left++ move a screenful at a time. Paging changes what
you see, never who receives a keystroke.

## "A pane is in the wrong place / there is an empty frame"

A host leaving the run leaves a hole where its pane was, on purpose: the
survivors keep their positions and their numbers. ++ctrl+r++ closes the holes
and re-tiles (and resizes the remote PTYs).

## "Output is missing from the top of a pane"

`~ older output dropped ~` marks where the bounded history (10,000 lines per
session) evicted output. ++shift+home++ jumps to the oldest retained line.

If a pane looks frozen while others move, check for `scrollback +N` on the
status bar — that pane is not following its tail. ++shift+end++ returns to it.

## "Copy does nothing"

Copying goes out over OSC 52. A terminal without OSC 52 support silently ignores
the sequence — the status line reports what lazycssh attempted, not what your
terminal did with it. Some terminals require the feature to be enabled
explicitly for security reasons.

## "The interface says the terminal is too small"

Below 24×4 the whole interface degrades to a single line. Below the width that
fits a sidebar plus a usable grid, the sidebar disappears first — output
squeezed to nothing is worse than no panel list.

## Reporting a bug

Include the version (`lazycssh --version`), your terminal and OS, **the host
count**, and **the broadcast mode and working set** you were in. See
[About & contributing](contributing.md).
