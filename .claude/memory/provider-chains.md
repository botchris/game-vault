---
name: provider-chains
description: Plex-style provider chains, TheGamesDB quota rules, the fallback pass and when to bump coverLogicChanged
metadata:
  type: project
---
- Chains for covers, barcodes and metadata. Default order = each provider's `DefaultOrder` (ties
  broken by registration order in `cmd/gamevault/plugins.go`); new providers are appended to
  existing users' chains, which the user reorders on the Providers page.
- **Metadata default (2026-10-11):** Steam (10) → Wikipedia (12) → IGDB (15) → TheGamesDB (20).
  First provider wins each field, so Wikipedia's summary (UI language, else English) beats IGDB's
  English one; its `SummarySource` credit travels with the summary in `GameDetails.fill`. YouTube
  trailers are deduplicated by id. `detailsLogicVersion` is 2 (sparse sheets refetched once).
  See [[wikipedia]], [[igdb]].
- **IGDB covers (2026-10-11):** per game, not per system: `Applies` = `q.ForPC() || q.Fallback`,
  `DefaultOrder` 85 (after TheGamesDB 10 and every store, Xbox 80) so store art wins on PC.
  `coverLogicChanged` was **not** bumped for it: enabling or reordering a cover provider already
  retries the "no cover" markers, and a bump would spend TheGamesDB's quota on every miss. Bump it
  only when the lookup logic itself changes.
- Media providers can keep learned state in `schema.FieldState` settings: they write it into the
  settings map they get, and the media service persists it with `provider.UpdateState` (wired for
  barcode lookups only so far; CeX's `country_hits`, see [[cex]]).
- TheGamesDB has a monthly quota (~1,000 requests): it only applies to physical copies, games no
  store knows (`CoverQuery.HasStoreLink`, which is just `len(Links) > 0`: any store link counts,
  so a new store needs no change there), and on the fallback pass when no store had art. See
  [[thegamesdb]].
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
- **Details fallback pass (2026-10-10):** game sheets have a fallback pass too. When every provider
  asked answered and the sheet is sparse (no summary and no screenshots), `fetchDetails` runs the
  chain again with `Fallback` set, asking only providers not asked yet, to fill the gaps (a PS4-only
  library game gets TheGamesDB's sheet). Rate limits in either pass keep the sheet uncached. The
  background scanner skips the pass (quota); it and a provider error in either pass mark the sparse
  sheet `FallbackSkipped`, so opening it runs the passes again. Increment `detailsLogicVersion` in
  `media/details.go` when sheet gathering improves: sparse sheets with an older `LogicVersion` are
  refetched once (a version, not a date, so sheets an old server caches later are caught too). TheGamesDB searches covers and sheets on the copies'
  systems (`game.SystemOf`: "PlayStation Store" → PS4).
