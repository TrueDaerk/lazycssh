# Selecting hosts

Two different narrowings, often confused:

- a **selection** is a set of toggled hosts, addressed by `BROADCAST selected`;
- a **working set** is the current subject of work, and it is what
  `BROADCAST all` means.

## Building a selection

The single-letter keys work at the app level; ++alt+space++ works from the grid
too, like the other pane chords.

| Key | Command line | Effect |
|---|---|---|
| ++alt+space++ | — | toggle the focused pane's host |
| ++a++ | `/select all` | every host in the run |
| — | `/select set` | the active working set |
| ++u++ | `/select up` | the hosts that can take input |
| ++d++ | `/select down` | the hosts that cannot — "show me what broke" |
| ++i++ | `/select invert` | flip the selection |
| ++c++ | `/select none` | clear it |
| — | `/select web-*` | every host matching a glob |
| — | `/deselect web-1*` | the same, in reverse |

A pattern matches across the **whole run**, not only the working set: a
selection is a statement about machines, and `web-*` must not mean different
things at different times.

Selection is keyed by host identifier, so it survives a reconnect and a page
turn — the pane moves, the host keeps its name.

Press ++shift+b++ to address the selection. The status bar count follows it as
it changes, so `BROADCAST selected (3/3 up)` is always the number of machines
your next keystroke reaches.

!!! note "Selected outside the working set"
    `selected` intersects with the working set rather than replacing it. Hosts
    you selected that fall outside the current set stay visibly selected but do
    **not** receive input, and are reported as excluded — the mismatch belongs on
    screen, not on the wire.

### Why `/select` starts with a slash

`select` is a shell builtin. A line that means "run this on forty machines" must
never be intercepted because it happens to start with a word lazycssh knows, so
meta commands carry a `/` prefix. A `/`-prefixed line is never sent to a host and
never recorded as a command.

## Narrowing the working set

The working set is a definition — a range, a pattern, everything, or the current
selection — and members are recomputed from the live host list every time it is
used. `first 20`, `21-40`, `web-*` and `selection` are all valid; a malformed
glob or a reversed range is an error rather than a pattern that silently matches
nothing.

++bracket-left++ / ++bracket-right++ in the sidebar page a positional set by
exactly its own size: "the first 20", then "the next 20". Paging does not touch
a named definition, so `front-half` still means the first twenty hosts after you
have paged three chunks away. Neither direction wraps.

The status bar names a narrowed set in the broadcast label, so `all` never
appears while fewer than every host is addressed:

```
BROADCAST set:front-half (19/20 up)
```

The full model — selectors, clamping, named sets — is in
[Groups, sessions and working sets](../concepts/groups-and-sessions.md#working-sets).

## Selecting versus seeing

Narrowing what is **on screen** is a third thing again:

- ++ctrl+a++ shows only the connected hosts, and narrows the broadcast with it;
- ++ctrl+s++ splits the visible hosts into chunks of N, and narrows the
  broadcast with it;
- ++ctrl+right++ / ++ctrl+left++ page the window, and change **nothing** about
  who receives a keystroke.

See [The grid and the window](../concepts/grid-and-window.md#narrowing-what-is-on-screen).
