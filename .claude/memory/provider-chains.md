---
name: provider-chains
description: Plex-style provider chains, TheGamesDB quota rules, the fallback pass and when to bump coverLogicChanged
metadata:
  type: project
---
- Chains for covers, barcodes and metadata. Default order = registration order in
  `cmd/gamevault/main.go`; new providers are appended to existing users' chains.
- Media providers can keep learned state in `schema.FieldState` settings: they write it into the
  settings map they get, and the media service persists it with `provider.UpdateState` (wired for
  barcode lookups only so far; CeX's `country_hits`, see [[cex]]).
- TheGamesDB has a monthly quota (~1,000 requests): it only applies to physical copies, games no
  store knows (`CoverQuery.HasStoreLink`), and on the fallback pass when no store had art.
  Add a new store's prefix to `HasStoreLink` when it gets a cover provider. See [[thegamesdb]].
- When cover resolution improves, bump `coverLogicChanged` in
  `internal/application/media/service.go` to the current UTC time (never in the future) so old
  "no cover" markers are retried.
- Add-ons (DLC, soundtracks…) borrow the base game's cover for the same system (`media/addon.go`).
- **Editions (2026-10-10):** covers are per (game, system) (`CoverQuery.System`, `QueryFor(g, system)`).
  Store providers apply only when `q.ForPC()`, Xbox to the PC and Xbox systems, TheGamesDB to any
  system with its quota rules per edition (its search filtered to the edition's system).
  `coverLogicChanged` was bumped on 2026-10-10 for editions. The pre-editions `cover.jpg` is adopted
  by the main edition. An edition is invalidated only when its cover inputs change (chosen cover,
  platforms of its copies, store links), not when a disc or key is added: that spares the quota. See
  [[data-model]].
