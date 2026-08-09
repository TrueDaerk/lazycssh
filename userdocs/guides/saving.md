# Saving groups and sessions

Typing forty hostnames on every run defeats the purpose of the tool. Save the
run once and reopen it with `lazycssh @name`.

## Save the current run

Two ways, same result:

- ++shift+s++ at the app level — a save prompt, prefilled with the run's session
  name, from anywhere;
- ++w++ in the sidebar — save the run as a group.

An existing name is **never** replaced silently: the first ++enter++ turns into
an `overwrite "x"?` confirm — ++enter++ or ++y++ overwrites, ++esc++ backs out. Saving an empty run reports `nothing to save` and keeps
your typed name.

While you are typing into a host or the broadcast bar, ++shift+s++ is a letter
like any other.

### What gets saved

The host **patterns** as you typed them — not the names they expanded to. A file
written when the fleet had 40 machines still means "all of them" after it grows,
and forty resolved hostnames would be unreadable and wrong the moment the fleet
changes.

The saved patterns track the run: the command line arguments first, then
everything connected or launched at runtime, deduplicated in order. Removing a
host drops a pattern that names it exactly; a glob or brace pattern stays,
because one host cannot narrow it.

Also saved: the connection options, the broadcast mode and the working set —
except when they are already the defaults, which are left out of the file
entirely.

Deliberately not saved: a **manual** working set (it is a list of identifiers
from this run, and restoring it against a different fleet would mean nothing),
and anything that only restates a default.

Never saved: any credential. See [Session files](../reference/session-files.md).

## Manage groups in the panel

The Groups panel (++2++) lists the saved groups with their host counts and
descriptions.

| Key | Effect |
|---|---|
| ++enter++ / ++space++ | open the group as a session |
| ++n++ | create a group — a two-question dialog: name, then host patterns |
| ++d++ | delete the group under the cursor, after a confirm |
| ++w++ | save the current run as a group |

While this panel has focus, ++n++ and ++d++ shadow their global meanings
(connect a host, select the down hosts) — lazygit-style panel keys.

The creation dialog owns the keyboard; a taken name or a malformed pattern keeps
it open with what you typed. Deleting a group's file does not touch an open
session of that group.

One unreadable file becomes one `(unreadable)` row rather than an empty panel —
hiding the other groups would make a typo look like data loss.

## Open sessions

Opening a group resolves its patterns through `~/.ssh/config` and connects them;
a group whose session is already open is brought to the foreground instead of
dialled twice. The open group's row is marked with `▸`.

The Sessions panel (++3++) lists the **open** sessions with their up counts:

| Key | Effect |
|---|---|
| ++enter++ / ++space++ | bring this session to the foreground |
| ++x++ | end this session, after a confirm |

Backgrounding a session keeps every connection — its panes leave the grid, its
output keeps arriving. Ending one sends ++ctrl+c++ then ++ctrl+d++ to each of its
connected terminals, and leaves nothing in the command log.

The full model is in
[Groups, sessions and working sets](../concepts/groups-and-sessions.md).

## Reopen

```sh
lazycssh @prod-web                      # the group
lazycssh @prod-web extra.example.com    # plus one more host
lazycssh @prod-web @canaries            # two groups merged
lazycssh --list-sessions                # what is saved
```
