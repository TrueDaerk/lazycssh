# Core

The runtime pieces of lazycssh.

* [Command line interface](./cli.md) - flags, arguments and exit codes of the `lazycssh` binary
* [Host argument expansion](./host-expansion.md) - brace expansion of host arguments, and where it differs from bash
* [Host resolution](./host-resolution.md) - turning an argument into a dialable target via ~/.ssh/config
* [SSH session lifecycle](./session.md) - one host end to end, and the event contract the UI depends on
* [Terminal emulation](./terminal.md) - the per-session vt emulator that will carry full-screen apps
* [Authentication](./authentication.md) - method order, credential caching across hosts, and how secrets stay out of logs
* [Host key verification](./host-keys.md) - known_hosts checking, why a changed key is never a prompt, and the one way out
* [Session manager](./manager.md) - owning a fleet: bounded fan-out dialling, one event channel, per-host reconnect
* [Working sets](./working-sets.md) - the current subject of work: counts, ranges, patterns, paging and named sets
* [Broadcast scope](./broadcast-scope.md) - what `all` means when a working set is active, and how the target count stays honest
* [Session files](./session-files.md) - the on-disk format for a saved run, and why no credential ever appears in one
* [Groups and open sessions](./groups-and-sessions.md) - persisted host groups, the sessions they open into, and what the foreground scopes
* [Credential handling](./credentials.md) - agent, identity file or secret_command, and how a secret stays out of logs and files
* [Theme and styles](./theme.md) - the one place styles live, and why colour is never the only carrier of meaning
* [Keymap and help](./keys.md) - every binding declared once, the help generated from it, and how ambiguity is prevented
* [TUI shell](./tui.md) - the root model, the layout arithmetic, and why a resize can never panic
* [Program assembly](./program.md) - the one place every layer meets: building the fleet and running the TUI over it

* [Command log](./command-log.md) - what this run sent, to how many hosts, and what is deliberately never recorded
* [Session logging](./session-logging.md) - opt-in per-host output files, rotation, and why single mode pauses the pen
