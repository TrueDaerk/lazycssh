# Your first run

```sh
lazycssh web-01 web-02 web-03
```

Three panes appear, one per host, each dialling. The interface is a lazygit
stack: numbered panels down the left, the pane grid on the right, the broadcast
bar under the grid, a status bar along the bottom.

## Answering the connection questions

The first time you reach a host, its key is unknown, and the pane asks — in
ssh's own words, written into that pane's own output:

```
The authenticity of host 'web-01 (10.0.0.1)' can't be established.
ssh-ed25519 key fingerprint is SHA256:… .
Are you sure you want to continue connecting (yes/no)?
```

Type the answer where a terminal would take it. Click the pane (or ++enter++ on
it) and type `yes` — or, when all three are asking the same question, press
++5++ and answer once in the broadcast bar: every prompting pane receives the
keystrokes and submits together.

Password and passphrase prompts work exactly the same way, and echo nothing.
A wrong answer fails that one pane; the others keep going.

## Where do my keys go?

That question has one answer at a time, and the status bar always states it.

**Into one host.** Click a pane or press ++enter++ on it. Now it is a terminal:
every keystroke goes to that host as you type it — ++ctrl+c++, ++tab++, ++esc++
and the arrows all belong to the remote shell. The status bar says:

```
TYPING web-01 — ctrl+] leaves · alt=app
```

++ctrl+bracket-right++ gives the keyboard back to lazycssh. That escape is
reserved and always works — it is the one sequence to remember when you feel
stuck.

**Into all of them.** Press ++5++. The broadcast bar under the grid takes the
keyboard and every keystroke fans out to the whole target set, live:

```
BROADCASTING EDIT → 3 hosts
```

Each pane shows its own host's echo, so a completion that differs between hosts
shows up as three panes disagreeing rather than one garbled line. ++enter++
sends a carriage return and records the assembled line in the command log.

**One command, once.** Press ++colon++ from the app level. The prompt carries
the scope with it:

```
:systemctl restart nginx → BROADCAST all (3/3 up)
```

++enter++ sends it, ++esc++ abandons, ++up++/++down++ walk this run's history.
Afterwards the status bar reports against the scope:
`sent to 3/3 hosts`.

## Seeing what happened

Each pane header carries its number, the host name, the connection state, and
how the command you just sent ended there: `·` while it is still running, `✓`
for exit 0, `exit N` for a failure. A failing pane also gets a danger-coloured
border, the status bar counts them (`1 host failed`), and ++exclam++ jumps the
focus to the next failing host. Send another command and the marks reset.

Hosts whose shell reports no exit codes show nothing at all rather than a tick
lazycssh cannot back — see [Reading output](../guides/output.md).

## Getting around

| Key | Effect |
|---|---|
| ++shift+alt+left++ ++shift+alt+right++ ++shift+alt+up++ ++shift+alt+down++ | move between panes (works while typing) |
| ++alt+z++ | full-screen the focused pane; again to return |
| ++alt+plus++ | cycle screen modes: normal, half, full |
| ++alt+r++ | reconnect this host |
| ++alt+x++ | close this host — on a dead host, remove its pane |
| ++shift+page-up++ / ++shift+page-down++ | scroll this pane's history |
| ++tab++ / ++shift+tab++ | walk the panels, then the grid |
| ++question++ | the full keybinding overlay |
| ++q++ / ++ctrl+q++ | quit |

++q++ quits only while no input has the keyboard; while typing it is a letter
like any other. ++ctrl+q++ quits from anywhere, including out of a text field.

## Next

- [Command line](command-line.md) — host patterns, `@groups`, flags.
- [Broadcast scope](../concepts/broadcast.md) — what `all` targets, and the
  three other modes.
- [Saving groups and sessions](../guides/saving.md) — so the next run is
  `lazycssh @prod-web`.
