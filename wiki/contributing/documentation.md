---
type: guide
title: Documentation layers
description: The three places lazycssh documents itself — wiki, userdocs site, repo root files — and which change belongs where.
resource: userdocs/
tags: [documentation, mkdocs, wiki, process]
timestamp: 2026-07-31T21:00:00Z
---

# Documentation layers

Three layers, three audiences. A change updates the ones it invalidates — none of them is
optional when it is the one that went stale.

| Layer | Audience | Question it answers |
|-------|----------|---------------------|
| `wiki/` | contributors and agents | why is it built this way, and what may it never do |
| `userdocs/` | users | how do I use it |
| `README.md`, `CONTRIBUTING.md`, `SECURITY.md` | someone arriving at the repository | what is this, how do I build it, where do I report things |

The split is deliberate: the wiki carries decisions and their reasoning — the alt-screen
exclusion rule, why `all` means the working set, why a changed host key is never a prompt — and
the site carries the behaviour those decisions produce. The site links to the wiki for
internals rather than restating it, so the same rule is not written twice and half-updated once.

## The documentation site

MkDocs Material, sources in `userdocs/`, configuration in `mkdocs.yml` at the repository root
(issue #195). `docs_dir: userdocs`, `strict: true`.

```sh
pip install -r userdocs/requirements.txt
mkdocs serve                 # preview on http://127.0.0.1:8000
mkdocs build --strict        # what CI runs
```

`.github/workflows/docs.yml` builds the site on every pull request that touches `userdocs/`,
`mkdocs.yml` or the workflow itself, and deploys to GitHub Pages on a push to `main`. `--strict`
turns broken links and warnings into build failures, so a page that is not reachable from the
`nav` in `mkdocs.yml` fails CI rather than being published as an orphan.

`site/` is build output and is git-ignored.

### Structure

| Section | Holds |
|---------|-------|
| Getting started | installation, first run, the command line |
| Concepts | the grid and the window, broadcast scope, groups/sessions/working sets, the security model |
| Guides | one page per task: connecting, broadcasting, selecting, reading output, full-screen apps, saving |
| Reference | keybindings, CLI flags and exit codes, the session file schema |
| Troubleshooting | the failure modes that are not bugs |
| About & contributing | what this project is, and how to contribute |

The reference pages are written by hand, not generated. The keymap remains the single source of
truth **inside** the program — the `?` overlay is generated from it — so a binding change
updates `core/keys.md` and `userdocs/reference/keybindings.md` in the same PR.

### What the site states about the project

That lazycssh is a personal project built with heavy AI assistance, with no support promise and
public on purpose. It appears in `README.md`, on the site's home page and on the About page. It
is not a disclaimer to be softened away: it is what lets a reader judge whether the defaults
were chosen for a situation like theirs.

## Rules

- A user-visible change updates `userdocs/` in the same PR, and `README.md` when a flag or a
  headline behaviour changed.
- An architectural change updates the matching `wiki/core/*` document, refreshes its
  `timestamp`, and adds a `log.md` entry — see [Wiki format](./wiki-format.md).
- The site never documents a behaviour the wiki contradicts. The wiki is the source of truth;
  when they disagree, one of them is a bug.
- Everything is written in English.
