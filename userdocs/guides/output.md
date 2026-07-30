# Reading output

## What a pane shows

A one-line header — pane number, host name, connection state, and the last
command's exit code — over the host's scrollback, following the tail.

Only failures are marked. `exit N` appears on the panes that failed; `ok` on two
hundred panes would bury the three that matter. When the width runs out, the
state label gives up its space first and the exit code last, and the host name
truncates **from the left** (`…-1a-40.example.com`) because in a fleet of
near-identical names the suffix is the distinguishing part.

Colour is never the only signal: a failed pane has a danger-coloured border
*and* says `exit N` in text, and the focused pane's border differs in thickness
as well as colour.

## Finding what failed

- the status bar counts failures: `3 hosts failed`;
- ++exclam++ jumps the pane focus to the next failing host, from anywhere,
  wrapping around — this is a search, so a failure behind the cursor must be as
  reachable as one ahead;
- a pane whose *connection* failed prints the reason into its own scrollback,
  the way a terminal running `ssh` would.

Exit codes come from a prompt hook. A shell that never ran the hook reports
nothing, and lazycssh shows nothing rather than a made-up zero.

## Scrollback

Each session keeps a bounded ring buffer — 10,000 lines by default. When it is
full the oldest line is evicted, and the eviction is **visible**: a
`~ N lines dropped ~` marker sits where the missing output was, because
truncated history that says nothing is worse than history that says it is
truncated. A chatty host can never stall the interface; it drops its own oldest
output instead.

| Key | Effect |
|---|---|
| ++shift+page-up++ / ++shift+page-down++ | scroll the focused pane half a pane |
| ++shift+home++ | the oldest retained output |
| ++shift+end++ | back to the tail, following again |
| wheel over a pane | scroll **that** pane, without stealing focus |

The offset is anchored at the bottom, so new output slides the window rather
than your position in it. A pane that is not following its tail says
`scrollback +N` on the status bar — fresh output landing behind a frozen window
must not look like a quiet host. The buffer keeps receiving at full speed while
you read.

### What escape sequences may do

The buffer stores what the host sent, verbatim; the renderer decides what it may
do. Colours (SGR) pass through, so `ls --color` looks like `ls --color`.
Everything else — cursor movement, screen clearing, OSC titles, stray control
bytes — is neutralised before the line is drawn: a pane renders scrollback text,
and one host emitting `clear` must not corrupt the layout around it. A line that
still carries a colour is closed with a reset, so an unbalanced sequence from
one host cannot bleed into a neighbouring pane.

(Full-screen apps are the deliberate exception — see
[Full-screen apps](full-screen-apps.md).)

Long lines hard-wrap at the pane width with their colours intact across the
break, and wide characters are counted by display width.

## Search

++alt+slash++ opens a search prompt that owns the keyboard while it is open.
++enter++ commits the term and scrolls the focused pane to the **newest** match —
you are almost always hunting the error that just happened.

| Key | Effect |
|---|---|
| ++alt+bracket-left++ / ++alt+bracket-right++ | older / newer match, without wrapping |
| ++alt+c++ | clear the term |

Matching lines are drawn in the match style with their own colours dropped — a
highlight fighting the remote's colours would lose. A bare ++esc++ belongs to the
remote shell.

One term is shared by every pane, because "which of my hosts printed this" is a
question about the run. The cross-pane form is the command line:

```
/find disk full
```

```
"disk full" found on 2/8 hosts: web-03, web-07
```

It searches each host's raw scrollback, so the answer does not depend on pane
widths or pages. Like `/select`, it is a meta command: nothing reaches a remote
shell. Matching is case-insensitive substring.

## Copying

The terminal's own selection cannot reach pane content — lazycssh owns the
mouse — so copying is built in, and everything goes out over **OSC 52**, which
means it reaches your local clipboard even when lazycssh itself is running over
SSH. A terminal without OSC 52 support ignores the sequence; the status line
reports what was attempted either way.

| Action | Effect |
|---|---|
| ++alt+y++ | copy the focused pane's **visible** text — scroll first to aim the window |
| ++alt+d++ | copy the focused pane's **whole retained scrollback** |
| drag over a pane body, then ++ctrl+c++ | copy the mouse selection |

Clipboard text is plain: ANSI styling is stripped and dropped-line markers are
excluded, because a paste target wants the ID or the error message, not the
colours around it.

### Mouse selection

Press and drag the left button over a pane's body: the covered text highlights
in reverse video, stream-shaped like a terminal's own selection. The pane under
the press owns the drag, so a neighbour pane or a border can never be selected.

++ctrl+c++ with a live selection copies it, clears it, and sends **nothing** —
the status line says `copied N lines from <host> … no interrupt sent`, so if you
expected an interrupt you can see why none went out. Without a selection,
++ctrl+c++ is what it always was: the interrupt keystroke for the host or the
broadcast targets.

Anything that changes the view clears the selection: a click without a drag,
++esc++, leaving the grid, a page or chunk turn, a re-tile, a zoom, the pane
closing, or scrolling. New output under a tail-following pane redraws beneath
the highlight without moving it.
