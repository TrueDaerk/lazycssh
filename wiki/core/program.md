---
type: concept
title: Program assembly
description: The one place every layer meets - building the fleet, wiring the router and the UI together, and the wrapper model that acts on what the UI may only ask for.
resource: internal/program/program.go
tags: [program, wiring, bubbletea, transport]
timestamp: 2026-07-30T23:00:00Z
---

# Program assembly

`internal/program` is the only package allowed to know every layer. It turns a resolved command
line into a running application: the [session manager](./manager.md) over the resolved hosts,
the [working set](./working-sets.md), the [broadcast router](./broadcast-scope.md), the
[command log](./command-log.md) and the [TUI](./tui.md), assembled in `Build` and run as one
bubbletea program.

The layering rules this preserves:

- `internal/ui` cannot dial. It reads the fleet through the `Fleet` interface and *asks* for
  transport actions by emitting messages;
- `internal/ssh` knows nothing about rendering. It reports through one event channel; the UI's
  `Update` re-reads the fleet into its model snapshot when an event arrives.

## The wrapper model

`Model` wraps `ui.App`. Every message passes through its `Update`; most are forwarded, four are
acted on:

| Message | Action |
|---------|--------|
| `ui.ReconnectHostMsg` | `Manager.Reconnect` in a `tea.Cmd`, then a redraw |
| `ui.CloseHostMsg` | `Manager.Close` in a `tea.Cmd`, then a redraw |
| `ui.RemoveHostMsg` | `Manager.Remove` + `Router.Forget` + `SetHosts` + PTY resize, then `HostsChangedMsg` |

The program owns the run's live pattern list: seeded from the CLI, extended by every runtime
connect and session launch, pruned when a removal names a pattern exactly, and handed to the UI
with every `HostsChangedMsg` so a save writes how the run was actually assembled.
| `ui.SessionLaunchMsg` | load the saved session, resolve its patterns, `Manager.Add` each host |
| `tea.WindowSizeMsg` | forwarded to the UI, then every remote PTY is resized to the pane content size |

Launching and merging a session both **add** hosts: panes already in the run are never torn
down by loading more. A non-merge launch additionally applies the session's broadcast mode and
working set, because that is what "launch" promises. A session file that cannot be loaded
leaves the run alone and re-reads the session directory, which is how the `(unreadable)` row
becomes visible.

## The event pump

The transport's event channel is drained by a self-re-arming `tea.Cmd`: it blocks on one event,
converts it (`OutputEvent` → `SessionOutputMsg`, everything else → `FleetUpdatedMsg`) and the
`Update` that receives the converted message immediately returns the next pump command. Events
carry no payload and may be dropped by the transport under load; that is fine, because every
`FleetUpdatedMsg` makes the UI's `Update` re-read the whole fleet into its model snapshot — one
surviving event carries everything the dropped ones hinted at — see [TUI shell](./tui.md).

## Authentication, for now

The real session factory offers the agent and identity files, and verifies host keys against
`known_hosts`. There is **no prompter yet**: a host that needs a password, a passphrase the
agent does not hold, or an unknown host key fails its own pane with a clear error instead of
hanging. The interactive prompt inside the TUI is tracked in its own issue (#87).
`--insecure-ignore-host-key` swaps in the explicit insecure callback and the status bar carries
the warning for the whole run.

## Shutdown

When the event loop returns, the dial context is cancelled **before** `CloseAll`, so a session
that would have connected afterwards cannot leak its goroutines, and `Wait` collects the rest.

## Testing

`Config.NewSession` and `Config.Resolver` exist for tests: a fake factory and a zero resolver
make every test in the package run without a network and without reading the developer's
`~/.ssh/config`. The wrapper is driven the way every model here is driven — synthetic messages
through `Update`.
