---
name: credentials
description: How rotating credentials, one-time codes and pasted browser sessions are stored and persisted
metadata:
  type: project
---
- Credentials that rotate are `schema.FieldState` settings: never shown to or accepted from the
  browser. Providers write the new value into the settings map; the service persists it
  (`Source.UpdateState`).
- One-time codes are exchanged in `sync.Preparer.Prepare`, with an in-memory cache by
  `sha256(code)` so "Test" then "Save" reuse the same code.
- If a provider also caches in memory, it must still hand the latest value back on every call:
  forgetting this lost Ubisoft's rotated ticket on 2026-10-08 (see [[ubisoft]]).
- Pasted browser cookies go through `internal/adapters/outbound/browsersession`.
