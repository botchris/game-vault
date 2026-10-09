# Writing a plugin

Everything Game Vault knows about an external service lives in one **plugin**: a Go package under
`internal/adapters/outbound/<name>/` that exports a `Plugin()` function. A plugin can bring any of
these pieces:

| Piece | Interface | What it does | Shows up in |
| --- | --- | --- | --- |
| Source | `sync.Provider` | Imports what an account owns (a library, keys) | Sources |
| Cover provider | `media.CoverProvider` | Finds box art for a game | Providers → Covers |
| Metadata provider | `media.MetadataProvider` | Fills the game sheet (summary, genres, trailers…) | Providers → Game details |
| Barcode provider | `media.BarcodeProvider` | Names the game behind a barcode | Providers → Barcodes |

The Steam plugin has a source, a cover provider and a metadata provider; Fanatical only a source;
TheGamesDB a cover and a metadata provider; CeX only a barcode provider.

The pieces of a plugin are independent at run time. They meet through the game's **links**: a source
links every game it imports to its store (`{"steam": "620"}`), and the store's cover provider reads
that link. So Steam's covers also work for a Humble key that knows its Steam AppID, or for a game the
user linked by hand, without any Steam account.

Plugins are compiled in. To add one, write its package and add one line to
[`cmd/gamevault/plugins.go`](../cmd/gamevault/plugins.go).

## The shared code

| Package | Use it for |
| --- | --- |
| `internal/application/plugin` | The `Plugin` type your package returns, and the registry that checks ids are unique |
| `internal/adapters/outbound/apiclient` | Calling a JSON API: base URL, User-Agent, headers, size limit, `IsStatus(err, 401, 403)` |
| `internal/adapters/outbound/browsersession` | Reusing cookies the user copies from their browser, keeping the ones the site renews |
| `internal/application/plugin/plugintest` | The completeness check every registered plugin passes (run by `task test`) |
| `internal/adapters/outbound/example` | A complete plugin to copy (see below) |
| `internal/domain/game` | `ImportedCopy`, `CopyDetails`, `Links`, `Store`: what a source returns |
| `internal/domain/schema` | Settings fields: text, secret, state (rotating credentials), consent |

## Start from the example plugin

[`internal/adapters/outbound/example`](../internal/adapters/outbound/example) is a complete plugin
for an imaginary store. It compiles, is tested like the real ones and passes the same checks, but is
not registered, so it never shows up in Game Vault. It has the two pieces most store plugins have:

| File | What it shows |
| --- | --- |
| `plugin.go` | `Plugin()`, and the `LinkedStore` the source and the cover provider share |
| `source.go` | A source: a token setting, `apiclient`, `Test`, `Fetch`, sentinel errors, skipping what is not an owned game, linking each game to the store |
| `covers.go` | A cover provider: `DefaultOrder`, `Applies` from the game's link, candidates best first, "unknown game" as no candidates, image hosts |
| `plugin_test.go` | A fake store with its failures, GIVEN / WHEN / THEN tests, and `plugintest` |

To write a new plugin:

1. Copy the package to `internal/adapters/outbound/<name>/` and rename it.
2. Replace the API calls and the answer shapes with the real service's. Keep a real answer, with
   its values changed, in the test's fake server.
3. Add its texts (`sources.<name>.…`, `providers.<id>.description`) to
   `web/src/i18n/locales/en.json` and `es.json`. Help texts use one step per line (`1. …`).
4. Add `<name>.Plugin(),` to [`cmd/gamevault/plugins.go`](../cmd/gamevault/plugins.go). From then
   on `task test` checks it against the UI's translation files.
5. Delete the pieces it does not need: a key seller has no covers, a game database no source.

The Fanatical plugin is a real, small one: one source, `apiclient`, a fake-server test.

## Rules for sources

- **`ExternalID`** is `"<name>:<id>"` and never changes format: a scan matches copies by it, so a
  new format duplicates every copy. If it must change, set `PreviousExternalID` on the copies.
- **Only import what the account owns**: skip subscriptions (Game Pass, PS Plus…), trials, and
  what is not a game (DLC, software, vouchers), counting the skipped items in a warning.
- **Withdraw** what you imported by mistake: return the item with `Withdrawn: true` and the copy
  is removed on the next scan.
