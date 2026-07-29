---
type: reference
title: Host resolution
description: How an expanded host argument becomes a dialable target using ~/.ssh/config, and the precedence between command line, config and defaults.
resource: internal/hosts/resolve.go
tags: [hosts, ssh-config, cli]
timestamp: 2026-07-29T19:00:00Z
---

# Host resolution

After [brace expansion](./host-expansion.md), every argument is resolved into a `Host`: the
address to dial, the login user, the port, the identity files to offer and a jump host.

Aliases are the normal case. `lazycssh bastion` works because `~/.ssh/config` says what
`bastion` means, and lazycssh reads the same file ssh does, via
`github.com/kevinburke/ssh_config`.

## Argument form

```
[user@]host[:port]
```

`root@srv1.example.com:2222` is understood. IPv6 addresses need brackets when a port follows —
`[2001:db8::1]:2222` — because a bare `2001:db8::1` is otherwise indistinguishable from a host
with a port. A bare IPv6 address without brackets is accepted and takes the default port.

## Precedence

The same order ssh itself applies:

1. what the command line states,
2. what the matching `~/.ssh/config` block states,
3. the built-in default (port 22, the current OS user).

Directives read: `HostName`, `User`, `Port`, `IdentityFile` (repeatable, order preserved) and
`ProxyJump`. `ProxyJump none` is treated as no jump host. `~` in identity paths is expanded.
Glob patterns in `Host` lines match as in ssh: `Host web-*` applies to `web-1`.

## Alias versus address

A resolved host keeps both:

- `Alias` — what the user typed, minus `user@` and `:port`. This is what panels and error
  messages display, because it is what the user recognises.
- `Addr` — what gets dialled, from `HostName` when one matched, otherwise the alias itself.

With `Host web-1 / HostName 10.0.0.1`, the pane is labelled `web-1` and the connection goes to
`10.0.0.1`.

## Listing candidates

`Resolver.Aliases()` enumerates the concrete host aliases the config declares, in file order,
deduplicated — the offer behind the host prompt's completion hints. Only patterns that *name*
one host qualify: wildcards (`*`, `web-?`) match rather than name, negations (`!bastion`)
exclude, and neither is expanded into the hosts it would cover — the config does not know them
either. A nil resolver or one without a config returns nil.

## Missing or unusable config

A missing `~/.ssh/config` is not an error — most users do not have one, and lazycssh works
without it. An unreadable file *is* an error, naming the path, because silently ignoring a
config the user believes is in effect would connect as the wrong user.

The parser is deliberately lenient about content, as OpenSSH is: unknown keywords and odd lines
are ignored rather than rejected. An invalid `Port` value in the config is still an error at
resolution time, since there is no sane fallback.

## Testing

Resolver tests run against `internal/hosts/testdata/ssh_config`, never against the developer's
real configuration.
