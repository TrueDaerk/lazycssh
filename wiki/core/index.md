# Core

The runtime pieces of lazycssh.

* [Command line interface](./cli.md) - flags, arguments and exit codes of the `lazycssh` binary
* [Host argument expansion](./host-expansion.md) - brace expansion of host arguments, and where it differs from bash
* [Host resolution](./host-resolution.md) - turning an argument into a dialable target via ~/.ssh/config
* [Scrollback buffer](./scrollback.md) - bounded per-session ring buffer, so a chatty host cannot stall the UI
* [SSH session lifecycle](./session.md) - one host end to end, and the event contract the UI depends on
* [Authentication](./authentication.md) - method order, credential caching across hosts, and how secrets stay out of logs
* [Host key verification](./host-keys.md) - known_hosts checking, why a changed key is never a prompt, and the one way out
* [Session manager](./manager.md) - owning a fleet: bounded fan-out dialling, one event channel, per-host reconnect
* [Working sets](./working-sets.md) - the current subject of work: counts, ranges, patterns, paging and named sets
* [Broadcast scope](./broadcast-scope.md) - what `all` means when a working set is active, and how the target count stays honest
