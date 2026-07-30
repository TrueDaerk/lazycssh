# Broadcast scope

A run has 40 hosts. You narrow the working set to 20. You type a command in
`BROADCAST all`. Does it reach 20 machines or 40?

**`all` means the active working set.** Once you have said "I am working on
these twenty", every subsequent action is about those twenty until you say
otherwise; a mode called "all" that quietly meant "all forty" would make
narrowing the working set a decoration. Reaching every host in the run is a
separate mode with its own chord.

## The four modes

| Mode | Key | Targets |
|---|---|---|
| `all` | ++b++ | the active [working set](groups-and-sessions.md#working-sets) — the whole run when nothing is narrowed |
| `selected` | ++shift+b++ | toggled hosts ∩ working set |
| `single` | ++s++ | the focused pane only; deliberately ignores the working set |
| `fleet` | ++ctrl+alt+b++ | every host in the run; ignores the working set, always rendered as a warning |

`selected` **intersects** rather than unions. A selection made before the
working set changed must not quietly widen the blast radius, so hosts selected
outside the set stay visibly selected and are reported as excluded instead of
silently receiving input.

`fleet` is `ctrl+alt+b` on purpose: it is the one mode that ignores the working
set, so it is not a single letter and you cannot land in it by cycling through
the others. Its status line reads `BROADCAST EVERY HOST`.

`single` is one keystroke because that is what a `sudo` prompt needs — press
++s++ and the status bar names the one host it now sends to.

## Scope and targets

Two different questions, kept apart:

- **Scope** — who you are addressing.
- **Targets** — who can actually receive right now. Scope minus every host
  whose session cannot take input (dialling, failed, closed).

A host that is down is excluded from the count *and* from delivery: a count
that included it would promise something the transport cannot do.

```
BROADCAST all (7/8 up)
BROADCAST all (6/8 up, 1 alt-screen skipped)
BROADCAST set:front-half (19/20 up)
BROADCAST selected (3/3 up)
BROADCAST single web-01 (1/1 up)
BROADCAST EVERY HOST (38/40 up)
```

The first number is always the target count, and a narrowed working set is
named in the label — `all` never appears while fewer than every host is
addressed.

### Full-screen apps

In `all` and `selected` mode, a host whose remote app is on the alternate
screen is skipped: a keystroke meant for one `vim` must not reach twenty of
them. That exclusion applies only while the scope is **mixed**. When every
reachable host in scope is on the alternate screen there is no stray editor to
protect — that uniform state is what a broadcast which *opened* those apps looks
like — so the keystrokes flow to all of them. That is how you drive `vim` on a
whole fleet from the broadcast line. See
[Full-screen apps](../guides/full-screen-apps.md).

## What is *not* in scope

**Typing into a focused pane never fans out.** A focused pane writes through a
one-host path that bypasses the router entirely, whatever the broadcast mode
says. The scope governs the broadcast bar (++5++) and the command line
(++colon++) — nothing else.

**Paging does not change the scope.** Moving the window to the next screenful
shows you different panes; it does not change who receives a keystroke.

**Narrowing the view does.** The connected-only filter (++ctrl+a++), an active
split (++ctrl+s++) and the foreground session each push a visibility limit into
the router, so `all` and `selected` stop at what is on screen. `fleet` is
deliberately exempt — an escape hatch that can be silently narrowed is not one.

## Sending

One host that refuses a write does not stop the others: a broken pipe is one
dead pane, never a command half the fleet missed without anyone saying so. Every
send reports against the scope:

```
sent to 40/40 hosts
sent to 7/40 hosts (33 did not receive it)
```

Zero delivery is reported too. A keystroke that reached nobody — empty scope,
every host down — says `sent to 0/N hosts … — no host can take input right now`
rather than looking like it worked. Typing into the void must not look like
typing.
