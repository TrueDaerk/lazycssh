# Log

## 2026-07-28

- Added `internal/hosts` with bash-style brace expansion of host arguments, documented in
  `core/host-expansion.md`. Malformed patterns are rejected rather than silently kept as
  literals, which is a deliberate divergence from bash. Version 0.1.1.

- Bootstrapped the Go module as `github.com/TrueDaerk/lazycssh` (the remote, not the
  `github.com/geant/...` path `CLAUDE.md` guessed at). Added `internal/version` with the
  `Version` constant at `0.1.0`, the `lazycssh` entrypoint with `--version`, and a CI workflow
  running gofmt, `go mod tidy`, build, vet and `go test -race`. New concept document
  `core/cli.md`.

- `contributing/conventions.md`: split testing into "what must be tested" and "how to test".
  Added the two binding obligations (exported behavior with branches, bugfix regression test),
  the exemption for delegation and rendering glue, and the deliberate absence of a coverage
  threshold. Mirrored as a short pointer in `CLAUDE.md`.

- Documented the issue type labels (`epic`, `idea`, `bug`, `enhancement`) in
  `contributing/issue-types.md` and linked them from the workflow document.

- Created the wiki bundle. Extracted process documentation out of `CLAUDE.md` into
  `contributing/`: code conventions, development workflow, versioning policy and the
  OKF wiki format rules. `CLAUDE.md` now links here instead of restating them.
