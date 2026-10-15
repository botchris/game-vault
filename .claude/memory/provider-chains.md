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
- **Fallback for every game (2026-10-11, maintainer's decision):** the cover fallback pass runs
  whenever the first pass found nothing, not only for games with store links; the providers asked in
  the first pass are skipped, so TheGamesDB (first pass for link-less games) spends nothing more.
  The Choose cover dialog always asks the fallback providers too (since 2026-10-12). On
  that pass: **Steam** searches its store by title for PC editions without an AppID (only exact
  `game.MatchKey`, one app; never stored as a link; 429 → rate limit) and **IGDB** (DefaultOrder 85,
  `Applies` = `q.Fallback`, starts disabled (needs credentials), silent without them) gives the
  game's cover for any system. Review round (2026-10-11): providers' "no cover" answers in a
  limited lookup are kept 24 h in memory per edition (`media/misses.go`); add-ons borrow the base
  game's cover before the fallback pass; configuring a sibling of a cover provider retries missing
  covers; Steam's store host (search, appdetails) is throttled (burst 10, 1.5 s, 5 s max wait; art
  lookups on the Web API are not) with a 10-min search cache; 5xx/no answer from Steam's search, Twitch or IGDB count as rate limits. `coverLogicChanged` bumped to 2026-10-11 09:20 UTC for it (an earlier bump for
  IGDB alone was reverted: enabling a provider already forgets the markers).
