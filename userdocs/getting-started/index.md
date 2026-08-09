# Getting started

Three pages, in order:

1. **[Installation](installation.md)** — build the binary and check what you
   built.
2. **[Your first run](first-run.md)** — connect a few hosts, type into one,
   broadcast to all of them, and get your keyboard back.
3. **[Command line](command-line.md)** — host patterns, saved groups, flags.

If you already have lazycssh on your `PATH`, the shortest possible start is:

```sh
lazycssh
```

An argumentless run opens on an empty grid with nothing focused for input. It
does not pick a first action for you: press ++shift+a++ to pick hosts from
`~/.ssh/config`, ++n++ to type one, or open a saved group from the Groups panel
(++2++). The status bar echoes the same nudge — `0 hosts — press A to add` —
until the first host connects.