- **Reuse platform names** (`"Steam"`, `"GOG"`, `"Epic Games"`… see `game.IsKnownPlatform`): keys
  are flagged as already owned by comparing them with the library of the same platform.
- **Link** every copy to the store it comes from (`Links`), and implement `sync.StoreLinker`.
  Sources that sell keys for other stores (Humble, Fanatical) link only what they know (Humble
  gives Steam AppIDs).
- **`Test(ctx, settings) error`** makes one cheap request to the real service. Nil means the
  settings work; the error says what is wrong and what to do.
- **Errors the user sees** say what happened and how to fix it ("… paste a new token"), as
  sentinel errors (`ErrSignedOut`) so callers can match them with `errors.Is`.
- **Read only.** Never reveal, redeem, buy or change anything on the user's account.
- **Settings**: secrets are `schema.FieldSecret`; credentials the plugin renews itself (refresh
  tokens, rotating cookies) are `schema.FieldState`, written back into the settings map so the
  service saves them; a service whose terms forbid this kind of access needs a
  `schema.FieldConsent` the user ticks, and the maintainers' agreement first.
- Optional interfaces, implemented only when needed: `sync.Preparer` (turn a one-time code into
  a stored session on save) and `sync.KeepAliver` (renew a browser session that expires when idle).

### Letting the browser extension fill a credential

Give the credential field a `SignIn` recipe (`schema.SignInRecipe`) so the Game Vault Connector
extension can fill it from the store's own sign-in: what to open, and one capture (`Cookie`,
`Cookies`, `Storage`, `Redirect` or `Fetch`). Cookies and storage can exist before the user signs
in, so give those a readiness condition (`When`: the signed-in page's `URLPrefix`, a value that
`Contains` something, or a `Fetch` that only answers when signed in); a storage `Path` reached only
after sign-in also counts. A `Redirect` takes no `When`, and a `Private` recipe cannot fetch.
Prefixes match only up to a `/`, `?` or `#`. List any other host the capture reads in `Hosts`. `plugintest` validates
every recipe. A capture the five primitives cannot express needs a new recipe version and an
extension update: discuss it first.

## Rules for media providers

- `Descriptor()` sets the provider's `ID`, `Kind`, `Name`, `DescriptionKey` and `DefaultOrder`:
  its place in the chain for new installs (lower runs first). Look at the others' values and pick
  a number between them; the user can reorder the chain afterwards.
- `Applies(q)` decides from the query alone, without network: `q.Links[LinkedStore.Key]`,
  `q.Platforms`, `q.PhysicalPlatforms`. A provider with a monthly quota also checks
  `q.HasStoreLink()` and `q.Fallback` so it is not spent on games stores already cover.
- Return `nil, nil` when the service does not know the game; an error only for real failures.
- `ImageHosts()` (`media.ImageHoster`) lists the hosts of the images it returns: the browser only
  loads them through Game Vault's image proxy, which refuses other hosts.
- A provider that reads a store's links implements `media.StoreLinker`; if it can also search the
  store's catalog by title, `media.LinkSearcher`, and the game page offers that search.
- Providers of one service that share a key set the same `SettingsGroup`.
- A dependency on another plugin's piece is a parameter of `Plugin()` (Battle.net's covers receive
  the Steam store), so it is visible in `plugins.go` and checked by the compiler.

## Tests

Every plugin has a test against an `httptest` fake server that reproduces the real API's answers,
including its failures (expired session, rate limit, an HTML page instead of JSON). Point the
plugin at it through its `apiclient.Client` (`API.BaseURL`, `API.HTTP`). Write the tests as
GIVEN / WHEN / THEN subtests with testify, as the existing `provider_test.go` files do.

`task test` also runs `plugintest` on every plugin in `plugins.go`: ids and names set, every text
translated in every language, media providers in the right chain with a `DefaultOrder`, stores
named.

## Before opening a pull request

- `task lint` and `task test` pass (everything runs in the toolchain container: you only need
  Docker and Task). CI runs the same on the pull request.
- "Test connection" against the real service with wrong credentials gives a clean error, not a
  404 or a decoding error.
- `docs/technical.md` describes how the plugin connects and what it imports, and a new store is
  added to the platforms table of the README.
