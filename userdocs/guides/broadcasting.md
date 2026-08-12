# Typing and broadcasting

There are three ways to send input, and the status bar always says which one is
active.

## Into one host

Click a pane, or press ++enter++ on it from the sidebar, or tab-cycle into the
grid. The pane is now a terminal: every keystroke is encoded and written to that
host as you type — no ++enter++ required, and ++ctrl+c++, ++tab++, ++esc++ and
the arrows all belong to the remote shell.

```
TYPING web-01 — ctrl+] leaves · alt=app
```

The terminal's cursor moves into that pane, onto the cell the host's own shell
says the next character lands in — including a cursor an app moves about, so
editing a line is as trackable as it is in a plain terminal. There is only ever
one cursor on screen: the other panes show none, and it disappears entirely when
no pane, prompt or dialog is taking input, when the pane is scrolled back, or
when the host is not connected.

lazycssh keeps exactly two kinds of key for itself while you type:

- ++ctrl+bracket-right++ — the reserved escape. It returns you to the app level,
  on the Status panel, which answers where the keys go now. It is the telnet
  escape on purpose: one sequence that always means "give me my keyboard back".
- the ++alt++ and ++shift++ pane chords — ++alt+arrows++, ++alt+z++,
  ++alt+plus++, ++alt+x++,
  ++alt+r++, ++alt+y++, ++alt+space++, ++shift+page-up++ and friends.
  Combinations the encoder never produced, so intercepting them forwards
  nothing you could otherwise have sent.

Typing into a pane **never** fans out, whatever the broadcast mode says: the
write goes down a one-host path that bypasses the router entirely. Typing into a
host that cannot take input says so rather than dropping the keys silently.

## Into the whole target set — the broadcast bar

++5++ focuses the bar under the grid. While it has the keyboard, it is a
terminal for the whole target set: every keystroke fans out live, so ++ctrl+c++
interrupts every target and ++tab++ completes on every target.

```
BROADCASTING EDIT → 7 hosts
```

