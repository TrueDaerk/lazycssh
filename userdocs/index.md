# lazycssh

lazycssh is a terminal UI for parallel SSH: it opens sessions to many hosts at
once, broadcasts your keystrokes to all of them, and shows every host's output
in one screen. It is shaped like [lazygit](https://github.com/jesseduffield/lazygit)
— numbered panels down the left, single-letter commands, a help overlay
generated from the keymap — and it replaces classic `cssh`.

```
╭ Status [1] ─╮┌─────────────┬─────────────┐
│ keys go to: ││ 1 web-01 ok │ 2 web-02 ok │
│  BROADCAST  │├─────────────┼─────────────┤
╰─────────────╯│ 3 web-03 ok │ 4 web-04 ✗  │
╭ Groups [2] ─╮└─────────────┴─────────────┘
╰─────────────╯╭ Broadcast [5] → 4 hosts ──╮
╭ Sessions [3]╮╰───────────────────────────╯
╰─────────────╯ BROADCAST all (4/4 up)   ?
```

## Install

lazycssh is a single Go binary. You need [Go 1.26+](https://go.dev/dl/).

```sh
git clone https://github.com/TrueDaerk/lazycssh.git
cd lazycssh
make install          # go install ./cmd/lazycssh
```

Then start it with hosts, with a saved group, or with nothing:

```sh
lazycssh                                # empty run; press n to connect
lazycssh web-01 web-02 db-01
lazycssh 'srv1-{01..40}.example.com'    # brace expansion, done by lazycssh
lazycssh @prod-web                      # a saved group
```

Host arguments are resolved through `~/.ssh/config`, so aliases, `HostName`,
`User`, `Port`, `IdentityFile` and `ProxyJump` behave the way ssh makes them
behave.

## Find your way around

<div class="grid cards" markdown>

- :material-rocket-launch: **[Getting started](getting-started/index.md)**

    Install, open your first hosts, and learn the handful of keys that get you
    moving.

- :material-shape: **[Concepts](concepts/index.md)**

    The grid, the window and the working set; what `all` means when you
    broadcast; groups versus open sessions; the security model.

- :material-book-open-variant: **[Guides](guides/index.md)**

    One page per task — connecting, broadcasting, selecting hosts, reading
    output, running `vim` on a fleet, saving a run.

- :material-table: **[Reference](reference/index.md)**

    Complete tables of keybindings, command line flags and the session file
    schema.

</div>

Stuck? [Troubleshooting](troubleshooting.md) collects the failure modes worth
knowing about.

## The short version of the keyboard

| Keys | What it does |
|---|---|
| ++n++ | Connect a host — any pattern; `~/.ssh/config` aliases complete with ++tab++ |
| ++5++ | Focus the broadcast bar: every keystroke goes to the whole target set |
| ++enter++ on a pane, or a click | Type into that one host |
| ++ctrl+bracket-right++ | Stop typing, give the keyboard back to lazycssh |
| ++colon++ | Send one command to the broadcast set |
| ++1++ ++2++ ++3++ ++4++ | Status, Groups, Sessions, Command log panels |
| ++question++ | The full keybinding overlay |

Everything is in the [keybinding reference](reference/keybindings.md), and the
`?` overlay is generated from the same keymap the program dispatches on — so it
cannot drift.

## The one safety rule

lazycssh types on many machines at once. That is the point, and it is also the
footgun. The design answer is not a confirmation dialog — you would click
through it forty times a day — but an **unmissable target count**:

```
BROADCAST all (7/8 up)
BROADCAST set:front-half (19/20 up)
BROADCAST EVERY HOST (38/40 up)
TYPING web-01 — ctrl+] leaves · alt=app
```

The status bar always says where your keys go and how many machines will
receive the next one. Anything that weakens a default — host keys unverified,
broadcasting to every host — is repeated there in a warning style for as long
as it is true. [Broadcast scope](concepts/broadcast.md) explains what each mode
targets.

## A note on what this is

lazycssh is a personal project — built by one person, to that person's taste,
with heavy AI assistance. There is no support promise and no roadmap
commitment.

That said: it is public on purpose. Use it if it suits you, and pull requests
that improve it are genuinely welcome — see
[About & contributing](contributing.md). The
[licence](https://github.com/TrueDaerk/lazycssh/blob/main/LICENSE) is MIT.

## Looking for the internals?

This site documents *using* lazycssh. The architecture documentation — one
concept document per subsystem, aimed at contributors — lives in
[`wiki/`](https://github.com/TrueDaerk/lazycssh/blob/main/wiki/index.md) in the
repository, and planning happens in
[GitHub issues](https://github.com/TrueDaerk/lazycssh/issues).
