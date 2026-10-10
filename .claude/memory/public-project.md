---
name: public-project
description: The repository is public — no personal data in PRs, commits, docs, specs, code comments or memory
metadata:
  type: feedback
---

The repository is public (decided and stated by the maintainer on 2026-10-10). Everything written
into it, or posted to GitHub, is public: PR titles and descriptions, commit messages, review
comments, `docs/`, specs and plans, code comments, test data, and these memory files.

- Never include anything about the maintainer's own accounts, library, collection or test data:
  counts ("about 20 non-games in the library", "96 Humble copies"), titles owned, which stores are
  connected, dates of their scans, results from their config copy, names, or "the user's …".
- Describe behaviour in general terms: "a PlayStation library can list apps, demos and
  soundtracks", "a catalog scanned before the fix holds old-format ids". Test fixtures use
  invented or well-known public data, not exports of the maintainer's catalog.
- Results from the maintainer's own data stay in the chat, never in the repository or on GitHub.
- Describe the license as source-available (PolyForm Noncommercial), see [[license]].

**Why:** the maintainer pointed out that PR #13 said "the user's PlayStation library has about 20
of them" — personal data in a public project.
**How to apply:** before opening a PR, committing docs/specs/plans, or writing a comment, reread
the text for personal details and rewrite them generically.