The bar never mirrors what you typed. Each pane shows its own host's echo, which
is the point: a completion that differs between hosts is visible as N panes
disagreeing rather than as one plausible-looking line. ++enter++ sends a
carriage return and records the assembled line in the
[command log](../concepts/security.md#the-audit-trail-records-commands-not-keystrokes)
— once, as a line, never as individual keystrokes, because this is where a
password gets typed.

The bar's title carries the live target count (`Broadcast [5] → 7 hosts`).
++ctrl+bracket-right++ leaves it.

### Edit mode and view mode

The bar has exactly two modes.

- **Edit mode** (the default) — keystrokes go to the hosts.
- **View mode** — every key is an app-level command instead: broadcast scope,
  selection, panel numbers, ++ctrl+a++, ++ctrl+r++, the pane chords. Nothing is
  sent, so you can drive the interface without leaving the input. The status bar
  reads `BROADCAST VIEW — keys are commands`.

++enter++ in view mode goes back to edit mode; selecting the bar again (++5++, a
click) also lands in edit mode. The mode does not outlive the bar's focus.

### The `ctrl+a` prefix

Inside the bar, ++ctrl+a++ is a csshx-style escape prefix:

| Sequence | Effect |
|---|---|
| ++ctrl+a++ ++ctrl+a++ | send one literal ++ctrl+a++ to the targets — the GNU-screen double press |
| ++ctrl+a++ ++a++ | the same literal, matching `screen`'s own ++ctrl+a++ ++a++ |
| ++ctrl+a++ ++right++ / ++left++ | page to the next / previous screenful |
| ++ctrl+a++ ++escape++ | switch to view mode |
| ++ctrl+a++ *anything else* | forward that key to the targets as a normal keystroke |

The prefix exists for the remote multiplexer, so it forwards by default: press
++ctrl+a++ ++ctrl+a++ then ++c++ to open a `screen` window on every host, or
++ctrl+a++ ++ctrl+a++ then ++n++ to step them all to the next one. Only those
few sequences are kept by lazycssh; everything else after the prefix reaches
the hosts. The prefix is cleared before the second key is handled, so it cannot
chain, and a prefixed key never enters the recorded command line. While a prefix
is armed the status bar says so:
`ctrl+a… ←/→ page · ctrl+a/a = literal · esc = view · any other key goes to the hosts`.

The same prefix works outside the bar — in a pane and at the app level — where
++ctrl+a++ ++right++ / ++left++ page and ++ctrl+a++ ++a++ sends the literal to
the focused host.

App commands from inside the bar go through view mode (++ctrl+a++ ++escape++),
which is the one place keys are commands instead of keystrokes.

## One command — the `:` line

++colon++ at the app level opens a prompt for a single command sent to the whole
active broadcast set. The prompt carries the scope with it:

```
:systemctl restart nginx → BROADCAST all (7/8 up)
```

The number of machines about to receive it is on screen at the moment you type
it, which is why there is no confirmation dialog.

- While the prompt is open, **every** key belongs to it: a command containing
  `b` does not switch the broadcast mode, a `:` does not open a second prompt,
  and ++ctrl+c++ while editing does not reach forty machines.
- ++enter++ sends, ++esc++ abandons, ++up++/++down++ walk the command history
  (repeats are not stored twice; walking past the newest entry returns to an
  empty line). History survives a restart: it is saved to `history` under
  `~/.config/lazycssh` as you send, and reloaded the next time you open
  lazycssh.
- Afterwards the status bar reports against the scope:
  `sent to 2/3 hosts (1 did not receive it)`.

Resending an entry from the command log (++4++, ++enter++) takes the same path,
so it goes to the set that is active **now** — replaying the old target list
would send a command to machines you have since paged away from.

### Catching up a host that missed a command

A host that reconnects after the fleet already ran a command missed it, and
resending to everybody would run it twice on the machines that did not. In the
command log, ++m++ on an entry sends it to the hosts that are up **now** and
were **not** among its targets — and to nobody else. ++p++ previews the entry and names
them and counts them (`missing → 2 hosts`) before you press the key; with
nothing missing it says `all hosts already received this` and sends nothing.

A host that *did* receive the command is never offered again, even if it has
reconnected since and its shell has forgotten what the command did — re-running
there is the ordinary ++enter++ resend. A clone, or a host that joined the run
after the send, has never received it and is offered.

## Switching what you address

| Key | Mode |
|---|---|
| ++b++ | `all` — the active working set |
| ++shift+b++ | `selected` |
| ++s++ | `single` — the focused pane, for a sudo prompt or an editor |
| ++ctrl+alt+b++ | `fleet` — every host in the run, always rendered as a warning |

What each mode targets is in [Broadcast scope](../concepts/broadcast.md).

## Answering authentication prompts

A pane waiting for a password, a passphrase or a host key question behaves like
a plain terminal running `ssh`: the question is printed in that pane and the
answer is typed into it. Two ways:

- **into the focused pane** — answers that host only, which is how per-host
  passwords work;
- **into the broadcast bar** — mirrors every keystroke into every *prompting*
  pane and submits them all on ++enter++. One typing action logs a uniform
  cluster in.

Auth prompts are answered against the **scope**, not the live targets: a session
waiting at a password prompt is exactly *not* connected yet, so filtering by
liveness would drop the very panes you are answering.

A masked answer echoes nothing (a cursor block marks the waiting prompt); an
echoing keyboard-interactive answer shows as typed. ++enter++ submits, ++esc++
or ++ctrl+c++ cancels and fails that attempt, ++ctrl+q++ still quits. The status
bar carries `AUTH <host>` (or `AUTH n hosts`) while prompts are open, and the
Status panel lists which hosts are asking. A line that no live host received is
never recorded in the command log — it may be a password.
