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
| `internal/domain/game` | `ImportedCopy`, `CopyDetails`, `Links`, `Store`: what a source returns |
| `internal/domain/schema` | Settings fields: text, secret, state (rotating credentials), consent |

The Fanatical plugin (`internal/adapters/outbound/fanatical`) is the smallest complete example:
one source, `apiclient`, a fake-server test.

## A minimal plugin

```go
// Package mystore imports the library of MyStore accounts.
package mystore

// Type is the source type id of MyStore accounts.
const Type source.Type = "mystore"

// LinkedStore is the store this plugin links games to.
var LinkedStore = game.Store{Key: "mystore", Name: "MyStore", PageURL: "https://mystore.example/game/{id}"}

// Plugin is the MyStore plugin: the library of MyStore accounts.
func Plugin() plugin.Plugin {
	return plugin.Plugin{ID: "mystore", Name: "MyStore", Sources: []sync.Provider{NewProvider()}}
}

// Provider implements sync.Provider for MyStore accounts.
type Provider struct {
	// API calls MyStore; tests point it at a fake server.
	API *apiclient.Client
}

// NewProvider returns the MyStore source with its production endpoints.
func NewProvider() *Provider { return &Provider{API: apiclient.New("https://api.mystore.example")} }

// LinkStore implements sync.StoreLinker.
func (p *Provider) LinkStore() game.Store { return LinkedStore }

// Descriptor implements sync.Provider.
func (p *Provider) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: Type, Name: "MyStore", DescriptionKey: "sources.mystore.description",
		Fields: []source.Field{{
			Key: "token", LabelKey: "sources.mystore.token", HelpKey: "sources.mystore.tokenHelp",
			HelpURL: "https://mystore.example/account", Kind: schema.FieldSecret, Required: true,
		}},
	}
}

// Test implements sync.Provider with the cheapest request that proves the token works.
func (p *Provider) Test(ctx context.Context, s source.Settings) error {
	_, err := p.library(ctx, s, 1)
	return err
}

// Fetch implements sync.Provider.
func (p *Provider) Fetch(ctx context.Context, s source.Settings) ([]game.ImportedCopy, []string, error) {
	items, err := p.library(ctx, s, 0)
	if err != nil {
		return nil, nil, err
	}

	copies := make([]game.ImportedCopy, 0, len(items))
	for _, it := range items {
		copies = append(copies, game.ImportedCopy{
			ExternalID: "mystore:" + it.ID, // stable forever: changing it duplicates copies
			Title:      it.Name,
			Links:      game.Links{LinkedStore.Key: it.ID},
			Details:    game.CopyDetails{Kind: game.KindLibrary, Platform: "MyStore", Status: game.StatusOwned},
		})
	}

	return copies, nil, nil
}

// ErrSignedOut means MyStore rejected the token.
var ErrSignedOut = errors.New("mystore did not accept the token: create a new one on mystore.example and paste it")

func (p *Provider) library(ctx context.Context, s source.Settings, limit int) ([]item, error) {
	var out struct{ Games []item `json:"games"` }

	err := p.API.Do(ctx, apiclient.Request{
		Path: "/v1/library", Query: url.Values{"limit": {strconv.Itoa(limit)}},
		Header: http.Header{"Authorization": {"Bearer " + s["token"]}},
	}, &out)
	if apiclient.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden) {
		return nil, ErrSignedOut
	}

	return out.Games, err
}
```

Then, in `cmd/gamevault/plugins.go`, add `mystore.Plugin(),` to the list, and add the texts
(`sources.mystore.description`, `sources.mystore.token`, `sources.mystore.tokenHelp`) to
`web/src/i18n/locales/en.json` and `es.json`. `task test` fails until every text exists in both.

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
  Docker and Task).
- "Test connection" against the real service with wrong credentials gives a clean error, not a
  404 or a decoding error.
- `docs/technical.md` describes how the plugin connects and what it imports, and a new store is
  added to the platforms table of the README.