- Media providers can keep learned state in `schema.FieldState` settings: they write it into the
  settings map they get, and the providers registry persists it (`Registry.SaveState`, wired for
  barcode lookups only so far; CeX's `country_hits`, see [[cex]]).
- TheGamesDB has a monthly quota (~1,000 requests): it only applies to physical copies, games no
  store knows (`GameQuery.HasStoreLink`, which is just `len(Links) > 0`: any store link counts,
  so a new store needs no change there), and on the fallback pass when no store had art. See
  [[thegamesdb]].
- When cover resolution improves, bump `coverLogicChanged` in
  `internal/application/media/covers.go` to the current UTC time (never in the future) so old
  "no cover" markers are retried.
- Add-ons (DLC, soundtracks…) borrow the base game's cover for the same system (`media/addon.go`).
- **Editions (2026-10-10):** covers are per (game, system) (`GameQuery.System`, `QueryFor(g, system)`).
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
- **Edition titles (2026-10-11, bug seen live):** an edition title ("Example West GOTY") found nothing on IGDB
  or Wikidata, silently. Searches now use `game.SearchTitle` (MatchKey's tags removed, casing kept),
  then the raw title if no match; `GameDetails.NotFoundBy` lists providers asked that found no game
  (footer "No clear match on …": no result or ambiguous; cached, not a warning; limited providers
  are not listed). Raw-title fallback compares after `game.NormalizeTitle` (™ alone is no other
  title); Steam's cover search has the fallback too.
- **Provider status (2026-10-11, maintainer's request):** provider-level errors (ErrRateLimited incl.
  quota/busy/ErrUnavailable, and ErrBadSettings) never reach game sheets, the cover picker, scan
  suggestions or price warnings (valuation reports through `ReportResult`, 2026-10-13); the
  providers registry keeps the last one per settings group in memory (`providers/status.go`, cleared by a
  success, new settings or its Until) and `GetProviderStatus` feeds the System page section and the
  Providers page badges. TheGamesDB's allowance error implements `provider.Resumer`. Review
  fixes: providers without settings must return ErrBadSettings, never an empty success (it would
  clear a sibling's status); TheGamesDB 401/403/5xx/no answer and Wikimedia 403/5xx/no answer are
  provider-level; TestProvider sets/clears too; the picker gets `providers_unavailable`.
  Since 2026-10-13 (audit PRV-2) every other provider classifies through `apiclient.ClassifyError`
  (a whole request's error) or `ClassifyStatus` (a bare status): 429 → ErrRateLimited, 5xx / no
  answer / HTML page → ErrUnavailable; 401/403 stay each adapter's call (keyless APIs answer bot
  checks with 403). Use them in new providers rather than `fmt.Errorf("HTTP %d")`.
- **PC box as last resort (2026-10-11, maintainer's request):** a console-only game with a PC-store
  link (`game.IsPCStoreLink`) has a virtual PC edition at `/media/covers/{id}/PC`: first pass only
  (store links only, `GameQuery.LinksOnly`: no TheGamesDB, IGDB, Battle.net/Ubisoft/Steam title searches, no fallback pass, no add-on borrowing; an xbox link alone does not count with an Xbox edition), cached/marked under PC, fingerprinted by links; the web appends
  PC last in `coverOrder` when `has_pc_store_link` and no PC edition, labelled "PC box".
- **Health (2026-10-11, maintainer's request):** System → Health replaces the provider-status
  section; `GetProviderStatus` is gone (status now in `Provider.status` from ListProviders). Checks:
  providers (settings = error), sources (`source.ErrSignedOut` = error, failed scan / keep-alive =
  warning), backups, disk. New checks implement `health.Check` and must stay cheap (no network).
- **SteamGridDB (2026-10-11, maintainer's decision):** cover provider after IGDB (DefaultOrder 90),
  fallback pass only, never `LinksOnly`, disabled by default behind a risk consent and the user's
  key; `coverLogicChanged` not bumped (enabling a provider already forgets the misses). See
  [[steamgriddb]].
- **Picker and exact titles (2026-10-12, maintainer's ruling):** Choose cover always asks the
  fallback-pass providers too (one request each per opening; first-pass candidates first, then the
  fallback ones, each in chain order; cached per edition while the dialog is open); automatic
  resolution unchanged. Exact title matches (Steam incl. FindLinks, IGDB, Wikipedia, SteamGridDB,
  TheGamesDB) use `game.SameTitle`: word lists compared, case/accents/punctuation ignored, "&" = "and"
  ("A - B" = "A: B", "Re-Volt" ≠ "Revolt", "F.E.A.R." ≠ "Fear"; empty never equal).
- 2026-10-13 (audit PRV-7): one helper each for the title terms (`game.SearchTerms`) and the systems
  (`GameQuery.SearchSystems()`) of every title search; TheGamesDB, which searched the raw title only,
  now searches the game's name first too (second request only on no match). `coverLogicChanged`
  bumped to 2026-10-13 10:50 UTC for it.
- 2026-10-13 (audit PR 5, APP-1): `media.Service` was split. Provider configuration and status live
  in `internal/application/providers` (`Registry`: configs read once into memory on first use of each
  kind, written through on change; `ReportResult`, `Statuses`, `Issues`); `media.New` wires `Covers`
  (resolveEdition = chosen → fromProviders → limited → add-on → rememberMissing), `Details`,
  `Assets`, `Identifier`, `Photos`, `AutoLinker`, `ImageProxy`; `media.NewStores` merges the link
  stores of providers and sources. `CoverQuery` is now `GameQuery`; add-on titles and retail product
  names are domain rules (`game.IsAddOnTitle`, `game.ParseProductName`).
- **Home page kinds (2026-10-15):** `deals` (IsThereAnyDeal, [[itad]]) and `releases` (IGDB
  `igdb-releases`) joined `provider.Kinds()`. They feed `internal/application/home`, which keeps one
  snapshot per block (`home_snapshots`), refreshed by an unattended job (deals 6 h, releases 24 h,
  ±10 %) or the user's Refresh (15-minute guard on `AttemptedAt`: failed and interrupted attempts
  count, across restarts). `provider.Kind.Scheduled()` (valuation, deals, releases) keeps a provider
  off when a sibling of its settings group is on. Owned deals are removed when the page is read,
  through the ownership lookup (Steam id or exact title; owned on PC hides, consoles only adds a note).
