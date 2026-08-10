---
type: guide
title: Development workflow
description: Issue-driven development — how work is created, branched, reviewed, merged and cleaned up.
tags: [workflow, github, git, process]
timestamp: 2026-08-10T00:00:00Z
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

An issue is written before the work, not after. Issues that only exist to describe a commit
already written are not the workflow.

### Before creating: check for duplicates

1. Search open **and** closed issues: `gh issue list --state all --search "<keywords>"`.
2. Skim the task list of any epic the topic belongs to.
3. If a matching issue exists, extend or comment on it instead of opening a new one; if it
   exists but is closed and the problem is back, reopen it.

### Conventions

- **Title and body in English.**
- **Body:** state the user-visible problem or the capability being added, plus any constraint
  that shapes the design (security, backpressure, broadcast semantics). List concrete
  acceptance criteria as a `- [ ]` checklist — they are what lets the PR be called done.
  Include tests and wiki updates in the checklist when they apply. Name dependencies by issue
  number (`Depends on #12`). Sub-issues link their epic (`epic: #<n>`).
- **Scope:** one issue = one independently completable, reviewable task. Split rather than
  batch.
- Every issue carries exactly one **type label** — see [Issue types](./issue-types.md).

```sh
gh issue create --label enhancement --title "..." --body "..."
```

### Epics and milestones

An epic gets a **GitHub milestone** of its own, assigned to the epic and all its sub-issues;
the milestone's progress bar is the progress tracking. New sub-issues are added to the epic's
task list and its milestone. When the last sub-issue closes, close the epic and its milestone.

Discoveries out of scope while working an issue become **new** issues (after the duplicate
check), not scope creep on the current one.

## Closing sequence

When an issue is done, the full closing sequence runs — none of these steps are optional:

1. **Docs** — update the wiki concept document(s) the change touches, refresh their `timestamp`,
   add a `log.md` entry. Update `userdocs/` if a user can see the change, and `README.md` if
   user-facing behavior or flags changed — see [Documentation layers](./documentation.md).
2. **Version bump** — see [Versioning](./versioning.md). Commit it with the rest of the work.
3. **PR** — `gh pr create` against `main`. The body references the issue with `Closes #<n>`
   so it auto-closes on merge.
4. **Merge** — run `gofmt -l .`, `go vet ./...` and `go test -race ./...` locally (there is no
   CI; the GitHub Actions workflows were removed in issue #287), then merge into `main`.
5. **Cleanup** — delete the branch locally and remotely (`git branch -d`,
   `git push origin --delete` or `gh pr merge --delete-branch`), then
   `git switch main && git pull`.
