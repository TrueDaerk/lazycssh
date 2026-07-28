---
type: guide
title: Development workflow
description: Issue-driven development — how work is created, branched, reviewed, merged and cleaned up.
tags: [workflow, github, git, process]
timestamp: 2026-07-28T00:00:00Z
---

# Development workflow

Work is tracked in **GitHub issues**. Every feature and every bugfix starts as an issue and is
closed by a merged PR. Use `gh` for all of it.

One issue = one branch. Never work on `main` directly.

## Starting work

```sh
gh issue list
gh issue view <n>
git switch -c <type>/<n>-<short-slug>   # feat/12-broadcast-modes, fix/34-pane-crash
```

Branch name pattern: `<type>/<issue-number>-<short-slug>`. Types follow the usual
`feat` / `fix` / `refactor` / `docs` / `chore` set.

## Creating issues

An issue is written before the work, not after. It states:

- the user-visible problem or the capability being added,
- the acceptance criteria that let the PR be called done,
- any constraint that shapes the design (security, backpressure, broadcast semantics).

Issues that only exist to describe a commit already written are not the workflow.

## Closing sequence

When an issue is done, the full closing sequence runs — none of these steps are optional:

1. **Docs** — update the wiki concept document(s) the change touches, refresh their `timestamp`,
   add a `log.md` entry. Update `README.md` if user-facing behavior or flags changed.
2. **Version bump** — see [Versioning](./versioning.md). Commit it with the rest of the work.
3. **PR** — `gh pr create` against `main`. The body references the issue with `Closes #<n>`
   so it auto-closes on merge.
4. **Merge** — merge into `main` once checks pass.
5. **Cleanup** — delete the branch locally and remotely (`git branch -d`,
   `git push origin --delete` or `gh pr merge --delete-branch`), then
   `git switch main && git pull`.
