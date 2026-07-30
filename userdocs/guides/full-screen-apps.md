# Full-screen apps

Every session runs its own terminal emulator, so `vim`, `htop`, `less` and
anything else that takes over the screen work in a pane.

## In one pane

Focus a pane and run the app. When it switches to the alternate screen, the pane
stops rendering scrollback text and renders the **live screen grid** instead:
the emulator's screen clipped to the pane body, with the remote app's cursor
drawn where the app says it is — and hidden when the app hides it, the way vim
does while repainting.

While the grid is active, the pane is entirely the remote app's:

- no tail-following, no scroll offset — scrolling is a no-op, so the offset
  cannot jump when the app exits,
- no scrollback search, no text selection.

Leaving the alternate screen returns to the scrollback view. The tail shows the
post-app screen — cleared, like a terminal after vim quits — and the history
from before the app stays reachable by scrolling.

A pane needs at least a 45×16 terminal for its host, and the remote PTY is sized
to exactly the pane's content area, so what the app draws matches what the pane
shows. Re-tile with ++ctrl+r++ after the grid shape changes to resize the remote
PTYs.

## On the whole fleet

This is the case that needs a rule, and lazycssh has one:

- **Mixed scope** — if some hosts in scope are on the alternate screen and some
  are not, `all` and `selected` **skip** the alternate-screen hosts. A keystroke
  meant for one `vim` must not reach twenty of them. The status bar names the
  skip: `BROADCAST all (6/8 up, 1 alt-screen skipped)`.
- **Uniform scope** — if *every* reachable host in scope is on the alternate
  screen, nothing is skipped and the keystrokes flow to all of them.

The second case is what a broadcast that opened those apps looks like. Type
`vim /etc/hosts` in the broadcast bar, every host enters the editor, and from
that point the broadcast line drives all of them together — `:wq` included.

Two ways past the skip when the scope is mixed:

| Want | Do |
|---|---|
| talk to the one editor | ++s++ — `single` mode, the focused pane, never excluded |
| talk to everything regardless | ++ctrl+alt+b++ — `fleet` mode, the explicit escape hatch |

## Terminal queries

Full-screen apps ask the terminal questions on startup — device attributes,
cursor position reports. Each session answers its own: a reply goes back to the
host that asked and never travels through the broadcast, so twenty `vim`s
starting at once cannot cross-talk.

## Limits

- Legacy alternate-screen mode `?47` is not implemented; `?1049` and `?1047`,
  which every terminfo in current use emits, are.
- An emulator costs a cell grid the size of the pane plus its own bounded
  scrollback. Twenty panes cost a few megabytes.
