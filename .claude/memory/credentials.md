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
- **Two servers must never share a rotating credential.** On 2026-10-08 test servers on copies
  of the config ran Ubisoft's keep-alive, rotated the ticket in the copy and killed the real
  server's (user had to paste a new one). Test servers now start with `-no-unattended`; on a
  copy only scan Humble or Steam. The same applies to a user running two instances (e.g. local
  `task run` and the Docker image) with one config.
