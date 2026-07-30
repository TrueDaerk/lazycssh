---
type: concept
title: Groups and open sessions
description: Persisted host groups, the open sessions they become, and how the foreground session scopes the grid and the broadcast.
resource: internal/ui/opensessions.go
tags: [groups, sessions, workspace, broadcast]
timestamp: 2026-07-30T09:00:00Z
---

# Groups and open sessions

Two words, two lifetimes:

- A **group** is a persisted host list: a [session file](./session-files.md) on disk. It
  survives restarts and is managed in the Groups panel — `n` creates, `d` deletes, `enter`
  opens.
- An **open session** is a group at runtime: a named set of hosts that is on screen together.
  Several can be open at once; exactly one is in the **foreground**.

## Opening

`enter` on a group emits `GroupOpenMsg`; the program loads the file, resolves every pattern
through `~/.ssh/config` — `HostName`, `Port`, `User` and `IdentityFile` apply, exactly like any
other connect, see [Host resolution](./host-resolution.md) — and adds the hosts to the fleet.
Hosts already in the run are reused by identifier, never dialled twice. The program answers
with `SessionOpenedMsg`, and the UI upserts the session and foregrounds it.

Opening group B while group A is on screen backgrounds A: its panes leave the grid, its
connections stay. Opening A again — from the Groups panel or the Sessions panel — restores its
panes without reconnecting anything.

## The foreground

The foreground session is what the interface is about:

- the **grid** draws only its panes; focus, paging and hit-testing index into that list,
- the **broadcast scope** is limited to it: the UI pushes the visible host set into the
  router's [visibility limit](./broadcast-scope.md), so `all` and `selected` stop at the
  session's edge. `fleet` mode stays the explicit every-host escape hatch,
- the **status bar** carries its name, and `N sessions` when more than one is open.

Switching emits `GridChangedMsg` so the program can resize the remote PTYs to the new pane
size.

## Membership

- The CLI arguments of an unnamed run live in the built-in `adhoc` session; a run started with
  `@name` carries that name.
- A host connected at runtime (`n`, the prompt) joins the foreground session — it goes where
  the user is looking.
- A host may belong to several sessions: two groups naming the same machine share its one
  connection and its one pane identity.
- A host removed from the run leaves every session; a session that loses its last host closes.
  Deleting a group's *file* does none of this — definitions and live sessions have separate
  lifetimes.

## Ending a session

`x` on a row in the Sessions panel asks `end "name"? y/n`. On `y`, every **connected** terminal
of the session receives `ctrl+c` then `ctrl+d` — interrupt the foreground process, log the
shell out — via the pane write path, so it targets exactly the session's hosts whatever the
broadcast mode is, and nothing lands in the command log. The session is marked `(ending)` and
leaves the list once its hosts are done; a shell that swallows the keystrokes keeps it listed,
and `x` asks again and resends.

A session ends by itself when **all** of its hosts reach `closed` — which is what `ctrl+d`
typed in broadcast mode across the whole session produces. Its hosts then leave the run, unless
another open session still contains them. A session marked as ending also accepts `failed` as
done — a host that died mid-shutdown must not keep a zombie session listed — but a session
whose hosts merely all failed, never asked to end, stays: that is an outage to look at and
reconnect, not a completed shutdown.

## What this deliberately is not

Sessions do not own transports. There is one fleet, one `ssh.Manager`, one event stream; a
session is a *view* over it. Backgrounding is invisible to the remote host, and a chatty
background host still lands in its scrollback — see [Scrollback](./scrollback.md).
