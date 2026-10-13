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
| `internal/adapters/outbound/apiclient` | Every HTTP call to the service: base URL, User-Agent (`BrowserUserAgent` for cookie-based stores), headers, JSON, form (`Request.Form`, `BasicAuth`) and GraphQL (`GraphQL`, `IsGraphQLCode`) requests, a size limit, `Send` for the whole answer (a redirect's `Location` with `NoRedirects`, the page a session ended on), `IsStatus(err, 401, 403)`, and the passing-trouble helpers (`Transient`, `ClassifyError`, `Rejected`) |
| `internal/adapters/outbound/browsersession` | Reusing cookies the user copies from their browser, keeping the ones the site renews; its client becomes the `apiclient.Client`'s `HTTP` |
| `internal/adapters/outbound/oauthsession` | A source that signs in with a one-time code: `Prepare`, the rotating session, access tokens, `WithRetry` after a rejected token |
| `internal/adapters/outbound/oauthapp` | An application token (OAuth client credentials) shared by a provider's requests, renewed once when refused |
| `internal/adapters/outbound/ratelimit` | Pacing a service's requests: a gap, a burst, a maximum wait and queue, a back-off after a 429 |
| `internal/application/unattended` | The jitter and the waits of a background loop (requests nobody asked for at that moment) |
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
| `source.go` | A source: a token setting, `apiclient`, `Test`, `Fetch`, sentinel errors, skipping what is not an owned game, linking each game to the store, and counting what it skips in the log (`source.Skipped`, `WithLogger`). `ImportedCopy.System` stays empty: a PC store's platform already says PC, and only a store that says more than its platform sets it (PlayStation, which names the console) |
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
  An item the store gives no id gets one from its title with `game.TitleIDKey` (frozen), never
  `game.MatchKey`, which changes as matching improves (Battle.net, EA and CSV rows without
  `externalId` do this).
- **Only import what the account owns**: skip subscriptions (Game Pass, PS Plus…), trials, and
  what is not a game (DLC, software, vouchers), counting the skipped items with a
  `source.Skipped` and writing it to the logger the plugin receives (`Plugin(log)`, the
  provider's `WithLogger`): one line per reason with its count. These skips happen on every scan,
  so they are never warnings. An item without a store id whose title gives no `game.TitleIDKey`
  either (a title with no Latin letters or digits) is skipped too, but the user loses a game
  there, so that count is a warning (`Skipped.Warnings`): every such item would share one id.
- **Withdraw** what you imported by mistake: return the item with `Withdrawn: true` and the copy
  is removed on the next scan.
- **Use the domain's names**: platforms, systems and link keys are constants in
  `internal/domain/game/platforms.go` (`game.PlatformSteam`, `game.SystemPS4`, `game.LinkGOG`…),
  never string literals. Keys are flagged as already owned by comparing their platform with the
  library of the same platform exactly, and the names are stored in documents, CSV files and file
  names. A new store or console is one line in that file's table, plus its constant.
  `TestVocabulary_adaptersUseTheDomainNames` (cmd/gamevault) fails on a platform or system written
  as a literal; a value of an external API that happens to look like one ("PS5" in Sony's answers)
  goes in its `rawVocabulary` list with the reason.
- **Link** every copy to the store it comes from (`Links`), and implement `sync.StoreLinker`.
  Sources that sell keys for other stores (Humble, Fanatical) link only what they know (Humble
  gives Steam AppIDs).
- **`Test(ctx, settings) error`** makes one cheap request to the real service, never a scan: give
  the paging helper a page limit and send exactly the request of a scan's first page, with the
  scan's own page size, then stop. Never a request shape the scan does not send (a page of one
  item): the service may refuse what its own client never asks. It still signs in the way a scan does, so a
  rotated credential is handed back into the settings. A service that answers everything in one
  request (Fanatical) is read whole. Nil means the settings work; the error says what is wrong
  and what to do.
- **Errors the user sees** say what happened and how to fix it ("… paste a new token"), as
  sentinel errors so callers can match them with `errors.Is`. Every source uses the same names
  for the same meaning: `ErrNoCredentials` (the credential setting is empty), `ErrBadCode` (the
  store rejected a one-time code) and `ErrSignedOut` (below); other sentinels name what happened
  (`ErrPrivateProfile`, `ErrProfileNotFound`). Give each one its kind with `errkind.New`
  (`errkind.Invalid` for input to fix): the API answers by kind, and an error without one is
  reported as a failure of Game Vault. `task test` fails for an exported sentinel made with a
  plain `errors.New`, and for an error type with its own `Is` method but no `Kind` method. A source's texts name the store (they also show in the
  store's own dialog). A store that no longer accepts the
  saved session or key returns `source.SignedOut("… sign in again …")`, which matches
  `source.ErrSignedOut`: System → Health then shows it as an error that says to sign in again.
  Only a real refusal is one: a token endpoint's 400 (`invalid_grant`), 401 or 403
  (`apiclient.Rejected`), and an access token refused even after one renewal (renew a rejected
  access token once before giving up). A 429, a 5xx or no answer is a passing trouble
  (`apiclient.ClassifyStatus` / `ClassifyError`), never "sign in again".
- **Read only.** Never reveal, redeem, buy or change anything on the user's account.
- **Settings**: secrets are `schema.FieldSecret`; credentials the plugin renews itself (refresh
  tokens, rotating cookies) are `schema.FieldState`, written back into the settings map so the
  service saves them; a service whose terms forbid this kind of access needs a
  `schema.FieldConsent` the user ticks, and the maintainers' agreement first.
- Optional interfaces, implemented only when needed: `sync.Preparer` (turn a one-time code into
  a stored session on save) and `sync.KeepAliver` (renew a browser session that expires when idle).

### The HTTP plumbing

Do not write requests by hand: every source calls its service through an `apiclient.Client` (one
per host), so the body limit, the User-Agent, errors without secrets and the status rules are the
same everywhere, and tests point it at a fake server through `BaseURL` and `HTTP`.

- **A one-time code** (OAuth authorization code, a pasted redirect address): build an
  `oauthsession.Sessions[yourSession]` with your `Exchange`, `Refresh` and the session's shape,
  and let `Prepare` and `WithRetry` do the rest. Never cache tokens or codes yourself: it hands the
  newest rotated session back into the settings and never sends a spent refresh token again.
- **A rejected access token**: return an error wrapping `apiclient.ErrTokenRejected` (usually on a
  401); `WithRetry` renews it once and only a second refusal signs the user out.
- **GraphQL**: `Client.GraphQL` (or `Response.GraphQL` for a persisted query sent by GET) checks
  the status before the body; match codes with `apiclient.IsGraphQLCode(err, "UNAUTHENTICATED")`.
- **Background requests** (a keep-alive, a scheduled scan): draw every pause with
  `unattended.Rhythm`, never a fixed interval.

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
  `q.Platforms`, `q.PhysicalPlatforms`. `q.System` is the edition's system (`""` when the query
  names none): a store provider applies only when `q.ForPC()`; a console catalog checks the system
  (see `game.IsXboxSystem`). A title search filters to `q.SearchSystems()` and searches the terms
  of `game.SearchTerms(q.Title)` in order (the game's name, then the raw title); a table mapping
  systems to the service's own ids is keyed by the `game.System…` constants and checked against
  `game.Consoles()` in a test. A provider with a monthly quota also checks
  `q.HasStoreLink()` and `q.Fallback` so it is not spent on games stores already cover.
- Return `nil, nil` when the service does not know the game; an error only for real failures.
- **Error texts do not name the provider**: the media service puts the provider's name before
  them in warnings ("TheGamesDB: …") and the status badges and Health show it next to them, so
  "IGDB is not answering" would read "IGDB: IGDB is not answering". Name another service only
  when the trouble is that one's (IGDB's sign-in through Twitch). Problems of the provider rather
  than of a game wrap `provider.ErrRateLimited`, `ErrQuotaExhausted`, `ErrUnavailable` or come from
  `provider.BadSettings`, which carry their kind already.
- `ImageHosts()` (`media.ImageHoster`) lists the hosts of the images it returns: the browser only
  loads them through Game Vault's image proxy, which refuses other hosts.
- A provider that reads a store's links implements `media.StoreLinker`; if it can also search the
  store's catalog by title, `media.LinkSearcher`, and the game page offers that search.
- Providers of one service that share a key set the same `SettingsGroup`.
- A dependency on another plugin's piece is a parameter of `Plugin()` (Battle.net's covers receive
  the Steam store), so it is visible in `plugins.go` and checked by the compiler.
- A service with a rate limit, a quota or terms that ask for restraint paces its requests with one
  `ratelimit.Throttle` per service (shared by its cover and details providers), configured from its
  documented limits; a caller that would wait too long gets `ratelimit.ErrBusy`, a kind of
  `provider.ErrRateLimited`, so nothing is remembered as missing.
- An application token (client credentials: a Client ID and Secret) lives in an
  `oauthapp.Tokens`, keyed by `oauthapp.Key(id, secret)`, used through `WithRetry`.

## Tests

Every plugin has a test against an `httptest` fake server that reproduces the real API's answers,
including its failures: a refused session or key, a rate limit, an outage, no answer and an HTML
page or a truncated answer instead of JSON. `apiclienttest.Failures()` serves all but the first,
with the sentinel each must match, and `apiclienttest.FailOn` makes one endpoint of your fake
fail while sign-in keeps working (see any `failures_test.go`). Point the plugin at the fake through
its `apiclient.Client` (`API.BaseURL`, `API.HTTP`). Write the tests as GIVEN / WHEN / THEN subtests
with testify: the example, Amazon, itch.io and Fanatical are the ones to copy; some older tests are
still flat functions.

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
