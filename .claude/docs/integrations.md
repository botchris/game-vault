# Adding integrations

Game Vault talks to three kinds of external systems. Each lives in its own package under
`internal/adapters/outbound/<name>/` and is registered in `cmd/gamevault/main.go`.

| Kind | Port | Examples | Shows up in |
| --- | --- | --- | --- |
| Source (an account that is scanned) | `sync.Provider` | humble, steam, epic, gog, battlenet, eaapp, ubisoft, xbox, playstation | Sources page |
| Cover provider | `media.CoverProvider` | steam (store), thegamesdb, epic, gog, ubisoft, eaapp, battlenet, xbox | Providers → Covers |
| Metadata provider (game sheet) | `media.MetadataProvider` | steam (details), thegamesdb (details) | Providers → Game details |
| Barcode database | `media.BarcodeProvider` | upcitemdb, eansearch, ebay | Providers → Barcodes |

Probing endpoints with `curl` from the host is fine (it is not part of the build). Before writing
code, research the service: what open-source tools (Heroic, Legendary, Playnite,
Lutris, psn-api…) use, then **probe the real endpoints with bogus credentials** from the shell.
A clean "invalid code" / "not authenticated" answer proves the endpoint, client id and request
shape are right without needing the user's account. GraphQL APIs often validate the query before
authenticating: use that to discover field names. Record what you learned in the store's
file under `.claude/memory/` (create `<store>.md` and add a line to `.claude/MEMORY.md`).

## A new source

1. `internal/adapters/outbound/<name>/provider.go`:
   - `const Type source.Type = "<name>"` and a `Platform` constant reusing an existing platform
     name when the store already appears in Humble keys (see `humble/mapper.go`) — this is what
     flags redundant keys.
   - `Descriptor()` with the settings `Fields`. Secrets are `source.FieldSecret`; values the
     provider rotates itself (refresh tokens, renewed cookies) are `source.FieldState` (never sent
     to or accepted from the browser). Point `HelpURL` at the sign-in page the user must open.
   - `Fetch(ctx, settings)` returns `[]game.ImportedCopy` with a stable, prefixed `ExternalID`
     (`"<name>:<id>"`), the title, and `CopyDetails{Kind, Platform, Status, Origin}`. Never change
     the ExternalID format later: it would duplicate copies.
   - Only import what the user owns: skip subscriptions (Game Pass, PS Plus, EA Play…), trials,
     DLC/add-ons, revoked entitlements. Decode leniently: a missing field must not drop a game.
   - `Test(ctx, settings) error` checks the settings for "Test connection" as cheaply as possible
     (e.g. list orders without downloading them). Nil means it works; the error says what failed
     and what to do. No counts or other details on success.
   - Fields with base URLs (`AuthURL`, `LibraryURL`…) so tests can use `httptest`.
2. Optional ports, as needed:
   - `sync.Preparer` — turn a one-time code into a stored session when the source is saved. Cache
     the exchanged session by `sha256(code)` for ~15 min: "Test" and "Save" both call it.
   - Rotating credentials: write the new value into the settings map passed to `Fetch`/`Test`;
     the service persists `FieldState` changes. If you also cache in memory, still hand the
     latest value back on every call (the Ubisoft bug of 2026-10-08).
   - `sync.KeepAliver` — for sessions copied from a browser that expire when idle. Keep the
     interval generous; the service adds jitter.
   - Browser cookies: use `internal/adapters/outbound/browsersession` (parsing pasted cookies,
     a cookie jar for the site's domain, remembering renewed cookies).
3. Errors: sentinel errors with the fix in the message (`ErrSignedOut = errors.New("… sign in again
   on … and paste …")`); wrap the service's own message for anything unexpected
   (`fmt.Errorf("<name> library: %s", msg)`) so the user can paste it back to you.
4. `provider_test.go` against a fake server: happy path with pagination, filtering (subscription,
   DLC…), rotation persisted across a "restart" (a new Provider value), and the signed-out / bad
   code errors.
5. Register in `main.go` (`sync.NewService(…, <name>.NewProvider())`).
6. Translations in `en.json` and `es.json`: `sources.<name>.description`, field labels and help.
   Help text uses one step per line (`1. …`) and backticks for things to copy; it is rendered by
   `web/src/components/FieldHelp.tsx`. The link button above the steps opens `HelpURL`.
7. If the store has art, add `"<name>:"` to `CoverQuery.HasStoreLink` and a cover provider.
8. Docs: a bullet in "Sources" of `docs/technical.md` (how it connects, what is imported), and the
   store added to the "Supported platforms" table of `README.md`.

## A new cover provider

1. `covers.go` in the store's package (or a new package): `CoverProviderID`, `Descriptor()` with
   `Kind: provider.KindCover`, `EnabledByDefault: true` when it is free.
2. `Applies(q)` must not touch the network: decide from `q.ExternalIDsWithPrefix("<name>:")`,
   `q.Links["<store>"]` (the game's link to a store, e.g. `game.LinkSteam`), `q.Platforms`,
   `q.PhysicalPlatforms`. Quota-limited providers also check
   `q.HasStoreLink()` / `q.Fallback`.
3. `Covers(ctx, q, settings)` returns candidates best first (portrait art before landscape), each
   with `URL`, `ThumbURL`, a `Label` saying where it comes from, and `Provider`. Return `nil, nil`
   when the game is unknown; errors only for real failures.
4. `ImageHosts()` (`media.ImageHoster`) lists the image hosts so the browser can show candidates
   through `/media/proxy`.
5. Register in `main.go` in `media.Providers.Covers` (order = default chain order for new users).
6. If the store's catalog can be searched by title, implement `media.LinkSearcher` (`LinkStore()`,
   `SearchLinks`): the game page then offers it under "Store links", and the add-on cover lookup
   searches it too. A source that knows a game's id in a store sets `ImportedCopy.Links`.
6. Bump `coverLogicChanged` in `internal/application/media/service.go` to the current UTC time so
   games previously marked "no cover" are retried.
7. Translation `providers.<id>.description` (en/es), test against a fake server, and the covers
   section of `docs/technical.md`.

## A new metadata provider

Implement `media.MetadataProvider` (`Applies`, `Details` returning `*media.GameDetails` in the UI
language). Images and videos in the result are downloaded into the game's folder by
`media/assets.go`; declare their hosts with `ImageHoster`. If it shares credentials with a cover
provider, set the same `SettingsGroup` in both descriptors.

## Checklist before telling the user it is ready

- [ ] `task lint` and `task test` (golangci-lint, vet, Go tests, TS types, translations); Go written
  following the `write-go` skill (GIVEN-WHEN-THEN tests with testify against a fake server)
- [ ] `task test-server`, dialog checked in the browser pane, "Test connection" with bogus
      credentials shows the clean error from the real service; `task test-server:stop`
- [ ] `docs/technical.md` (and the README platforms table for a new store) updated; the store's `.claude/memory/<store>.md` written and indexed in
      `.claude/MEMORY.md`; `memory/status-integrations.md` says it awaits the user's real test
- [ ] Tell the user exactly what was verified for real and what only against fakes, and which
      error to paste back if the first real test fails
