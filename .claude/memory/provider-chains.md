---
name: provider-chains
description: Plex-style provider chains, TheGamesDB quota rules, the fallback pass and when to bump coverLogicChanged
metadata:
  type: project
---
- Chains for covers, barcodes and metadata. Default order = registration order in
  `cmd/gamevault/main.go`; new providers are appended to existing users' chains.
- TheGamesDB has a monthly quota (~1,000 requests): it only applies to physical copies, games no
  store knows (`CoverQuery.HasStoreLink`), and on the fallback pass when no store had art.
  Add a new store's prefix to `HasStoreLink` when it gets a cover provider. See [[thegamesdb]].
- When cover resolution improves, bump `coverLogicChanged` in
  `internal/application/media/service.go` to the current UTC time (never in the future) so old
  "no cover" markers are retried.
- Add-ons (DLC, soundtracks…) borrow the base game's cover (`media/addon.go`).
