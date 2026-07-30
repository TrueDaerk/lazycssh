# Concepts

Four ideas carry most of lazycssh. Two of them are easy to confuse with each
other, and confusing them is how a command reaches the wrong machines — so they
are named, separated and documented on purpose.

<div class="grid cards" markdown>

- :material-view-grid: **[The grid and the window](grid-and-window.md)**

    How panes are tiled, why the grid pages instead of shrinking, and why
    "what is on screen" is deliberately not "who receives a keystroke".

- :material-broadcast: **[Broadcast scope](broadcast.md)**

    The four modes, what each one targets, and how the status bar count is kept
    honest.

- :material-folder-network: **[Groups, sessions and working sets](groups-and-sessions.md)**

    A group is a file. An open session is a group at runtime. A working set is
    which hosts a command is *about*.

- :material-shield-key: **[Security model](security.md)**

    Host keys, credentials, the command log, and the things lazycssh refuses to
    do.

</div>

## The two-word summary

| Concept | The question it answers |
|---|---|
| **Window** | How many panes fit on the screen right now? |
| **Working set** | Which hosts is this command about? |
| **Scope** | Who am I addressing? |
| **Targets** | Who can actually receive right now? |

Paging the window never changes who receives a keystroke. Scope minus the hosts
that cannot take input equals targets, and the first number on the status bar
is always the target count — the label and the reality cannot drift.
