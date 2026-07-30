# Contributing to lazycssh

Thanks for looking. Pull requests are genuinely welcome — this page describes
how the repository is organised so a patch has an easy time getting merged.

Please read [What this project is](#what-this-project-is) first. It is not
boilerplate; it explains the one thing that decides whether a change lands.

## What this project is

lazycssh is a personal project. It is built by one person, to that person's
taste, with heavy AI assistance — "vibe-coded" is a fair description, and the
architecture notes in [`wiki/`](wiki/index.md) exist largely so the agents and
the human stay on the same page.

That has consequences worth stating openly:

- **Design decisions are personal preferences, not committee output.** The
  keymap follows lazygit's shape, the broadcast semantics follow one specific
  opinion about what `all` should mean, and defaults are chosen for the setup
  it was built for.
- **There is no support promise.** No SLA, no roadmap commitment, no guarantee
  that a feature survives a refactor. Issues are worked on when they are
  interesting or in the way.
- **You are very welcome to use it anyway.** If it is useful to you, take it —
  see [`LICENSE`](LICENSE) (MIT).
- **Improvements are welcome.** A PR that fixes a bug, adds a test, sharpens
  the docs, or adds a feature cleanly behind a flag will get read and, if it
  fits, merged.

The corollary: a change that reshapes existing behaviour to a different taste
is a harder sell than one that adds an option. If you are unsure which one
yours is, open an issue first and ask — that costs you nothing and saves you
from writing a patch that gets declined on grounds you could not have guessed.

## Before you write code

**Open an issue first**, or comment on the one you are picking up. Every piece
of work in this repository starts as an issue and is closed by a merged PR;
planning lives entirely in
[GitHub issues](https://github.com/TrueDaerk/lazycssh/issues).

```sh
gh issue list --state all --search "<keywords>"
```

Every issue carries exactly one type label — `epic`, `idea`, `bug` or
`enhancement`. What each one commits to is written down in
[`wiki/contributing/issue-types.md`](wiki/contributing/issue-types.md). An
issue body states the user-visible problem, the acceptance criteria that let
the PR be called done, and any constraint that shapes the design (security,
backpressure, broadcast semantics).

## Working on a change

1. Branch per issue: `<type>/<number>-<slug>` — `feat/12-broadcast-modes`,
   `fix/34-pane-crash`, `docs/195-user-docs`. Never work on `main`.
2. Write the code **and its tests** (see below).
3. Update the docs you invalidated, in the same change:
   - [`wiki/`](wiki/index.md) is the architecture bundle (OKF v0.1) — update
     the matching concept document, refresh its `timestamp`, and add a
     [`wiki/log.md`](wiki/log.md) entry.
   - [`userdocs/`](userdocs/index.md) is the user-facing documentation site.
     If a user can see your change, say so there. `README.md` too when flags or
     headline behaviour changed.
4. Bump the version in `internal/version` — patch by default, minor for a large
   feature — and commit it with the work. See
   [`wiki/contributing/versioning.md`](wiki/contributing/versioning.md).
5. Open a PR whose body contains `Closes #<number>` so the issue closes on
   merge.

Everything — code, comments, doc comments, commit messages, issues, PRs — is
written in **English**.

### Commit messages

Conventional-commit style:

```
feat: broadcast reaches full-screen apps when every target is one
fix: keep the focused host when the host list changes
docs: user documentation site, README and contributor files
```

Keep the subject under ~72 characters and explain the *why* in the body when it
is not obvious.

## Building and testing

```sh
make build           # go build ./cmd/lazycssh
make install         # go install ./cmd/lazycssh
make test            # go test ./...
make vet             # go vet ./...
go test -race ./...  # what CI runs
```

The conventions are binding, not suggestions — the full text is in
[`wiki/contributing/conventions.md`](wiki/contributing/conventions.md). The
parts that come up most:

- `gofmt` and `go vet` clean; CI also checks `go mod tidy`.
- Errors are wrapped with context; no naked `panic` outside `main`. One dead
  host is one dead pane, never a program that exits.
- **Tests are not optional.** Every exported behaviour with branches or error
  paths gets tests, and every bugfix ships with a test that fails without the
  fix. Pure delegation and rendering glue are exempt; there is no coverage
  threshold.
- The SSH layer sits behind an interface, so UI tests use fake sessions and
  never dial. TUI tests drive `Update` with synthetic messages and assert on
  `View().Content` with the styling stripped.
- Security-relevant behaviour gets an explicit negative test: no credential in
  a log line or an error string, no silent connect to a changed host key.
- No global state; dependencies are passed into constructors.

`internal/version` is the single source of truth for the version number, and
`main` carries a matching `v<x.y.z>` tag after each merge.

### The documentation site

```sh
pip install -r userdocs/requirements.txt
mkdocs serve                 # preview on http://127.0.0.1:8000
mkdocs build --strict        # what CI runs
```

Broken links fail the build, so a page you add must be reachable from
`mkdocs.yml`'s nav.

## Licensing of contributions

By submitting a pull request you agree that your contribution is licensed under
the same terms as the project — see [`LICENSE`](LICENSE) (MIT). There is no
separate CLA.

## Reporting bugs and vulnerabilities

Bugs: open an issue with the `bug` label. Include the lazycssh version
(`lazycssh --version`), your terminal and OS, the host count, and the broadcast
mode you were in — the last two matter more than they sound.

Security issues: do **not** open a public issue. See [`SECURITY.md`](SECURITY.md).
