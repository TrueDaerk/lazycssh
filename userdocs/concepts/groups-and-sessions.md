# Groups, sessions and working sets

Three words that sound alike and mean different things.

| Word | What it is | Lifetime |
|---|---|---|
| **Group** | a persisted host list — a file on disk | survives restarts |
| **Open session** | a group at runtime: a named set of hosts on screen together | this run |
| **Working set** | which hosts a command is *about* | until you change it |

## Groups

A group is a [session file](../reference/session-files.md) in
`$XDG_CONFIG_HOME/lazycssh/sessions/<name>.yaml`. It stores host **patterns**
as you typed them — `srv1-{01..40}.example.com`, not the forty names it expands
to — so a group written when the fleet had 40 machines still means "all of
them" after it grows.

The Groups panel (++2++) is a group's whole lifecycle: ++n++ creates one,
++d++ deletes it after a confirm, ++w++ saves the current run as one, ++enter++
opens it. Deleting a group's file does not touch an open session of that group —
definitions and live sessions have separate lifetimes.

## Open sessions

Opening a group resolves its patterns through `~/.ssh/config` and connects them.
Hosts already in the run are reused, never dialled twice.

Several sessions can be open at once; exactly one is in the **foreground**, and
the foreground is what the interface is about:

- the grid draws only its panes,
- the broadcast scope is limited to it — `all` and `selected` stop at the
  session's edge (`fleet` stays the explicit every-host escape hatch),
- the status bar carries its name, and `N sessions` when more than one is open.

Opening group B while A is on screen **backgrounds** A: its panes leave the
grid, its connections stay, and its output keeps landing in its scrollback.
Opening A again restores its panes without reconnecting anything. The Sessions
panel (++3++) lists the open sessions with their up counts and brings one to the
foreground with ++enter++.

A host may belong to several sessions: two groups naming the same machine share
one connection and one pane identity. A host removed from the run leaves every
session.

### Ending a session

++x++ on a row in the Sessions panel asks `end "name"?`. On ++enter++ or ++y++, every
connected terminal of that session receives ++ctrl+c++ then ++ctrl+d++ —
interrupt the foreground process, log the shell out — targeting exactly the
session's hosts whatever the broadcast mode is, and leaving nothing in the
command log.

A shell that logs out cleanly closes its own pane. A session ends by itself once
every host is done and at least one ended in a clean logout — but a session
whose hosts merely all *failed*, with no logout and no request to end, stays
listed: that is an outage to look at and reconnect, not a completed shutdown.

When the run loses its last host, the TUI stays open and falls back to the
neutral argumentless start: no pane focus, no kept grid shape, no filters. It is
the hub the next group is opened from; quitting stays what ++q++ is for.

## Working sets

With forty hosts you rarely work on forty hosts. The working set is which hosts
are the current subject of work — and `BROADCAST all` means exactly that set.

A working set stores a **definition**, never a captured list. Members are
recomputed from the live host list on every use, so `web-*` still means that
after a host is added.

| Selector | What you type | Meaning |
|---|---|---|
| All | `all`, `*` | every host in the run |
| Range | `20`, `first 20`, `21-40` | positions in the host list, 1-based, inclusive |
| Pattern | `web-*`, `db-01` | glob against the host identifier |
| Manual | `selection` | the hosts currently toggled with ++alt+space++ |

A malformed glob or a reversed range is an error, not a pattern that silently
matches nothing. Bounds are clamped at use, not at definition: `21-40` against a
fleet that shrank to 25 hosts yields five members rather than an error.

**Paging.** ++bracket-left++ / ++bracket-right++ in the sidebar shift a
positional set by exactly its own size — "show me the first 20", then "now the
next 20". The chunk size is the size of the *definition*, so a short last page
still pages back to a full first page. Paging only applies to a range; a pattern
has no next chunk. Neither direction wraps: wrapping would silently send the
next command to the hosts at the other end of the fleet.

**Named sets.** A definition can be saved under a name and returned to later.
Paging produces an ad hoc set — the stored definition is left untouched, so
`front-half` still means the first twenty hosts after you have paged three
chunks away.

The status bar never renders the size without the fleet total:

```
front-half (20/40 hosts)
21-40 (20/40 hosts)
all (40/40 hosts)
```

`20` on its own reads as "all twenty hosts" to someone whose run has forty, and
that misreading is exactly the one that sends a command to the wrong machines.
