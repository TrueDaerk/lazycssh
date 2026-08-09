# Keybindings

Every binding is declared once in the program's keymap; the help line and the
++question++ overlay are generated from it, so what you see in the tool cannot
drift from what it does. This page is the same table.

A key is dispatched by **focus**. "App level" means no input has the keyboard —
not typing into a pane, not in the broadcast bar's edit mode, not in a prompt.

## Global

| Key | Action |
|---|---|
| ++question++ | help overlay |
| ++q++ | quit — only while no input has the keyboard |
| ++ctrl+q++ | quit from anywhere, including out of a text field |
| ++tab++ / ++shift+tab++ | next / previous stop: each sidebar panel, then the grid |
| ++1++ ++2++ ++3++ ++4++ | Status, Groups, Sessions, Command log |
| ++5++ | focus the broadcast bar |
| ++b++ / ++shift+b++ / ++s++ | broadcast to the working set / the selection / one pane |
| ++ctrl+alt+b++ | broadcast to **every** host in the run |
| ++colon++ | send one command |
| ++exclam++ | jump to the next host whose last command failed |
| ++shift+s++ | save the run as a session, prompt prefilled |
| ++n++ | connect a new host (pattern prompt; `~/.ssh/config` aliases complete with ++tab++) |
| ++ctrl+a++ | show only the connected hosts; broadcast follows the visible set |
| ++ctrl+r++ | re-tile the grid for the current hosts |
| ++ctrl+s++ | split the grid into chunks of N panes (empty or `0` clears) |
| ++ctrl+shift+right++ / ++ctrl+shift+left++ | next / previous screenful: pages, then split chunks, wrapping; works while typing too. Plain ++ctrl+right++ / ++ctrl+left++ stay word movement for the hosts |
| ++a++ | select every host |
| ++i++ | invert the selection |
| ++c++ | clear the selection |
| ++u++ | select the hosts that are up |
| ++d++ | select the hosts that are down |

## Sidebar

| Key | Action |
|---|---|
| ++up++ / ++k++, ++down++ / ++j++ | move the cursor inside the focused panel; a no-op at the top/bottom of the list, and on the Status panel, which has none |
| ++left++ / ++right++ | previous / next panel, stopping at Status / Command log |
| ++enter++ / ++space++ | choose the row: open a group, foreground a session, resend a log entry |
| ++w++ | save the run as a group |
| ++bracket-left++ / ++bracket-right++ | previous / next chunk of hosts |
| ++n++ | *(Groups panel)* new group |
| ++d++ | *(Groups panel)* delete the group under the cursor, after `y/n` |
| ++x++ | *(Sessions panel)* end the session under the cursor, after `y/n` |

The Groups panel's ++n++ and ++d++ deliberately shadow their global meanings
while it has focus — lazygit-style panel keys.

++up++/++down++ never change which panel is focused — that is always explicit,
via ++left++/++right++, ++tab++/++shift+tab++, or the number keys.

## A focused pane

A focused pane **is a terminal**: every plain key is forwarded to the host —
letters, ++enter++, ++tab++, ++esc++, ++ctrl+c++, the arrows, all of it. What
lazycssh keeps for itself is the reserved escape and the chord set, which work
from the app level too.

| Key | Action |
|---|---|
| ++ctrl+bracket-right++ | stop typing: back to the app level, on the Status panel |
| ++shift+alt+left++ ++shift+alt+right++ ++shift+alt+up++ ++shift+alt+down++ | move between panes |
| ++alt+left++ / ++alt+right++ | word backward / forward on the remote line (sent as ++escape++ `b` / ++escape++ `f`) |
| ++alt+backspace++ / ++alt+delete++ | kill the previous / next word |
| ++cmd+left++ / ++cmd+right++ | line start / end on the remote line (sent as ++ctrl+a++ / ++ctrl+e++) |
| ++cmd+backspace++ | kill to line start (sent as ++ctrl+u++) |
| ++alt+space++ | toggle this pane's host in the selection |
| ++alt+z++ | full-screen this pane |
| ++alt+r++ | reconnect this host |
| ++alt+x++ | close this host; on a dead host, remove its pane |
| ++alt+y++ | copy this pane's visible text (OSC 52) |
| ++alt+d++ | copy this pane's whole scrollback (OSC 52) |
| ++ctrl+c++ | with a live mouse selection: copy it and clear it, sending nothing. Without one: the interrupt keystroke |
| ++shift+page-up++ / ++shift+page-down++ | scroll this pane back / forward |
| ++shift+home++ / ++shift+end++ | oldest retained output / back to the tail |
| ++alt+slash++ | search the scrollback |
| ++alt+bracket-left++ / ++alt+bracket-right++ | older / newer match |
| ++alt+c++ | clear the search |

## The broadcast bar

In **edit mode**, every keystroke goes to the target set. The bar keeps
++ctrl+bracket-right++, the pane chords, and the ++ctrl+a++ escape prefix:

| Key | Action |
|---|---|
| ++ctrl+a++ ++escape++ | switch to view mode |
| ++ctrl+a++ ++a++ | send one literal ++ctrl+a++ to the targets |
| ++ctrl+a++ *other* | run that key as a one-shot lazycssh command (++ctrl+a++ ++ctrl+a++ toggles connected-only, ++ctrl+a++ ++question++ opens the help) |
| ++enter++ | send a carriage return and record the assembled line |

In **view mode** every key is an app-level command instead, and nothing is sent.
++enter++ returns to edit mode.

## Command line and prompts

While a prompt is open it owns the keyboard: a command containing `b` does not
switch the broadcast mode.

| Key | Action |
|---|---|
| ++enter++ | send / confirm |
| ++esc++ | abandon |
| ++up++ / ++down++ | walk this run's command history |
| ++ctrl+q++ | quit |

Meta commands, which never reach a host:

| Command | Effect |
|---|---|
| `/select all\|set\|up\|down\|invert\|none\|<glob>` | build the selection |
| `/deselect <glob>` | the reverse |
| `/find <text>` | search every host's scrollback and report which ones match |

## Mouse

| Action | Where | Effect |
|---|---|---|
| left click | pane body | focus the pane and start typing into its host |
| left click | `[x]` in a pane header | close a live host; remove a dead one |
| left click | sidebar box / row | select the panel, move its cursor to the row |
| left click | broadcast bar | give the bar the keyboard |
| drag | pane body | select text; ++ctrl+c++ copies it |
| wheel | over a pane | scroll that pane's scrollback, without changing focus |
| wheel | over a sidebar list | move that panel's cursor |
