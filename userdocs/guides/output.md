# Reading output

## What a pane shows

A one-line header — pane number, host name, connection state, and how the last
command ended — over the host's scrollback, following the tail.

When the width runs out, the state label gives up its space first and the exit
status last, and the host name truncates **from the left**
(`…-1a-40.example.com`) because in a fleet of near-identical names the suffix is
the distinguishing part.

Colour is never the only signal: a failed pane has a danger-coloured border
*and* says `exit N` in text, and the focused pane's border differs in thickness
as well as colour.

## Who failed the last command

The exit indicator in the header is **per command**. It answers one question:
how did the last command you sent from the command line (++colon++) end *on this
host*?

| Header | Meaning |
|---|---|
| nothing | no command of yours is outstanding here, or this shell reports no exit codes |
| `·` | the command is out, this host has not come back to a prompt yet |
| `✓` | it finished with exit code 0 |
| `exit N` | it failed, with the code |

Send a new command and every reached pane drops back to `·`, so the grid reads
as a live scoreboard for the question you just asked instead of an archive of
older ones. A failure spells out its code; a success is one character, because
`ok` on two hundred panes buries the three that matter.

Around it:

- the status bar counts the last command's failures: `3 hosts failed`;
- ++exclam++ jumps the pane focus to the next failing host, from anywhere,
  wrapping around — this is a search, so a failure behind the cursor must be as
  reachable as one ahead;
- a pane whose *connection* failed prints the reason into its own scrollback,
  the way a terminal running `ssh` would.

### When you get no indicator

Exit codes come from a prompt hook lazycssh arms once, at login: bash's
`PROMPT_COMMAND` and zsh's `precmd` print an invisible marker carrying `$?`
before every prompt. Nothing you type is rewritten, and nothing is added to your
shell history.

That means there are cases where lazycssh honestly cannot say, and it shows
nothing rather than a green tick it cannot back:

- **the shell does not run the hook** — a plain POSIX `sh`, a restricted shell,
  or a profile that overwrites those variables. That host stays blank;
- **you typed the command into a pane or the broadcast bar** instead of the
  command line. Raw keystrokes are bytes, not commands: lazycssh cannot tell
  where one ends, and a bare ++enter++ at a prompt reports the *previous*
  command's code. Keystrokes therefore change no header;
- **the command did not reach that host** — it was out of the broadcast scope,
  or the send failed for it (the status bar says how many missed it);
- **the host reconnected** — the new shell is a new session, and the old
  question died with the old one.

One caveat worth knowing: a pane sitting on `·` means "no prompt yet". A
full-screen app (`vim`, `less`) or a command that never returns keeps it there,
which is accurate — the command really has not finished.

## Which machines disagree

After sending a command with ++colon++, press ++6++: the Output diff panel
groups the hosts by the output they produced since that send, largest group
first — the consensus leads, the outliers follow in the warning colour. The
preview shows the answer under the cursor whole, with the hosts that gave it.

The comparison ignores everything printed before the send, trims trailing
whitespace, and treats a host's own name — the prompt, a `hostname` echo — as
equal across machines. Timestamps are not smoothed over: a command that prints
the clock will not converge.

++enter++ on a variant makes its hosts the selection; ++shift+b++ then
addresses it, so the fix goes to exactly the machines that disagree.

## Scrollback

Each session keeps a bounded ring buffer — 10,000 lines by default. When it is
full the oldest line is evicted, and the eviction is **visible**: a
`~ older output dropped ~` marker sits where the missing output was, because
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

Each pane is a real terminal emulator: colours, cursor movement, prompt
redraws and erase sequences render the way the host meant them, so `ls
--color` looks like `ls --color` and a rewritten line leaves no artifacts.
`clear` empties the pane — and the cleared output stays reachable by
scrolling up, because the emulator pushes it into the history instead of
discarding it; even the scrollback-erase some terminals send with `clear`
is filtered out. Every line is clipped to the pane, so one misbehaving host
cannot corrupt the layout around it.

(Full-screen apps own the whole pane — see
[Full-screen apps](full-screen-apps.md).)

Long lines wrap at the pane width with their colours intact across the
break, and wide characters are counted by display width. Resizing the pane
re-wraps the whole history at the new width, as if the terminal had always
been that size.

## Search

++slash++ opens a search prompt that owns the keyboard while it is open — from
the app level, where plain letters are commands. A focused pane is a terminal, so
there the slash goes to the host and ++alt+slash++ opens the same prompt.
++enter++ commits the term and scrolls the focused pane to the **newest** match —
you are almost always hunting the error that just happened.

| Key | Effect |
|---|---|
| ++n++ / ++shift+n++ | older / newer match, without wrapping (while a search is live) |
| ++alt+bracket-left++ / ++alt+bracket-right++ | the same, and they work while typing to a host |
| ++esc++ / ++alt+c++ | leave the search: highlight off, panes back where they were |

Matching lines are drawn in the match style with their own colours dropped — a
highlight fighting the remote's colours would lose — and the line you are
standing on is drawn louder still. The status bar counts it:

```
search "error" 3/17
```

A term that matches nothing says `search "error" no match` and moves nothing.
Leaving the search puts every pane it scrolled back where it was. While typing
to a host, a bare ++esc++ belongs to the remote shell; ++alt+c++ leaves the
search from there.

While a search is live, ++n++ means "older match" rather than "connect a new
host". ++esc++ ends the search and gives the letter back.

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

## Exporting to a file

The clipboard only reaches your local machine, and only until the next copy.
For a postmortem you want to keep — the one host that broke, out of the forty
you ran a command on — ++alt+w++ writes the focused pane's whole retained
scrollback to a file:

```
lazycssh-<alias>-<timestamp>.log
```

in the directory lazycssh was started from, ANSI escapes stripped, same as
the clipboard copies. The status bar confirms it — `wrote 214 lines of
web-03's scrollback to lazycssh-web-03-2026-08-10_09-14-02.log` — or says why
it did not, if the write failed.

This is a one-shot, explicit export, not the same thing as
[logging a run to disk](#logging-a-run-to-disk) (`--log-dir`): nothing is
written until you press the key, and only the one pane you are looking at,
never every host for the whole run.

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

## Logging a run to disk

Off by default. Start a run with `--log-dir DIR` and every host's output is
written to its own file in a fresh run directory:

```
DIR/2026-08-09_14-05-09/web-01.log
```

While logging is on, the status bar carries `SESSION LOGGING ON` for the whole
run, and a clean exit prints where the logs went. Files rotate at 8 MiB (one
older generation, `web-01.log.1`, is kept), a reconnect appends to the same
file with a visible marker, and everything is `0600` in a `0700` directory.

What never reaches the files: your keystrokes — the log taps only what the
hosts print — and any output that arrives while `single` mode is active,
because that is the mode a sudo prompt is answered in. The pause is marked in
the file (`[lazycssh: output not logged: single-mode input]`), so a gap is
visible instead of silent. Before typing a password into a pane, switch to
`single` mode; that is what pauses the pen.
