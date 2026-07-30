# About this project & contributing

## Why this exists

Classic `cssh` (cluster ssh) does the job: it opens a window per host and types
into all of them. It also opens a window per host — which stops being usable
somewhere around a dozen machines, has no idea which of them just failed, and
gives you no way to say "actually, only these twenty".

The tools that solved the *shape* of this problem are `lazygit` and
`lazydocker`: numbered panels, single-letter commands, one screen that answers
"what is going on" without a manual. lazycssh is that shape applied to parallel
SSH — every host in one grid, one keystroke to say who receives the next one,
and a status bar that never lets you forget how many machines that is.

The other half is the safety model. A tool that broadcasts to forty production
machines cannot ask "are you sure?" every time — you would stop reading it by
the second day. So it does the opposite: no confirmation, and an always-visible,
always-honest count of what the next keystroke reaches.

## What lazycssh is

lazycssh is a personal project. It is built by one person, to that person's
taste, with heavy AI assistance — "vibe-coded" is a fair description, and the
architecture notes in the repository's `wiki/` exist largely so the agents and
the human stay on the same page.

None of that is a warning label. It is just useful to know before you decide
whether it fits how *you* work.

- **Use it if you like it.** It is public on purpose. The
  [licence](https://github.com/TrueDaerk/lazycssh/blob/main/LICENSE) is MIT.
- **There is no support promise.** No SLA, no roadmap commitment, no guarantee
  a feature survives a refactor. Issues get worked on when they are interesting
  or in the way.
- **Improvements are very welcome.** Bug fixes, tests, documentation, and
  features that sit cleanly behind a flag all get read, and merged when they
  fit.

The one rule worth knowing in advance: a change that *adds* something has an
easier time than one that reshapes an existing default to a different taste. If
you are unsure which yours is, open an issue and ask before writing the patch.

## How to contribute

The full guide lives in the repository:

- **[CONTRIBUTING.md](https://github.com/TrueDaerk/lazycssh/blob/main/CONTRIBUTING.md)**
  — the issue-first workflow, branch naming, commit style, tests, and what to
  update in the docs.
- **[SECURITY.md](https://github.com/TrueDaerk/lazycssh/blob/main/SECURITY.md)**
  — reporting a vulnerability privately instead of in a public issue.

The short version:

1. Open an issue, or comment on an existing one, before writing code.
2. Branch as `<type>/<number>-<slug>` — `feat/`, `fix/`, `docs/`, `chore/`.
3. Ship tests with the change; `go test -race ./...` has to pass.
4. Update `wiki/` (architecture) and `userdocs/` (this site) when your change
   invalidates them, and bump `internal/version`.
5. Open a PR whose body says `Closes #<number>`.

Everything is written in English — code, comments, commits, issues, PRs.

## Filing a good bug report

Include the version (`lazycssh --version`), your terminal and OS, and two
things that are specific to this tool:

- **the host count** — plenty of layout and paging behaviour only shows up past
  the point where panes stop fitting;
- **the broadcast mode and the working set** you were in, because "it went to
  the wrong hosts" means something different in each of them.

[Troubleshooting](troubleshooting.md) covers the failure modes that turn out
not to be bugs.
