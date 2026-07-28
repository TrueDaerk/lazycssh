---
type: guide
title: Versioning
description: Semver policy for lazycssh and the single source of truth for the version number.
resource: internal/version
tags: [semver, release, git-tags]
timestamp: 2026-07-28T00:00:00Z
---

# Versioning

Semver. Single source of truth: a `Version` constant in `internal/version`, plus a matching
`v<x.y.z>` git tag on `main` after the merge.

| Level | When |
|-------|------|
| **Patch** (`0.1.3` → `0.1.4`) | The default. Bump after every closed issue: bugfixes, small features, refactors, docs-only changes that ship. |
| **Minor** (`0.1.4` → `0.2.0`) | On request, or occasionally for a large new feature. When a change feels minor-worthy, bump it and say so; the user can correct it. |
| **Major** | **Never** on an agent's own initiative. Only on explicit request. |

The bump is committed together with the work it belongs to, not as a separate follow-up PR —
see the closing sequence in [Development workflow](./workflow.md).
