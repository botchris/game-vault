# Home Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A "Home" page that opens on start and shows, at a glance, what needs attention (pending keys, Health), current deals from IsThereAnyDeal without the games the user owns, upcoming releases from IGDB (from the user's series and the most anticipated), and the collection in figures.

**Architecture:** Two new provider kinds, `deals` and `releases`, join `provider.Kinds()` and get the registry's configuration, status and Health for free. A new use case, `internal/application/home`, owns the ports (`home.DealsProvider`, `home.ReleasesProvider`, `home.Snapshots`), a jittered unattended refresh job (deals every 6 h, releases every 24 h, ±10 %) and a user-started refresh with a 15-minute guard; it stores one snapshot per block in SQLite (`0012_home_snapshot.sql`) and, when read, removes deals for games owned on PC and notes console-only ones. A new ITAD plugin implements `deals`; the IGDB plugin gains `releases`. `HomeService` (`GetHome`, `RefreshHome`) serves the external blocks; the web computes the pending strip and the collection figures from the data it already holds (AppData, Health, the collection value), with the library's own helpers, so the numbers always match the lists they link to.

**Tech Stack:** Go 1.26, ConnectRPC/buf, SQLite (pure Go); React 19 + TypeScript, i18next; Node test runner for pure web helpers.

**Spec:** `docs/superpowers/specs/2026-10-15-home-dashboard-design.md`

## Global Constraints

- Everything compiles, tests and generates in the toolchain container through Task: `task lint`, `task test`, `task generate`, `task lint:fix`, `task i18n`. One Go package: `task go -- test ./internal/application/home/ -run TestRefresh -v`. One web Node test: `docker run --rm -v "$PWD:/src" -w /src/web -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 tests/home.test.mjs`. The tag is the first 12 hex digits of the SHA-256 of `build/toolchain.Dockerfile` (`shasum -a 256 build/toolchain.Dockerfile | cut -c1-12`; it is `446777a3814f` today). Never use host Go, Node, npm or linters.
- Load the `write-go` skill before writing Go: a doc comment on every exported identifier and every interface method, one struct field per line, a blank line between documented members, functional options (`type Option func(*T)` for a package with one constructor), GIVEN/WHEN/THEN tests with `context.WithTimeout(t.Context(), 10*time.Second)` and testify, never a bare context passed to code under test. Test code below is written compactly: run `task lint:fix` (it puts each struct field on its own line and fixes cuddling) before `task lint`.
- `task lint` and `task test` pass at the end of every task.
- The repository is public: invented test data only (titles such as "Lantern Keep", "Echo Valley", "Harbor Lights", "Copper Coast"; Steam ids such as `4410`; ITAD ids such as `018d937f-0000-7000-8000-000000000001`), no personal data, accounts, library counts or results from the user's config copy in code, comments, docs, commits or the PR.
- English in the repository. Every UI string goes through `t()` with keys in `web/src/i18n/locales/en.json` and `es.json` (`task i18n`). English is the default language; Spanish is a translation (never Spanish-first).
- No UI text names or compares with other products. Integrated services are named: IsThereAnyDeal, IGDB, and the shops ITAD reports.
- Comments explain why, in full sentences, at the density of the surrounding code.
- Web files that Node tests import (`web/src/lib/home.ts`, `lib/route.ts`) use structural types only: no generated protobuf code, no `lib/model.ts`, imports with the `.ts` extension.
- Unattended requests are jittered with `internal/application/unattended.Rhythm` (deals 6 h, releases 24 h, each ±10 %; start-up refresh of an overdue block at a random moment 1–5 minutes after start). `-no-unattended` turns the home job off: then nothing calls ITAD or IGDB unless the user presses Refresh.
- ITAD's terms (2026-10-15): private personal use with the user's own key; never alter its data (prices, links with their affiliate tags); never compete with it (no price alerts, no waitlist of Game Vault's own); cache (no constant maxing of the limit); 1000 requests per 5 minutes, 429 with `Retry-After`; credit recommended ("Prices from IsThereAnyDeal" under the block).
- Privacy: ITAD receives no collection data (only deal-list requests with the country, and lookups of its own game ids). IGDB receives the Steam AppIDs of owned games to find their series (it already receives titles and store ids for sheets).
- Safety: never touch `config/` or the server on port 8080; check the UI with `task test-server` (port 8093, a copy of the config, `-no-unattended`). On that copy never scan sources whose credentials rotate (Ubisoft, Epic, GOG, Xbox, PlayStation, Battle.net, EA). External endpoints are probed only with bogus keys. Nothing is pushed, tagged or published.

## Review Focus

- **ITAD games without a Steam app id** (the lookup answers `null`, an empty list, or only `sub/…` packages): the deal must stay, with an empty Steam id, and be removed only when an owned game has exactly the same title; an empty id must never match owned games that have no Steam link. Tests: Task 4 ("AND a game ITAD knows no Steam app for keeps an empty Steam id"), Task 6 ("AND a deal without a Steam id is never matched through an empty id").
- **Titles in other scripts** (a Japanese title on ITAD or IGDB): `game.SameTitle` compares every script, so "星の灯台" removes only an owned "星の灯台", never an owned "星の港" or a Latin title. Test: Task 6 ("AND titles in another script match only the same title").
- **An empty collection** (a new install with a key entered first): the home page and both refreshes must work, deals stay unfiltered, IGDB is not asked for series (no request with an empty list), and the web shows zeros and "All caught up" rather than errors. Tests: Task 5 ("GIVEN no owned game has a Steam link"), Task 6 ("GIVEN an empty collection"), Task 9 ("an empty collection is all zeros, not an error").
- **A provider not configured, or disabled after data was stored**: the block says "not configured" and links to Providers (the stored data is hidden, not shown as current), the job sends nothing, and Refresh answers "enable a provider" instead of failing. Tests: Task 6 ("GIVEN deals stored, then ITAD turned off", "GIVEN no deals provider enabled"), Task 7 ("GIVEN no provider configured"), Task 8 ("WHEN the user refreshes releases": failed precondition).
- **A stale snapshot after a long downtime, or a server run with `-no-unattended`**: the page shows the last good data with its age as stale (older than twice the interval even without an error), releases whose date passed are dropped, an overdue block is refreshed 1–5 minutes after start, and reading the page never calls a provider. Tests: Task 6 ("GIVEN deals fetched three days ago, without an error since", "AND reading never calls a provider"), Task 7 ("GIVEN snapshots older than their interval").

## Rulings made while planning (where the spec is silent, ambiguous or inconsistent)

- **Pending and collection are computed in the web, not by `GetHome`.** The library's "Pending", "Expiring" and "Not needed" filters, the redeem-deadline rule (`lib/deadlines.ts`) and the play-status filter live in the web; the page already holds every game (AppData), the Health issues and the collection value. Counting them with the same helpers guarantees a chip says what the library shows when it is clicked, and "added this month" is counted in the user's time zone, which the server does not know. `GetHome` returns the two external blocks only.
- **"Keys that expire soon" uses the library's "Expiring" rule (30 days, `EXPIRING_DAYS`), not 7 days.** The chip links to that filter; a 7-day count next to a 30-day list would disagree. The chip names the nearest deadline ("Lantern Keep in 3 days"), which keeps the urgency visible.
- **The owned-deal filter runs when the page is read, not when deals are fetched.** The snapshot keeps ITAD's lists as they came (about 40 each); `GetHome` removes the owned ones and keeps 12. A game bought or scanned between two refreshes disappears at once.
- **Owned is matched by Steam app id or by exact title** (`game.SameTitle`, through `game.TitleKey`), not "title only when there is no Steam id": an owned GOG or disc copy has no Steam link, so its deal would otherwise stay. "Owned on PC" means a held copy (`game.Copy.Held`) whose `System()` is `PC`; a game held only on consoles stays with its systems ("You have it on PS4").
- **ITAD lists (probed 2026-10-15, documentation of the same day).** Featured = `GET /deals/v2` in ITAD's own order (no `sort`; the documentation says sort values are "same as in deals list on the website" and documents only `-cut` and `price`). Historical lows = `sort=-cut`, `limit=200`, kept locally when `flag` is `"H"` or the price is at or under `historyLow`. Only items of `type` `"game"` are kept; `mature=false`. The key goes in the `ITAD-API-Key` header, never in the URL. A bad key is HTTP **403** `{"status_code":403,"reason_phrase":"Invalid or expired api key"}` (not 401); no key is 403 `"Missing api key"`; unknown paths are 404 JSON.
- **Steam ids come from `POST /lookup/shop/61/id/v1`** (Steam is shop 61), in batches of 200 ITAD ids; the answer maps each id to `["app/4410", "sub/99"]` or `null`; the first `app/` wins.
- **IGDB series are resolved by Steam AppID, not by an IGDB id from the details cache**: the cache stores no provider ids. `OwnedRef` carries the game's id, title and store links; `Sequels` asks `external_games` (source 1, Steam) in batches of 500 for `game.franchise`, `game.franchises`, `game.collections`, then one `games` query per 500 series ids for upcoming entries. Owned games without a Steam link (discs, other stores) do not contribute series yet (see open questions).
- **Anticipated** = one `games` query: `first_release_date` within the next 60 days, `hypes > 0`, no editions or add-ons (`version_parent = null & parent_game = null`), `sort hypes desc`, 40 results. `hypes` is documented as "number of follows a game gets before release"; `follows` and `category`/`collection` are deprecated.
- **Scheduled kinds do not switch on with a sibling.** `registerNew` already keeps price providers off when their service's other providers are enabled; `deals` and `releases` run on a schedule too, so `provider.Kind.Scheduled()` names the three and IGDB releases stays off (with the shared Twitch keys) until the user turns it on.
- **Country:** ITAD's own optional setting (two letters), else derived from the preferred currency (a small table in `home`; EUR → DE, since euro-zone shops mostly charge one price), else US.
- **Block states:** `not_configured` (no enabled provider; stored data hidden), `never_fetched` (configured, nothing stored yet: a card with Refresh; the skeleton shows only while a request runs, so a `-no-unattended` server never shows an endless skeleton), `stale` (the last attempt failed, or the data is older than twice the block's interval), `fresh`.
- **Refresh is synchronous** and returns the whole home, so the page re-renders from one answer. The 15-minute guard counts failed attempts too and is read from the snapshot, so it survives a restart. A refresh is all or nothing per block: if the second list fails, the previous snapshot stays whole.
- **At read time**, releases whose date has passed are dropped, and so are "from your series" entries whose owned game no longer exists.
- **Links from ITAD and IGDB are used only when they are `https://`** (they become `href`s); images are shown through the image proxy, so ITAD's deals provider declares `assets.isthereanydeal.com` (`media.ImageHoster`).

## Files

| File | Responsibility |
|---|---|
| `internal/domain/provider/provider.go` | `KindDeals`, `KindReleases`, `Kind.Scheduled()` |
| `internal/domain/game/market.go` (new) | `game.Deal`, `game.Release` |
| `internal/application/providers/registry.go` | scheduled kinds stay off when a sibling is on |
| `internal/application/home/{ports,service,owned,scheduler,options}.go` (new) | ports, reading, refresh, owned filter, the job |
| `internal/application/plugin/plugin.go`, `plugintest/plugintest.go` | `Plugin.Deals`, `Plugin.Releases`, `Registry.Deals()`, `Registry.Releases()` |
| `internal/adapters/outbound/sqlite/{home_snapshots.go,migrations/0012_home_snapshot.sql}` (new) | snapshot table and repository |
| `internal/adapters/outbound/itad/{deals,plugin}.go` (new) | the ITAD plugin |
| `internal/adapters/outbound/igdb/releases.go` (new), `plugin.go`, `fake_test.go` | IGDB `releases` |
| `internal/app/app.go`, `cmd/gamevault/{main,plugins}.go` | wiring and the job |
| `proto/gamevault/v1/home.proto` (new), `internal/adapters/inbound/rpc/{home_handler,server}.go` | `HomeService` |
| `web/src/lib/home.ts` (new), `lib/route.ts` | pure helpers, home as the start page |
| `web/src/features/home/*` (new), `App.tsx`, `features/library/LibraryPage.tsx`, `features/providers/ProvidersPage.tsx`, `components/Icon.tsx`, `api/client.ts`, `styles.css`, `i18n/locales/{en,es}.json` | the page, its blocks, deep links, provider chains |
| `docs/technical.md`, `docs/plugins.md`, `README.md`, `.claude/memory/{itad,provider-chains,igdb,unattended-requests}.md`, `.claude/MEMORY.md`, `.claude/docs/ui.md` | documentation |

---

### Task 1: The deals and releases kinds, and the deal and release values

**Files:**
- Modify: `internal/domain/provider/provider.go` (the `Kind` constants, `Kinds()`, new `Kind.Scheduled`)
- Modify: `internal/domain/provider/kind_test.go`
- Create: `internal/domain/game/market.go`
- Modify: `internal/application/providers/registry.go` (`registerNew`)
- Modify: `internal/application/providers/registry_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `provider.KindDeals Kind = "deals"`, `provider.KindReleases Kind = "releases"`; `provider.Kinds()` returns `[cover, barcode, metadata, valuation, deals, releases]`.
  - `func (k provider.Kind) Scheduled() bool` — true for `valuation`, `deals`, `releases`.
  - `type game.Deal struct { ProviderGameID string; Title string; SteamAppID string; Shop string; Price game.Money; Regular game.Money; Cut int; HistoricalLow bool; Expires time.Time; URL string; ImageURL string; OwnedOn []string }`
  - `type game.Release struct { ProviderGameID string; Title string; Date game.Date; Platforms []string; CoverURL string; URL string; Because game.ID; BecauseTitle string }`

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the failing domain test** (append to `internal/domain/provider/kind_test.go`)

```go
func TestKinds(t *testing.T) {
	t.Run("GIVEN every kind of provider", func(t *testing.T) {
		t.Run("THEN the Providers page lists the home page's deals and releases after the others", func(t *testing.T) {
			assert.Equal(t, []Kind{KindCover, KindBarcode, KindMetadata, KindValuation, KindDeals, KindReleases}, Kinds())
		})

		t.Run("AND only prices, deals and releases are asked on a schedule", func(t *testing.T) {
			want := map[Kind]bool{
				KindCover:     false,
				KindBarcode:   false,
				KindMetadata:  false,
				KindValuation: true,
				KindDeals:     true,
				KindReleases:  true,
			}

			for _, k := range Kinds() {
				assert.Equal(t, want[k], k.Scheduled(), k)
			}
		})
	})
}
```

- [ ] **Step 3: Write the failing registry test** (append to `internal/application/providers/registry_test.go`)

```go
func TestProviders_scheduledKindsStayOff(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an enabled metadata provider with a key, and a releases provider of the same service seen for the first time", func(t *testing.T) {
		reg := providers.NewRegistry(newRepo(t, ctx), []providers.Impl{
			grouped{"shop", provider.KindMetadata},
			grouped{"shop-releases", provider.KindReleases},
		})

		_, err := reg.Providers(ctx, provider.KindMetadata)
		require.NoError(t, err)
		_, err = reg.ConfigureProvider(ctx, "shop", true, schema.Settings{"key": "k"})
		require.NoError(t, err)

		t.Run("WHEN the releases providers are listed", func(t *testing.T) {
			views, err := reg.Providers(ctx, provider.KindReleases)
			require.NoError(t, err)

			t.Run("THEN it shares the key but stays off: it would send requests on its own schedule", func(t *testing.T) {
				require.Len(t, views, 1)
				assert.Equal(t, "k", views[0].Settings()["key"])
				assert.False(t, views[0].Enabled())
			})
		})
	})
}
```

- [ ] **Step 4: Run both to see them fail**

Run: `task go -- test ./internal/domain/provider/ ./internal/application/providers/ -run 'TestKinds|TestProviders_scheduledKindsStayOff' -v`
Expected: FAIL to compile: `undefined: KindDeals`, `undefined: KindReleases`, `k.Scheduled undefined`.

- [ ] **Step 5: Add the kinds** (`internal/domain/provider/provider.go`, replacing the `Kind` constants and `Kinds`)

```go
// Values of Kind.
const (
	KindCover     Kind = "cover"
	KindBarcode   Kind = "barcode"
	KindMetadata  Kind = "metadata"  // game details: summary, genres, companies, trailers...
	KindValuation Kind = "valuation" // second-hand price estimates of physical copies
	KindDeals     Kind = "deals"     // current store deals, for the home page
	KindReleases  Kind = "releases"  // upcoming games, for the home page
)

// Kinds returns every kind of provider, in the order the Providers page lists them. A new kind is
// added here and to the plugins that bring providers of it; the configuration registry ranges over
// this list.
func Kinds() []Kind {
	return []Kind{KindCover, KindBarcode, KindMetadata, KindValuation, KindDeals, KindReleases}
}

// Scheduled reports whether providers of the kind are asked on a schedule rather than when a person
// looks at something: price estimates, and the home page's deals and releases. They send requests
// nobody asked for at that moment, so the user turns each of them on explicitly.
func (k Kind) Scheduled() bool {
	return k == KindValuation || k == KindDeals || k == KindReleases
}
```

- [ ] **Step 6: Keep scheduled kinds off when a sibling is on** (`internal/application/providers/registry.go`, in `registerNew`, replacing the price-provider condition)

```go
			// Providers asked on a schedule (prices, deals, releases) send requests nobody asked
			// for at that moment, so the user turns them on explicitly.
			if sib[0].Enabled() && !d.Kind.Scheduled() {
				_ = p.Configure(d, true, sib[0].Settings(), r.now()) // stays disabled if settings are incomplete
			}
```

- [ ] **Step 7: Add the values** (`internal/domain/game/market.go`)

```go
package game

import "time"

// Deal is a shop's current price for a game, as a deals service reports it. Its prices and link are
// the service's, unchanged: its terms forbid altering them (affiliate tags included).
type Deal struct {
	// ProviderGameID is the deals service's own id of the game.
	ProviderGameID string

	Title string

	// SteamAppID is the game's Steam app, "" when the service knows none.
	SteamAppID string

	// Shop names the shop that sells it ("Steam", "GOG"...).
	Shop string

	// Price is what it costs now; Regular what it costs when not on sale.
	Price   Money
	Regular Money

	// Cut is the discount, in percent of the regular price (0–100).
	Cut int

	// HistoricalLow reports that the price is the lowest the service has seen for the game.
	HistoricalLow bool

	// Expires is when the deal ends; zero when the shop does not say.
	Expires time.Time

	// URL opens the deal (through the service's link); "" when it gave none usable.
	URL string

	// ImageURL is the game's art on the service; "" when there is none.
	ImageURL string

	// OwnedOn lists the systems the user owns the game on, when they own it only on consoles. The
	// home page fills it; providers leave it empty.
	OwnedOn []string
}

// Release is a game that comes out soon, as a game database reports it.
type Release struct {
	// ProviderGameID is the database's own id of the game.
	ProviderGameID string

	Title string

	// Date is the first release date (YYYY-MM-DD).
	Date Date

	// Platforms are the Game Vault systems it is announced for, known ones only.
	Platforms []string

	// CoverURL is its box art on the database; URL its page there. Either may be "".
	CoverURL string
	URL      string

	// Because is the owned game whose series it continues, and BecauseTitle that game's title;
	// both empty for a release listed for its anticipation alone.
	Because      ID
	BecauseTitle string
}
```

- [ ] **Step 8: Run the tests**

Run: `task go -- test ./internal/domain/provider/ ./internal/application/providers/ ./internal/domain/game/ -v -run 'TestKinds|TestProviders_'`
Expected: PASS.

- [ ] **Step 9: Lint and the whole suite**

Run: `task lint:fix && task lint && task test`
Expected: 0 issues; all green. (`plugin.providersOf` ignores the new kinds until Task 2, which is harmless: no plugin has providers of them yet.)

- [ ] **Step 10: Commit**

```bash
git add internal/domain/provider internal/domain/game/market.go internal/application/providers
git commit -m "Add the deals and releases provider kinds, kept off until the user turns them on"
```

### Task 2: The home ports, and plugins that bring deals and releases

**Files:**
- Create: `internal/application/home/ports.go`
- Modify: `internal/application/plugin/plugin.go` (fields `Deals`, `Releases`; `providersOf`; `Registry.Deals`, `Registry.Releases`)
- Modify: `internal/application/plugin/plugin_test.go`
- Modify: `internal/application/plugin/plugintest/plugintest.go` (`Problems`)

**Interfaces:**
- Consumes: `provider.KindDeals`, `provider.KindReleases`, `game.Deal`, `game.Release` (Task 1).
- Produces (package `gamevault/internal/application/home`):
  - `type DealList string`; `DealsFeatured DealList = "featured"`, `DealsHistoricalLows DealList = "historical_lows"`
  - `type DealQuery struct { List DealList; Country string; Limit int }`
  - `type DealsProvider interface { providers.Impl; Deals(ctx context.Context, settings schema.Settings, q DealQuery) ([]game.Deal, error) }`
  - `type OwnedRef struct { Game game.ID; Title string; Links game.Links }`
  - `type ReleasesProvider interface { providers.Impl; Anticipated(ctx context.Context, settings schema.Settings, window time.Duration) ([]game.Release, error); Sequels(ctx context.Context, settings schema.Settings, owned []OwnedRef) ([]game.Release, error) }`
  - `type Block string`; `BlockDeals Block = "deals"`, `BlockReleases Block = "releases"`; `func Blocks() []Block`
  - `type ErrorKind string`; `ErrorRateLimited = "rate_limited"`, `ErrorQuota = "quota"`, `ErrorSettings = "settings"`, `ErrorUnavailable = "unavailable"`, `ErrorFailed = "failed"`
  - `type Snapshot struct { Block Block; Featured []game.Deal; HistoricalLows []game.Deal; FromSeries []game.Release; Anticipated []game.Release; FetchedAt time.Time; Error string; ErrorKind ErrorKind; FailedAt time.Time }`
  - `type Snapshots interface { Snapshot(ctx context.Context, b Block) (Snapshot, bool, error); SaveSnapshot(ctx context.Context, s Snapshot) error }`
  - `plugin.Plugin` fields `Deals []home.DealsProvider`, `Releases []home.ReleasesProvider`; `func (r *plugin.Registry) Deals() []home.DealsProvider`; `func (r *plugin.Registry) Releases() []home.ReleasesProvider`

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the ports** (`internal/application/home/ports.go`)

```go
// Package home gathers the home page's external blocks: current deals and upcoming releases, which
// change from one day to the next. They come from providers configured like any other
// (provider.KindDeals, provider.KindReleases), gathered by an unattended, jittered job and stored as
// one snapshot per block, so the page loads at once, the services get few requests, and a failure
// shows the last good data with its age.
package home

import (
	"context"
	"time"

	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

// DealList names one of the deal lists the home page shows.
type DealList string

// Values of DealList.
const (
	DealsFeatured       DealList = "featured"        // the service's own selection of current deals
	DealsHistoricalLows DealList = "historical_lows" // deals at the lowest price the service has seen
)

// DealQuery asks a deals provider for one list.
type DealQuery struct {
	List DealList

	// Country is the ISO 3166-1 alpha-2 code whose prices to list ("US", "DE"); a provider's own
	// country setting wins over it.
	Country string

	// Limit is how many deals to return at most.
	Limit int
}

// DealsProvider lists current deals (provider.KindDeals). It knows nothing of the collection: the
// home service removes the games the user owns.
type DealsProvider interface {
	providers.Impl

	// Deals returns the list q asks for, best first. Errors that concern the provider (a rejected
	// key, a rate limit, an outage) match the provider sentinels.
	Deals(ctx context.Context, settings schema.Settings, q DealQuery) ([]game.Deal, error)
}

// OwnedRef is an owned game as a releases provider needs it to find the series it belongs to.
type OwnedRef struct {
	Game  game.ID
	Title string

	// Links are the game's store links (automatic ones included); a provider uses the ones it can
	// look up.
	Links game.Links
}

// ReleasesProvider lists upcoming games (provider.KindReleases).
type ReleasesProvider interface {
	providers.Impl

	// Anticipated returns the most anticipated games that come out within window from now, most
	// anticipated first.
	Anticipated(ctx context.Context, settings schema.Settings, window time.Duration) ([]game.Release, error)

	// Sequels returns the upcoming games of the series (franchises and collections) the owned games
	// belong to, soonest first, each with the owned game that explains it (Release.Because).
	Sequels(ctx context.Context, settings schema.Settings, owned []OwnedRef) ([]game.Release, error)
}

// Block names one of the home page's external blocks.
type Block string

// Values of Block.
const (
	BlockDeals    Block = "deals"
	BlockReleases Block = "releases"
)

// Blocks returns the external blocks, in the order the job looks at them.
func Blocks() []Block { return []Block{BlockDeals, BlockReleases} }

// ErrorKind classifies a failed refresh, for a message the page translates.
type ErrorKind string

// Values of ErrorKind.
const (
	ErrorRateLimited ErrorKind = "rate_limited" // the service asked to slow down
	ErrorQuota       ErrorKind = "quota"        // the service's allowance is used up
	ErrorSettings    ErrorKind = "settings"     // the service rejects the key or settings
	ErrorUnavailable ErrorKind = "unavailable"  // the service is not answering
	ErrorFailed      ErrorKind = "failed"       // anything else
)

// Snapshot is what a block's last refreshes left: the last good data, and the last failure if it
// came after it.
type Snapshot struct {
	Block Block

	// Featured and HistoricalLows are the deals block's lists, as the provider gave them (the owned
	// games are removed when the page is read, so a purchase shows at once).
	Featured       []game.Deal
	HistoricalLows []game.Deal

	// FromSeries and Anticipated are the releases block's lists.
	FromSeries  []game.Release
	Anticipated []game.Release

	// FetchedAt is when the data was gathered; zero when no refresh has succeeded yet.
	FetchedAt time.Time

	// Error, ErrorKind and FailedAt describe the last failed refresh; empty when the last attempt
	// succeeded.
	Error     string
	ErrorKind ErrorKind
	FailedAt  time.Time
}

// Snapshots is the persistence port of the blocks' snapshots.
type Snapshots interface {
	// Snapshot returns a block's snapshot, and false when none was stored (or it can no longer be
	// read: the block is then gathered again).
	Snapshot(ctx context.Context, b Block) (Snapshot, bool, error)

	// SaveSnapshot stores a block's snapshot in place of the previous one.
	SaveSnapshot(ctx context.Context, s Snapshot) error
}
```

- [ ] **Step 3: Write the failing plugin test** (append to `internal/application/plugin/plugin_test.go`; add imports `"time"` and `"gamevault/internal/application/home"`)

```go
type fakeDeals struct{ id provider.ID }

func (f fakeDeals) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:   f.id,
		Kind: provider.KindDeals,
	}
}

func (fakeDeals) Test(context.Context, schema.Settings) error { return nil }

func (fakeDeals) Deals(context.Context, schema.Settings, home.DealQuery) ([]game.Deal, error) {
	return nil, nil
}

type fakeReleases struct{ id provider.ID }

func (f fakeReleases) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:   f.id,
		Kind: provider.KindReleases,
	}
}

func (fakeReleases) Test(context.Context, schema.Settings) error { return nil }

func (fakeReleases) Anticipated(context.Context, schema.Settings, time.Duration) ([]game.Release, error) {
	return nil, nil
}

func (fakeReleases) Sequels(context.Context, schema.Settings, []home.OwnedRef) ([]game.Release, error) {
	return nil, nil
}

func TestRegistry_homeProviders(t *testing.T) {
	t.Run("GIVEN a deals plugin and a game database that also lists releases", func(t *testing.T) {
		deals := plugin.Plugin{
			ID:    "deals",
			Name:  "Deals",
			Deals: []home.DealsProvider{fakeDeals{"deals"}},
		}
		db := plugin.Plugin{
			ID:       "db",
			Name:     "DB",
			Covers:   []media.CoverProvider{fakeCovers{"db"}},
			Releases: []home.ReleasesProvider{fakeReleases{"db-releases"}},
		}

		r, err := plugin.NewRegistry(deals, db)
		require.NoError(t, err)

		t.Run("THEN the home service gets them, and the providers registry configures them with the others", func(t *testing.T) {
			require.Len(t, r.Deals(), 1)
			require.Len(t, r.Releases(), 1)

			var ids []provider.ID
			for _, p := range r.Providers() {
				ids = append(ids, p.Descriptor().ID)
			}

			assert.Equal(t, []provider.ID{"db", "deals", "db-releases"}, ids, "kind by kind, in provider.Kinds' order")
		})
	})

	t.Run("GIVEN a releases provider whose id another plugin's deals provider uses", func(t *testing.T) {
		_, err := plugin.NewRegistry(
			plugin.Plugin{ID: "a", Deals: []home.DealsProvider{fakeDeals{"same"}}},
			plugin.Plugin{ID: "b", Releases: []home.ReleasesProvider{fakeReleases{"same"}}},
		)

		t.Run("THEN the registry refuses them", func(t *testing.T) {
			assert.ErrorIs(t, err, plugin.ErrDuplicate)
		})
	})
}
```

- [ ] **Step 4: Run it to see it fail**

Run: `task go -- test ./internal/application/plugin/ -run TestRegistry_homeProviders -v`
Expected: FAIL to compile: `unknown field Deals in struct literal of type plugin.Plugin`, `r.Deals undefined`.

- [ ] **Step 5: Add the pieces to `plugin.Plugin`** (`internal/application/plugin/plugin.go`)

Import `"gamevault/internal/application/home"`, then add after `Valuations`:

```go
	// Valuations estimate second-hand prices of physical copies.
	Valuations []valuation.Provider

	// Deals and Releases feed the home page: current deals and upcoming games.
	Deals    []home.DealsProvider
	Releases []home.ReleasesProvider
```

In `providersOf`, add two cases before `default`:

```go
	case provider.KindDeals:
		return asProviders(p.Deals)
	case provider.KindReleases:
		return asProviders(p.Releases)
```

After `Valuations()`, add:

```go
// Deals returns every plugin's deals providers, for the home service.
func (r *Registry) Deals() []home.DealsProvider {
	var out []home.DealsProvider
	for _, p := range r.plugins {
		out = append(out, p.Deals...)
	}

	return out
}

// Releases returns every plugin's releases providers, for the home service.
func (r *Registry) Releases() []home.ReleasesProvider {
	var out []home.ReleasesProvider
	for _, p := range r.plugins {
		out = append(out, p.Releases...)
	}

	return out
}
```

Update the doc comment of `Providers()` to "(covers, barcodes, metadata, prices, deals, releases)".

- [ ] **Step 6: Check the new pieces in `plugintest`** (`internal/application/plugin/plugintest/plugintest.go`, in `Problems`)

Replace the "adds nothing" count and add two loops after the valuations loop:

```go
	if len(p.Sources)+len(p.Covers)+len(p.Metadata)+len(p.Barcodes)+len(p.Valuations)+len(p.Deals)+len(p.Releases) == 0 {
		c.add("plugin %q: adds nothing (no source and no provider)", p.ID)
	}
```

```go
	for _, d := range p.Deals {
		c.provider(p.ID, d, provider.KindDeals)
	}

	for _, r := range p.Releases {
		c.provider(p.ID, r, provider.KindReleases)
	}
```

- [ ] **Step 7: Run the tests**

Run: `task go -- test ./internal/application/plugin/... -v`
Expected: PASS.

- [ ] **Step 8: Lint and the whole suite**

Run: `task lint:fix && task lint && task test`
Expected: 0 issues; all green.

- [ ] **Step 9: Commit**

```bash
git add internal/application/home internal/application/plugin
git commit -m "Add the home ports and let plugins bring deals and releases providers"
```

### Task 3: The snapshot table

**Files:**
- Create: `internal/adapters/outbound/sqlite/migrations/0012_home_snapshot.sql`
- Create: `internal/adapters/outbound/sqlite/home_snapshots.go`
- Create: `internal/adapters/outbound/sqlite/home_snapshots_test.go`
- Modify: `internal/adapters/outbound/sqlite/migrate_test.go` (`TestMigration0012`)

**Interfaces:**
- Consumes: `home.Snapshot`, `home.Snapshots`, `home.Block`, `home.ErrorKind` (Task 2); `game.Deal`, `game.Release` (Task 1).
- Produces: `func sqlite.NewHomeSnapshots(db *sqlite.DB) *sqlite.HomeSnapshots` implementing `home.Snapshots`; table `home_snapshots(block PRIMARY KEY, doc, fetched_at, error, error_kind, failed_at)`.

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the failing round-trip test** (`internal/adapters/outbound/sqlite/home_snapshots_test.go`)

```go
package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
)

func TestHomeSnapshots(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := NewHomeSnapshots(db)
	fetched := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

	t.Run("GIVEN no snapshot stored", func(t *testing.T) {
		t.Run("WHEN the deals block is read", func(t *testing.T) {
			s, ok, err := repo.Snapshot(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN there is none, named after its block", func(t *testing.T) {
				assert.False(t, ok)
				assert.Equal(t, home.BlockDeals, s.Block)
			})
		})
	})

	t.Run("GIVEN a deals snapshot with both lists", func(t *testing.T) {
		in := home.Snapshot{
			Block: home.BlockDeals,
			Featured: []game.Deal{{
				ProviderGameID: "018d937f-0000-7000-8000-000000000001",
				Title:          "Lantern Keep",
				SteamAppID:     "4410",
				Shop:           "Steam",
				Price:          game.Money{Amount: 499, Currency: "EUR"},
				Regular:        game.Money{Amount: 1999, Currency: "EUR"},
				Cut:            75,
				HistoricalLow:  true,
				Expires:        time.Date(2026, 10, 20, 17, 0, 0, 0, time.UTC),
				URL:            "https://itad.link/018d9386-0000-7000-8000-00000000aaaa/",
				ImageURL:       "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000001/boxart.jpg",
			}},
			HistoricalLows: []game.Deal{{Title: "Echo Valley"}},
			FetchedAt:      fetched,
		}

		require.NoError(t, repo.SaveSnapshot(ctx, in))

		t.Run("WHEN it is read back", func(t *testing.T) {
			got, ok, err := repo.Snapshot(ctx, home.BlockDeals)
			require.NoError(t, err)
			require.True(t, ok)

			t.Run("THEN every field survives, and the unset times stay zero", func(t *testing.T) {
				assert.Equal(t, in, got)
				assert.True(t, got.FailedAt.IsZero())
				assert.True(t, got.HistoricalLows[0].Expires.IsZero())
			})
		})

		t.Run("WHEN a failed refresh is saved over it, keeping the data", func(t *testing.T) {
			failed := in
			failed.Error = "the service is not answering: try again later"
			failed.ErrorKind = home.ErrorUnavailable
			failed.FailedAt = fetched.Add(6 * time.Hour)
			require.NoError(t, repo.SaveSnapshot(ctx, failed))

			got, _, err := repo.Snapshot(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN the failure is stored next to the last good data", func(t *testing.T) {
				assert.Equal(t, failed, got)
			})
		})
	})

	t.Run("GIVEN a releases snapshot", func(t *testing.T) {
		in := home.Snapshot{
			Block: home.BlockReleases,
			FromSeries: []game.Release{{
				ProviderGameID: "2001",
				Title:          "Lantern Keep II",
				Date:           "2026-11-20",
				Platforms:      []string{game.SystemPC, game.SystemPS5},
				CoverURL:       "https://images.igdb.com/igdb/image/upload/t_cover_big/co-lk2.jpg",
				URL:            "https://www.igdb.com/games/lantern-keep-ii",
				Because:        "g1",
				BecauseTitle:   "Lantern Keep",
			}},
			Anticipated: []game.Release{{
				ProviderGameID: "2002",
				Title:          "Harbor Lights",
				Date:           "2026-12-01",
			}},
			FetchedAt: fetched,
		}

		require.NoError(t, repo.SaveSnapshot(ctx, in))

		t.Run("WHEN it is read back", func(t *testing.T) {
			got, ok, err := repo.Snapshot(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN it is whole and the deals block is untouched", func(t *testing.T) {
				require.True(t, ok)
				assert.Equal(t, in, got)

				deals, _, err := repo.Snapshot(ctx, home.BlockDeals)
				require.NoError(t, err)
				assert.Len(t, deals.Featured, 1)
			})
		})
	})

	t.Run("GIVEN a row whose document cannot be read", func(t *testing.T) {
		_, err := db.conn(ctx).ExecContext(ctx, `UPDATE home_snapshots SET doc = '{"featured": 7}' WHERE block = 'deals'`)
		require.NoError(t, err)

		t.Run("WHEN it is read", func(t *testing.T) {
			_, ok, err := repo.Snapshot(ctx, home.BlockDeals)

			t.Run("THEN it counts as missing, so the block is gathered again rather than failing the page", func(t *testing.T) {
				require.NoError(t, err)
				assert.False(t, ok)
			})
		})
	})
}
```

- [ ] **Step 3: Write the failing migration test** (append to `internal/adapters/outbound/sqlite/migrate_test.go`; add the `home` import)

```go
func TestMigration0012(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a database migrated up to 0011", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.db")
		backups := filepath.Join(dir, "backups")

		db, err := openUpTo(ctx, path, "", "0011")
		require.NoError(t, err)
		db.Close()

		t.Run("WHEN this version opens it", func(t *testing.T) {
			db, err := Open(ctx, path, backups)
			require.NoError(t, err)

			defer db.Close()

			t.Run("THEN it was copied first, and home snapshots can be stored", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(backups, "pre-migration-0012.db"))
				assert.NoError(t, NewHomeSnapshots(db).SaveSnapshot(ctx, home.Snapshot{Block: home.BlockDeals}))
			})
		})
	})
}
```

- [ ] **Step 4: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run 'TestHomeSnapshots|TestMigration0012' -v`
Expected: FAIL to compile: `undefined: NewHomeSnapshots`.

- [ ] **Step 5: Write the migration** (`internal/adapters/outbound/sqlite/migrations/0012_home_snapshot.sql`)

```sql
-- The home page's external blocks (deals, releases): one row per block with the last good data as a
-- JSON document, when it was gathered, and the last failed refresh ('' when the last one worked).
-- The data is a cache of public information: losing it only means gathering it again.
CREATE TABLE home_snapshots (
  block      TEXT PRIMARY KEY,
  doc        TEXT NOT NULL,
  fetched_at TEXT NOT NULL DEFAULT '',
  error      TEXT NOT NULL DEFAULT '',
  error_kind TEXT NOT NULL DEFAULT '',
  failed_at  TEXT NOT NULL DEFAULT ''
);
```

- [ ] **Step 6: Write the repository** (`internal/adapters/outbound/sqlite/home_snapshots.go`)

```go
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
)

// HomeSnapshots implements home.Snapshots with one row per block.
type HomeSnapshots struct{ db *DB }

var _ home.Snapshots = (*HomeSnapshots)(nil)

// NewHomeSnapshots returns the repository backed by db.
func NewHomeSnapshots(db *DB) *HomeSnapshots { return &HomeSnapshots{db: db} }

// Snapshot implements home.Snapshots. A document that cannot be read counts as missing: the snapshot
// is a cache, and the job gathers the block again.
func (r *HomeSnapshots) Snapshot(ctx context.Context, b home.Block) (home.Snapshot, bool, error) {
	var raw, fetched, msg, kind, failed string

	err := r.db.conn(ctx).QueryRowContext(ctx,
		`SELECT doc, fetched_at, error, error_kind, failed_at FROM home_snapshots WHERE block = ?`, b).
		Scan(&raw, &fetched, &msg, &kind, &failed)
	if errors.Is(err, sql.ErrNoRows) {
		return home.Snapshot{Block: b}, false, nil
	}

	if err != nil {
		return home.Snapshot{}, false, err
	}

	var doc snapshotDoc
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return home.Snapshot{Block: b}, false, nil
	}

	return home.Snapshot{
		Block:          b,
		Featured:       dealsOf(doc.Featured),
		HistoricalLows: dealsOf(doc.HistoricalLows),
		FromSeries:     releasesOf(doc.FromSeries),
		Anticipated:    releasesOf(doc.Anticipated),
		FetchedAt:      parseTime(fetched),
		Error:          msg,
		ErrorKind:      home.ErrorKind(kind),
		FailedAt:       parseTime(failed),
	}, true, nil
}

// SaveSnapshot implements home.Snapshots.
func (r *HomeSnapshots) SaveSnapshot(ctx context.Context, s home.Snapshot) error {
	raw, err := json.Marshal(snapshotDoc{
		V:              1,
		Featured:       dealDocsOf(s.Featured),
		HistoricalLows: dealDocsOf(s.HistoricalLows),
		FromSeries:     releaseDocsOf(s.FromSeries),
		Anticipated:    releaseDocsOf(s.Anticipated),
	})
	if err != nil {
		return err
	}

	_, err = r.db.conn(ctx).ExecContext(ctx, `INSERT INTO home_snapshots (block, doc, fetched_at, error, error_kind, failed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(block) DO UPDATE SET doc = excluded.doc, fetched_at = excluded.fetched_at, error = excluded.error,
			error_kind = excluded.error_kind, failed_at = excluded.failed_at`,
		s.Block, string(raw), timeOrEmpty(s.FetchedAt), s.Error, string(s.ErrorKind), timeOrEmpty(s.FailedAt))

	return err
}

// timeOrEmpty stores a zero time as '', which parseTime reads back as zero.
func timeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return formatTime(t)
}

// snapshotDoc is the stored form of a snapshot's lists. Field names never change, so rows written
// by an older version stay readable; a field added later reads as empty from them.
type snapshotDoc struct {
	V              int          `json:"v"`
	Featured       []dealDoc    `json:"featured"`
	HistoricalLows []dealDoc    `json:"historicalLows"`
	FromSeries     []releaseDoc `json:"fromSeries"`
	Anticipated    []releaseDoc `json:"anticipated"`
}

// dealDoc is the stored form of a game.Deal.
type dealDoc struct {
	ProviderGameID  string   `json:"providerGameId"`
	Title           string   `json:"title"`
	SteamAppID      string   `json:"steamAppId"`
	Shop            string   `json:"shop"`
	Price           int64    `json:"price"`
	PriceCurrency   string   `json:"priceCurrency"`
	Regular         int64    `json:"regular"`
	RegularCurrency string   `json:"regularCurrency"`
	Cut             int      `json:"cut"`
	HistoricalLow   bool     `json:"historicalLow"`
	Expires         string   `json:"expires"`
	URL             string   `json:"url"`
	ImageURL        string   `json:"imageUrl"`
	OwnedOn         []string `json:"ownedOn"`
}

// releaseDoc is the stored form of a game.Release.
type releaseDoc struct {
	ProviderGameID string   `json:"providerGameId"`
	Title          string   `json:"title"`
	Date           string   `json:"date"`
	Platforms      []string `json:"platforms"`
	CoverURL       string   `json:"coverUrl"`
	URL            string   `json:"url"`
	Because        string   `json:"because"`
	BecauseTitle   string   `json:"becauseTitle"`
}

func dealDocsOf(in []game.Deal) []dealDoc {
	out := make([]dealDoc, 0, len(in))
	for _, d := range in {
		out = append(out, dealDoc{
			ProviderGameID:  d.ProviderGameID,
			Title:           d.Title,
			SteamAppID:      d.SteamAppID,
			Shop:            d.Shop,
			Price:           d.Price.Amount,
			PriceCurrency:   d.Price.Currency,
			Regular:         d.Regular.Amount,
			RegularCurrency: d.Regular.Currency,
			Cut:             d.Cut,
			HistoricalLow:   d.HistoricalLow,
			Expires:         timeOrEmpty(d.Expires),
			URL:             d.URL,
			ImageURL:        d.ImageURL,
			OwnedOn:         d.OwnedOn,
		})
	}

	return out
}

func dealsOf(in []dealDoc) []game.Deal {
	if len(in) == 0 {
		return nil
	}

	out := make([]game.Deal, 0, len(in))
	for _, d := range in {
		out = append(out, game.Deal{
			ProviderGameID: d.ProviderGameID,
			Title:          d.Title,
			SteamAppID:     d.SteamAppID,
			Shop:           d.Shop,
			Price:          game.Money{Amount: d.Price, Currency: d.PriceCurrency},
			Regular:        game.Money{Amount: d.Regular, Currency: d.RegularCurrency},
			Cut:            d.Cut,
			HistoricalLow:  d.HistoricalLow,
			Expires:        parseTime(d.Expires),
			URL:            d.URL,
			ImageURL:       d.ImageURL,
			OwnedOn:        d.OwnedOn,
		})
	}

	return out
}

func releaseDocsOf(in []game.Release) []releaseDoc {
	out := make([]releaseDoc, 0, len(in))
	for _, r := range in {
		out = append(out, releaseDoc{
			ProviderGameID: r.ProviderGameID,
			Title:          r.Title,
			Date:           string(r.Date),
			Platforms:      r.Platforms,
			CoverURL:       r.CoverURL,
			URL:            r.URL,
			Because:        string(r.Because),
			BecauseTitle:   r.BecauseTitle,
		})
	}

	return out
}

func releasesOf(in []releaseDoc) []game.Release {
	if len(in) == 0 {
		return nil
	}

	out := make([]game.Release, 0, len(in))
	for _, r := range in {
		out = append(out, game.Release{
			ProviderGameID: r.ProviderGameID,
			Title:          r.Title,
			Date:           game.Date(r.Date),
			Platforms:      r.Platforms,
			CoverURL:       r.CoverURL,
			URL:            r.URL,
			Because:        game.ID(r.Because),
			BecauseTitle:   r.BecauseTitle,
		})
	}

	return out
}
```

Note for the implementer: `assert.Equal(t, in, got)` compares nil and empty slices as different. The repository reads an empty list back as `nil` (`dealsOf`, `releasesOf`), and the test's snapshots leave unset lists `nil`, so they agree; keep it that way rather than returning empty slices. `OwnedOn` and `Platforms` that were `nil` are written as JSON `null` and read back as `nil`.

- [ ] **Step 7: Run the tests**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -v -run 'TestHomeSnapshots|TestMigration'`
Expected: PASS.

- [ ] **Step 8: Lint and the whole suite**

Run: `task lint:fix && task lint && task test`
Expected: 0 issues; all green.

- [ ] **Step 9: Commit**

```bash
git add internal/adapters/outbound/sqlite
git commit -m "Store one home snapshot per block, keeping the last good data next to the last failure"
```

### Task 4: The IsThereAnyDeal plugin

**Files:**
- Create: `internal/adapters/outbound/itad/deals.go`, `internal/adapters/outbound/itad/plugin.go`
- Create: `internal/adapters/outbound/itad/fake_test.go`, `deals_test.go`, `failures_test.go`
- Modify: `cmd/gamevault/plugins.go` (one line, under "Game databases": a new "Deals" group)
- Modify: `web/src/i18n/locales/en.json`, `es.json` (`providers.itad.*`)

**Interfaces:**
- Consumes: `home.DealsProvider`, `home.DealQuery`, `home.DealsFeatured`, `home.DealsHistoricalLows` (Task 2); `game.Deal` (Task 1); `provider.KindDeals` (Task 1); `apiclient`, `ratelimit`, `apiclienttest`.
- Produces: `itad.ID provider.ID = "itad"`; `func itad.NewDeals() *itad.Deals` (fields `API *apiclient.Client`, unexported `throttle`); `func itad.Plugin() plugin.Plugin`; sentinels `itad.ErrNotConfigured`, `itad.ErrBadKey`, `itad.ErrBadCountry` (all `provider.BadSettings`); image host `assets.isthereanydeal.com`.

ITAD facts this task is built on (documentation read on 2026-10-15; probes with a bogus key on 2026-10-15):
- `GET https://api.isthereanydeal.com/deals/v2` — query `country` (ISO 3166-1 alpha-2, default `US`), `offset`, `limit` (1–200, default 20), `sort` ("same as in deals list on the website"; documented examples `-cut`, `price`), `nondeals`, `mature` (default false), `shops`, `filter`. Answer `{"nextOffset", "hasMore", "list": [{"id", "slug", "title", "type" ("game", "dlc", null…), "mature", "assets": {"boxart", "banner145", "banner300", "banner400", "banner600"}, "deal": {"shop": {"id", "name"}, "price": {"amount", "amountInt", "currency"}, "regular", "cut", "voucher", "storeLow", "historyLow", "historyLow_1y", "historyLow_3m", "flag" ("H" on a historical low in the sample; null), "drm", "platforms", "timestamp", "expiry" (RFC 3339 with offset, or null), "url" (https://itad.link/…)}}]}`. Assets are served from `assets.isthereanydeal.com` (probed: 200 image/jpeg).
- `POST /lookup/shop/{shopId}/id/v1` with a JSON array of ITAD ids → `{"<itad id>": ["app/1336490", "sub/467072"] | null}`; Steam is shop 61.
- Auth: the key as `key` query parameter or the `ITAD-API-Key` header (Game Vault uses the header). Bogus key → HTTP 403 `{"status_code":403,"reason_phrase":"Invalid or expired api key"}`, checked before the parameters; no key → 403 `{"status_code":403,"reason_phrase":"Missing api key"}`; unknown path → 404 JSON. 1000 requests per 5 minutes; beyond, 429 with `Retry-After`.

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the fake ITAD** (`internal/adapters/outbound/itad/fake_test.go`)

```go
package itad

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// Answers of the real API, with invented values. The error bodies were received on 2026-10-15 with
// a bogus key; the data shapes follow ITAD's documentation (docs.isthereanydeal.com) of that day.
const (
	// badKey is ITAD's answer to an unknown or revoked key (HTTP 403, not 401).
	badKey = `{"status_code":403,"reason_phrase":"Invalid or expired api key"}`

	// missingKey is ITAD's answer to a request without a key (HTTP 403).
	missingKey = `{"status_code":403,"reason_phrase":"Missing api key"}`

	// featuredPage is a deals list in ITAD's own order: a historical low on Steam, a game with no
	// expiry and no Steam app, a soundtrack (not a game), and a game without art (ITAD sends an empty
	// array for its assets) whose link is not https.
	featuredPage = `{"nextOffset": 4, "hasMore": true, "list": [
  {"id": "018d937f-0000-7000-8000-000000000001", "slug": "lantern-keep", "title": "Lantern Keep", "type": "game", "mature": false,
   "assets": {"boxart": "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000001/boxart.jpg?t=1",
              "banner400": "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000001/banner400.jpg?t=1"},
   "deal": {"shop": {"id": 61, "name": "Steam"},
            "price": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "regular": {"amount": 19.99, "amountInt": 1999, "currency": "EUR"},
            "cut": 75, "voucher": null,
            "storeLow": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "historyLow": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "flag": "H", "drm": [{"id": 61, "name": "Steam"}], "platforms": [{"id": 1, "name": "Windows"}],
            "timestamp": "2026-10-14T15:25:52+02:00", "expiry": "2026-10-20T19:00:00+02:00",
            "url": "https://itad.link/018d9386-0000-7000-8000-00000000aaaa/"}},
  {"id": "018d937f-0000-7000-8000-000000000002", "slug": "echo-valley", "title": "Echo Valley", "type": "game", "mature": false,
   "assets": {"boxart": "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000002/boxart.jpg?t=2"},
   "deal": {"shop": {"id": 35, "name": "GOG"},
            "price": {"amount": 2.99, "amountInt": 299, "currency": "EUR"},
            "regular": {"amount": 9.99, "amountInt": 999, "currency": "EUR"},
            "cut": 70, "voucher": null, "storeLow": null,
            "historyLow": {"amount": 2.99, "amountInt": 299, "currency": "EUR"},
            "flag": null, "drm": [], "platforms": [{"id": 1, "name": "Windows"}],
            "timestamp": "2026-10-14T10:00:00+02:00", "expiry": null,
            "url": "https://itad.link/018d9386-0000-7000-8000-00000000bbbb/"}},
  {"id": "018d937f-0000-7000-8000-000000000003", "slug": "harbor-lights-soundtrack", "title": "Harbor Lights Soundtrack", "type": "dlc", "mature": false,
   "assets": {"boxart": "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000003/boxart.jpg?t=3"},
   "deal": {"shop": {"id": 61, "name": "Steam"},
            "price": {"amount": 0.99, "amountInt": 99, "currency": "EUR"},
            "regular": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "cut": 80, "voucher": null, "storeLow": null, "historyLow": null, "flag": null, "drm": [], "platforms": [],
            "timestamp": "2026-10-14T10:00:00+02:00", "expiry": null,
            "url": "https://itad.link/018d9386-0000-7000-8000-00000000cccc/"}},
  {"id": "018d937f-0000-7000-8000-000000000004", "slug": "copper-coast", "title": "Copper Coast", "type": "game", "mature": false,
   "assets": [],
   "deal": {"shop": {"id": 16, "name": "Epic Game Store"},
            "price": {"amount": 9.99, "amountInt": 999, "currency": "EUR"},
            "regular": {"amount": 19.99, "amountInt": 1999, "currency": "EUR"},
            "cut": 50, "voucher": null, "storeLow": null,
            "historyLow": {"amount": 4.99, "amountInt": 499, "currency": "EUR"},
            "flag": null, "drm": [], "platforms": [],
            "timestamp": "2026-10-14T10:00:00+02:00", "expiry": "2026-10-16T17:00:00Z",
            "url": "http://itad.link/018d9386-0000-7000-8000-00000000dddd/"}}
]}`

	// steamLookup is the Steam lookup's answer for the three games: a package listed before the app,
	// a game with no Steam entry (null) and one with an empty list.
	steamLookup = `{
  "018d937f-0000-7000-8000-000000000001": ["sub/99", "app/4410"],
  "018d937f-0000-7000-8000-000000000002": null,
  "018d937f-0000-7000-8000-000000000004": []
}`
)

// fakeITAD reproduces the deals list and the Steam lookup, with ITAD's key check.
type fakeITAD struct {
	srv *httptest.Server

	mu sync.Mutex

	// deals answers /deals/v2.
	deals string

	// queries records the query of every deals request; lookups the ids of every lookup.
	queries []url.Values
	lookups [][]string

	// keys records the ITAD-API-Key header of every request; keyInURL that a key came as a
	// query parameter instead.
	keys     []string
	keyInURL bool

	// busy makes ITAD answer 429 with a Retry-After of two minutes.
	busy bool
}

func newFakeITAD(t *testing.T) *fakeITAD {
	t.Helper()

	f := &fakeITAD{deals: featuredPage}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeITAD) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	key := r.Header.Get("ITAD-API-Key")
	f.keys = append(f.keys, key)

	if r.URL.Query().Has("key") {
		f.keyInURL = true
	}

	switch {
	case key == "":
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(missingKey))

		return
	case key != "good-key":
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(badKey))

		return
	case f.busy:
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)

		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/deals/v2":
		f.queries = append(f.queries, r.URL.Query())
		w.Write([]byte(f.deals))
	case r.Method == http.MethodPost && r.URL.Path == "/lookup/shop/61/id/v1":
		var ids []string
		_ = json.NewDecoder(r.Body).Decode(&ids)
		f.lookups = append(f.lookups, ids)
		w.Write([]byte(steamLookup))
	default:
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"status_code":404,"reason_phrase":"Not Found"}`))
	}
}

func (f *fakeITAD) set(change func(f *fakeITAD)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	change(f)
}

func (f *fakeITAD) sentQueries() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]url.Values(nil), f.queries...)
}

func (f *fakeITAD) sentLookups() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([][]string(nil), f.lookups...)
}

// provider returns the deals provider pointed at the fake, without spacing its requests.
func (f *fakeITAD) provider() *Deals {
	d := NewDeals()
	d.API.BaseURL = f.srv.URL
	d.API.HTTP = f.srv.Client()
	d.throttle = nil

	return d
}
```

- [ ] **Step 3: Write the failing tests** (`internal/adapters/outbound/itad/deals_test.go`)

```go
package itad

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

var goodSettings = schema.Settings{settingAPIKey: "good-key", settingCountry: "es"}

func TestDeals(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a key and a country in the settings", func(t *testing.T) {
		f := newFakeITAD(t)
		d := f.provider()

		t.Run("WHEN the featured deals are asked for, with another country in the query", func(t *testing.T) {
			got, err := d.Deals(ctx, goodSettings, home.DealQuery{List: home.DealsFeatured, Country: "DE", Limit: 40})
			require.NoError(t, err)

			t.Run("THEN the settings' country wins, in ITAD's own order, without mature games", func(t *testing.T) {
				q := f.sentQueries()
				require.Len(t, q, 1)
				assert.Equal(t, "ES", q[0].Get("country"))
				assert.Equal(t, "40", q[0].Get("limit"))
				assert.Equal(t, "false", q[0].Get("mature"))
				assert.False(t, q[0].Has("sort"))
			})

			t.Run("AND the key travels in the header, never in the URL", func(t *testing.T) {
				assert.False(t, f.keyInURL)
			})

			t.Run("AND only games are kept, in order, with their prices and link as ITAD gave them", func(t *testing.T) {
				require.Len(t, got, 3)
				assert.Equal(t, game.Deal{
					ProviderGameID: "018d937f-0000-7000-8000-000000000001",
					Title:          "Lantern Keep",
					SteamAppID:     "4410",
					Shop:           "Steam",
					Price:          game.Money{Amount: 499, Currency: "EUR"},
					Regular:        game.Money{Amount: 1999, Currency: "EUR"},
					Cut:            75,
					HistoricalLow:  true,
					Expires:        time.Date(2026, 10, 20, 17, 0, 0, 0, time.UTC),
					URL:            "https://itad.link/018d9386-0000-7000-8000-00000000aaaa/",
					ImageURL:       "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000001/boxart.jpg?t=1",
				}, normalized(got[0]))
				assert.Equal(t, "Echo Valley", got[1].Title)
				assert.Equal(t, "Copper Coast", got[2].Title)
			})

			t.Run("AND a game ITAD knows no Steam app for keeps an empty Steam id", func(t *testing.T) {
				assert.Empty(t, got[1].SteamAppID, "null in the lookup")
				assert.Empty(t, got[2].SteamAppID, "an empty list in the lookup")
			})

			t.Run("AND a deal at its lowest price is marked by price when ITAD sends no flag", func(t *testing.T) {
				assert.True(t, got[1].HistoricalLow)
				assert.False(t, got[2].HistoricalLow)
			})

			t.Run("AND a deal without an end, art or an https link has none", func(t *testing.T) {
				assert.True(t, got[1].Expires.IsZero())
				assert.Empty(t, got[2].ImageURL, `"assets": [] does not fail the list`)
				assert.Empty(t, got[2].URL, "only https links become links on the page")
			})

			t.Run("AND the Steam apps are looked up once, for the games kept", func(t *testing.T) {
				assert.Equal(t, [][]string{{
					"018d937f-0000-7000-8000-000000000001",
					"018d937f-0000-7000-8000-000000000002",
					"018d937f-0000-7000-8000-000000000004",
				}}, f.sentLookups())
			})
		})

		t.Run("WHEN the historical lows are asked for", func(t *testing.T) {
			got, err := d.Deals(ctx, goodSettings, home.DealQuery{List: home.DealsHistoricalLows, Limit: 40})
			require.NoError(t, err)

			t.Run("THEN the deepest cuts are asked for, a full page of them", func(t *testing.T) {
				q := f.sentQueries()
				last := q[len(q)-1]
				assert.Equal(t, "-cut", last.Get("sort"))
				assert.Equal(t, "200", last.Get("limit"))
			})

			t.Run("AND only the deals at their lowest price are kept", func(t *testing.T) {
				var titles []string
				for _, deal := range got {
					titles = append(titles, deal.Title)
					assert.True(t, deal.HistoricalLow)
				}

				assert.Equal(t, []string{"Lantern Keep", "Echo Valley"}, titles)
			})
		})
	})

	t.Run("GIVEN a key and no country in the settings", func(t *testing.T) {
		f := newFakeITAD(t)
		d := f.provider()
		s := schema.Settings{settingAPIKey: "good-key"}

		t.Run("WHEN deals are asked for with a country, then without one", func(t *testing.T) {
			_, err := d.Deals(ctx, s, home.DealQuery{List: home.DealsFeatured, Country: "de", Limit: 10})
			require.NoError(t, err)
			_, err = d.Deals(ctx, s, home.DealQuery{List: home.DealsFeatured, Limit: 10})
			require.NoError(t, err)

			t.Run("THEN the query's country is used, else the United States", func(t *testing.T) {
				q := f.sentQueries()
				require.Len(t, q, 2)
				assert.Equal(t, "DE", q[0].Get("country"))
				assert.Equal(t, "US", q[1].Get("country"))
			})
		})
	})

	t.Run("GIVEN settings ITAD cannot work with", func(t *testing.T) {
		f := newFakeITAD(t)
		d := f.provider()

		for _, tc := range []struct {
			name string
			s    schema.Settings
			want error
		}{
			{"no key", schema.Settings{}, ErrNotConfigured},
			{"a country that is not a two-letter code", schema.Settings{settingAPIKey: "good-key", settingCountry: "Spain"}, ErrBadCountry},
		} {
			t.Run("WHEN deals are asked for with "+tc.name, func(t *testing.T) {
				_, err := d.Deals(ctx, tc.s, home.DealQuery{List: home.DealsFeatured, Limit: 10})

				t.Run("THEN the settings are rejected without a request", func(t *testing.T) {
					assert.ErrorIs(t, err, tc.want)
					assert.ErrorIs(t, err, provider.ErrBadSettings)
					assert.Empty(t, f.sentQueries())
				})
			})
		}
	})

	t.Run("GIVEN a key ITAD does not know", func(t *testing.T) {
		f := newFakeITAD(t)
		d := f.provider()

		t.Run("WHEN deals are asked for", func(t *testing.T) {
			_, err := d.Deals(ctx, schema.Settings{settingAPIKey: "bogus"}, home.DealQuery{List: home.DealsFeatured, Limit: 10})

			t.Run("THEN the key is rejected as the provider's settings", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrBadKey)
				assert.ErrorIs(t, err, provider.ErrBadSettings)
			})
		})
	})

	t.Run("GIVEN ITAD asks to slow down", func(t *testing.T) {
		f := newFakeITAD(t)
		f.set(func(f *fakeITAD) { f.busy = true })
		d := f.provider()

		t.Run("WHEN deals are asked for", func(t *testing.T) {
			_, err := d.Deals(ctx, goodSettings, home.DealQuery{List: home.DealsFeatured, Limit: 10})

			t.Run("THEN it is a rate limit, which says nothing about the deals", func(t *testing.T) {
				assert.ErrorIs(t, err, provider.ErrRateLimited)
				assert.NotErrorIs(t, err, provider.ErrBadSettings)
			})
		})
	})
}

func TestDeals_test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the fake ITAD", func(t *testing.T) {
		f := newFakeITAD(t)
		d := f.provider()

		t.Run("WHEN a good key is tested", func(t *testing.T) {
			err := d.Test(ctx, goodSettings)

			t.Run("THEN it works, with one small request", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, f.sentQueries(), 1)
				assert.Equal(t, "1", f.sentQueries()[0].Get("limit"))
			})
		})

		t.Run("WHEN a bogus key is tested", func(t *testing.T) {
			err := d.Test(ctx, schema.Settings{settingAPIKey: "bogus"})

			t.Run("THEN it says the key was rejected", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrBadKey)
			})
		})
	})
}

// normalized returns d with its expiry in UTC, so a test compares the instant, not the offset.
func normalized(d game.Deal) game.Deal {
	if !d.Expires.IsZero() {
		d.Expires = d.Expires.UTC()
	}

	return d
}
```

And `internal/adapters/outbound/itad/failures_test.go`:

```go
package itad

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient/apiclienttest"
	"gamevault/internal/application/home"
	"gamevault/internal/domain/provider"
)

// TestDeals_failures checks each way ITAD can fail, on the deals list and on the Steam lookup: a
// rate limit, an outage, no answer, a web page and a truncated answer all fail the list with what
// happened, and none is taken for a rejected key, which would tell the user to fix their settings.
func TestDeals_failures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, path := range []string{"/deals/v2", "/lookup/shop/61/id/v1"} {
		for _, fail := range apiclienttest.Failures() {
			t.Run("GIVEN ITAD's "+path+" answers with "+fail.Name, func(t *testing.T) {
				f := newFakeITAD(t)
				srv := httptest.NewServer(apiclienttest.FailOn(f.srv.Config.Handler, path, fail.Serve))
				t.Cleanup(srv.Close)

				d := f.provider()
				d.API.BaseURL = srv.URL
				d.API.HTTP = srv.Client()

				t.Run("WHEN the featured deals are asked for", func(t *testing.T) {
					_, err := d.Deals(ctx, goodSettings, home.DealQuery{List: home.DealsFeatured, Limit: 10})

					t.Run("THEN the list fails, and not as a rejected key", func(t *testing.T) {
						require.Error(t, err)
						assert.NotErrorIs(t, err, provider.ErrBadSettings)

						if fail.Want != nil {
							assert.ErrorIs(t, err, fail.Want)
						}
					})
				})
			})
		}
	}
}
```

- [ ] **Step 4: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/itad/ -v`
Expected: FAIL to compile: `undefined: NewDeals`, `undefined: settingAPIKey`.

- [ ] **Step 5: Write the provider** (`internal/adapters/outbound/itad/deals.go`)

```go
// Package itad is the IsThereAnyDeal plugin: current game deals (provider.KindDeals) from ITAD's
// API v2 (https://docs.isthereanydeal.com), with the user's own free API key.
//
// ITAD's terms allow private personal use with one's own key, ask for caching and a credit, and
// forbid altering its data (prices, and links with their affiliate tags) and competing with it.
// Game Vault therefore shows its lists as they come, links through ITAD's own links, keeps the
// lists as a snapshot refreshed every six hours, and has no price alerts of its own. Its limit is
// 1000 requests per 5 minutes (HTTP 429 with Retry-After beyond); a refresh sends four.
package itad

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/ratelimit"
	"gamevault/internal/application/home"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ID identifies IsThereAnyDeal in the deals chain.
const ID provider.ID = "itad"

const (
	settingAPIKey  = "api_key"
	settingCountry = "country"

	defaultBaseURL = "https://api.isthereanydeal.com"

	// keyURL is where a signed-in user registers an app and gets its API key.
	keyURL = "https://isthereanydeal.com/apps/my/"

	// steamShop is Steam's shop id in ITAD's lookups.
	steamShop = 61

	// imageHost serves the games' art.
	imageHost = "assets.isthereanydeal.com"

	// maxList is the largest page ITAD's deals list gives (limit 1–200). The Steam lookup takes the
	// ids of one page at once.
	maxList = 200

	requestTimeout = 20 * time.Second
)

var (
	// ErrNotConfigured means the API key is missing.
	ErrNotConfigured = provider.BadSettings("enter your IsThereAnyDeal API key in the provider settings")

	// ErrBadKey means ITAD rejected the API key (HTTP 403 "Invalid or expired api key").
	ErrBadKey = provider.BadSettings("the API key was rejected: check it in the provider settings")

	// ErrBadCountry means the country setting is not a two-letter code.
	ErrBadCountry = provider.BadSettings("the country must be a two-letter code such as US, DE or ES: fix it in the provider settings")
)

// Deals implements home.DealsProvider with ITAD's deals list.
type Deals struct {
	// API calls ITAD (https://api.isthereanydeal.com); tests point it at a fake server.
	API *apiclient.Client

	// throttle spaces the requests; nil sends them at once.
	throttle *ratelimit.Throttle
}

var (
	_ home.DealsProvider = (*Deals)(nil)
	_ media.ImageHoster  = (*Deals)(nil)
)

// NewDeals returns the provider with ITAD's production endpoint. One request a second is far
// below ITAD's limit; a 429 or an outage rests it a minute, or as long as Retry-After asks.
func NewDeals() *Deals {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = requestTimeout

	return &Deals{
		API: api,
		throttle: ratelimit.New(ratelimit.Config{
			Gap:     time.Second,
			MaxWait: 30 * time.Second,
			Backoff: time.Minute,
		}),
	}
}

// Descriptor implements providers.Impl.
func (d *Deals) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             ID,
		Kind:           provider.KindDeals,
		Name:           "IsThereAnyDeal",
		DescriptionKey: "providers.itad.description",
		Fields: schema.Fields{
			{
				Key:      settingAPIKey,
				LabelKey: "providers.itad.apiKey",
				HelpKey:  "providers.itad.apiKeyHelp",
				HelpURL:  keyURL,
				Kind:     schema.FieldSecret,
				Required: true,
			},
			{
				Key:      settingCountry,
				LabelKey: "providers.itad.country",
				HelpKey:  "providers.itad.countryHelp",
				Kind:     schema.FieldText,
			},
		},
		// Disabled until the user enters a key, like every provider that needs one.
		DefaultOrder: 10,
	}
}

// ImageHosts implements media.ImageHoster: the games' art goes through the image proxy.
func (d *Deals) ImageHosts() []string { return []string{imageHost} }

// Test implements providers.Impl with the smallest deals list.
func (d *Deals) Test(ctx context.Context, s schema.Settings) error {
	key, country, err := settingsOf(s, "")
	if err != nil {
		return err
	}

	var page dealsPage

	return d.send(ctx, key, apiclient.Request{
		Path:  "/deals/v2",
		Query: url.Values{"country": {country}, "limit": {"1"}},
	}, &page)
}

var reCountry = regexp.MustCompile(`^[A-Za-z]{2}$`)

// settingsOf returns the key and the country to ask for: the settings' own, else fallback (the
// home page's, from the preferred currency), else the United States.
func settingsOf(s schema.Settings, fallback string) (key, country string, err error) {
	key = strings.TrimSpace(s[settingAPIKey])
	if key == "" {
		return "", "", ErrNotConfigured
	}

	country = strings.TrimSpace(s[settingCountry])
	if country == "" {
		country = strings.TrimSpace(fallback)
	}

	if country == "" {
		country = "US"
	}

	if !reCountry.MatchString(country) {
		return "", "", ErrBadCountry
	}

	return key, strings.ToUpper(country), nil
}

// Deals implements home.DealsProvider. ITAD documents no "historical lows" list: those are the
// deepest cuts (a full page) whose price is at the lowest ITAD has seen.
func (d *Deals) Deals(ctx context.Context, s schema.Settings, q home.DealQuery) ([]game.Deal, error) {
	key, country, err := settingsOf(s, q.Country)
	if err != nil {
		return nil, err
	}

	limit := q.Limit
	if limit <= 0 || limit > maxList {
		limit = maxList
	}

	query := url.Values{"country": {country}, "mature": {"false"}}
	keep := func(itemJSON) bool { return true }

	switch q.List {
	case home.DealsFeatured:
		// No sort: ITAD's own order, the one its deals page opens with.
		query.Set("limit", strconv.Itoa(limit))
	case home.DealsHistoricalLows:
		query.Set("sort", "-cut")
		query.Set("limit", strconv.Itoa(maxList))

		keep = itemJSON.atLow
	default:
		return nil, fmt.Errorf("unknown deal list %q", q.List)
	}

	var page dealsPage
	if err := d.send(ctx, key, apiclient.Request{Path: "/deals/v2", Query: query}, &page); err != nil {
		return nil, err
	}

	// Only games: a soundtrack's or a DLC's deal says nothing about a game the user may own.
	items := make([]itemJSON, 0, limit)

	for _, it := range page.List {
		if it.Type == "game" && it.Deal != nil && keep(it) {
			items = append(items, it)
		}

		if len(items) == limit {
			break
		}
	}

	steam, err := d.steamApps(ctx, key, items)
	if err != nil {
		return nil, err
	}

	out := make([]game.Deal, 0, len(items))
	for _, it := range items {
		out = append(out, it.deal(steam[it.ID]))
	}

	return out, nil
}

var reAppID = regexp.MustCompile(`^[0-9]{1,10}$`)

// steamApps returns the Steam app of each game ITAD knows one for, so the home page matches
// ownership by Steam id. A package ("sub/…") is not an app and is skipped.
func (d *Deals) steamApps(ctx context.Context, key string, items []itemJSON) (map[string]string, error) {
	out := map[string]string{}
	if len(items) == 0 {
		return out, nil
	}

	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}

	// A game without a Steam entry is null (or an empty list): a nil slice either way.
	var found map[string][]string

	err := d.send(ctx, key, apiclient.Request{
		Method: http.MethodPost,
		Path:   fmt.Sprintf("/lookup/shop/%d/id/v1", steamShop),
		Body:   ids,
	}, &found)
	if err != nil {
		return nil, err
	}

	for id, shopIDs := range found {
		for _, s := range shopIDs {
			if app, ok := strings.CutPrefix(s, "app/"); ok && reAppID.MatchString(app) {
				out[id] = app
				break
			}
		}
	}

	return out, nil
}

// send makes one request with the key, through the throttle. The key travels in a header: request
// errors name the URL, and they reach the logs and the page.
func (d *Deals) send(ctx context.Context, key string, r apiclient.Request, out any) error {
	r.Header = http.Header{"ITAD-API-Key": {key}}

	err := d.throttle.Do(ctx, func() error {
		return apiclient.ClassifyError(ctx, d.API.Do(ctx, r, out))
	})
	if apiclient.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden) {
		return ErrBadKey
	}

	return err
}

// dealsPage is one page of ITAD's deals list (the fields used).
type dealsPage struct {
	List []itemJSON `json:"list"`
}

// itemJSON is a game of the deals list with its deal.
type itemJSON struct {
	ID    string `json:"id"`
	Title string `json:"title"`

	// Type is "game", "dlc"... and null for some items.
	Type string `json:"type"`

	// Assets is an object of image URLs, but ITAD sends an empty array for a game without art:
	// it is decoded on its own so that never fails the list.
	Assets json.RawMessage `json:"assets"`

	Deal *dealJSON `json:"deal"`
}

// dealJSON is an item's current deal.
type dealJSON struct {
	Shop struct {
		Name string `json:"name"`
	} `json:"shop"`
	Price      priceJSON  `json:"price"`
	Regular    priceJSON  `json:"regular"`
	HistoryLow *priceJSON `json:"historyLow"`
	Cut        int        `json:"cut"`

	// Flag is "H" on a historical low in ITAD's sample; null otherwise.
	Flag string `json:"flag"`

	// Expiry is RFC 3339 with an offset, or null.
	Expiry string `json:"expiry"`
	URL    string `json:"url"`
}

// priceJSON is an amount; AmountInt is in the currency's minor unit, as game.Money.
type priceJSON struct {
	AmountInt int64  `json:"amountInt"`
	Currency  string `json:"currency"`
}

// atLow reports whether the deal is at the lowest price ITAD has seen: its flag says so, or its
// price is at or under the recorded low.
func (it itemJSON) atLow() bool {
	if it.Deal == nil {
		return false
	}

	low := it.Deal.HistoryLow

	return it.Deal.Flag == "H" || (low != nil && low.AmountInt > 0 && it.Deal.Price.AmountInt <= low.AmountInt)
}

// deal returns the item as a game.Deal, its data unchanged.
func (it itemJSON) deal(steamAppID string) game.Deal {
	out := game.Deal{
		ProviderGameID: it.ID,
		Title:          strings.TrimSpace(it.Title),
		SteamAppID:     steamAppID,
		Shop:           it.Deal.Shop.Name,
		Price:          game.Money{Amount: it.Deal.Price.AmountInt, Currency: it.Deal.Price.Currency},
		Regular:        game.Money{Amount: it.Deal.Regular.AmountInt, Currency: it.Deal.Regular.Currency},
		Cut:            it.Deal.Cut,
		HistoricalLow:  it.atLow(),
		URL:            httpsOnly(it.Deal.URL),
		ImageURL:       image(it.Assets),
	}

	if t, err := time.Parse(time.RFC3339, it.Deal.Expiry); err == nil {
		out.Expires = t
	}

	return out
}

// httpsOnly returns raw when it is an https address: the page turns it into a link.
func httpsOnly(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}

	return raw
}

// image returns the game's box art (else a banner) on ITAD's image host, "" when there is none.
func image(raw json.RawMessage) string {
	var assets struct {
		Boxart    string `json:"boxart"`
		Banner400 string `json:"banner400"`
	}

	if json.Unmarshal(raw, &assets) != nil {
		return "" // an empty array: no art
	}

	for _, candidate := range []string{assets.Boxart, assets.Banner400} {
		if u, err := url.Parse(candidate); err == nil && u.Scheme == "https" && u.Hostname() == imageHost {
			return candidate
		}
	}

	return ""
}
```

- [ ] **Step 6: Write the plugin** (`internal/adapters/outbound/itad/plugin.go`)

```go
package itad

import (
	"gamevault/internal/application/home"
	"gamevault/internal/application/plugin"
)

// Plugin is the IsThereAnyDeal plugin: the home page's current deals.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:    "itad",
		Name:  "IsThereAnyDeal",
		Deals: []home.DealsProvider{NewDeals()},
	}
}
```

- [ ] **Step 7: Register it** (`cmd/gamevault/plugins.go`: import `"gamevault/internal/adapters/outbound/itad"` and add after `steamgriddb.Plugin(),`)

```go
		// Deals
		itad.Plugin(),
```

- [ ] **Step 8: Translate its texts** (`web/src/i18n/locales/en.json` and `es.json`, inside `providers`, next to `steamgriddb`)

en:

```json
    "itad": {
      "description": "Current game deals for the home page: IsThereAnyDeal's featured deals and historical lows in your country, without the games you already own. Needs a free IsThereAnyDeal API key. Prices and links are shown as IsThereAnyDeal gives them.",
      "apiKey": "API key",
      "apiKeyHelp": "1. Sign in to IsThereAnyDeal (page above) and open your apps.\n2. Register an app (any name, for example `Game Vault`).\n3. Copy its API key here. It is only used from this computer, for your own use.",
      "country": "Country",
      "countryHelp": "Two-letter code of the country whose prices to show, for example `ES` or `US`. Empty: the one that goes with your default currency."
    },
```

es:

```json
    "itad": {
      "description": "Ofertas actuales para la página de inicio: las ofertas destacadas y los mínimos históricos de IsThereAnyDeal en tu país, sin los juegos que ya tienes. Necesita una clave gratuita de la API de IsThereAnyDeal. Precios y enlaces se muestran tal como los da IsThereAnyDeal.",
      "apiKey": "Clave de la API",
      "apiKeyHelp": "1. Inicia sesión en IsThereAnyDeal (página de arriba) y abre tus aplicaciones.\n2. Registra una aplicación (con cualquier nombre, por ejemplo `Game Vault`).\n3. Copia aquí su clave de la API. Solo se usa desde este ordenador, para tu uso personal.",
      "country": "País",
      "countryHelp": "Código de dos letras del país cuyos precios mostrar, por ejemplo `ES` o `US`. Vacío: el que corresponde a tu moneda por defecto."
    },
```

- [ ] **Step 9: Run the tests**

Run: `task go -- test ./internal/adapters/outbound/itad/ ./cmd/gamevault/ -v -run 'TestDeals|TestPlugins'`
Expected: PASS (`TestPlugins` checks the new plugin's translations and its place in the deals chain).

- [ ] **Step 10: Probe the real endpoint with a bogus key** (never a real one)

Run: `curl -sS -m 20 -w ' [%{http_code}]\n' -H 'ITAD-API-Key: bogus0000' 'https://api.isthereanydeal.com/deals/v2?limit=1'`
Expected: `{"status_code":403,"reason_phrase":"Invalid or expired api key"} [403]`, which the provider maps to `ErrBadKey`. If the shape changed, update `badKey` in `fake_test.go` and say so in the commit.

- [ ] **Step 11: Lint and the whole suite**

Run: `task lint:fix && task lint && task test`
Expected: 0 issues; all green.

- [ ] **Step 12: Commit**

```bash
git add internal/adapters/outbound/itad cmd/gamevault/plugins.go web/src/i18n/locales
git commit -m "Add the IsThereAnyDeal plugin: featured deals and historical lows with Steam ids"
```

### Task 5: IGDB releases

**Files:**
- Create: `internal/adapters/outbound/igdb/releases.go`
- Create: `internal/adapters/outbound/igdb/releases_test.go`
- Modify: `internal/adapters/outbound/igdb/fake_test.go` (two canned answers for `games`)
- Modify: `internal/adapters/outbound/igdb/plugin.go` (`Releases`)
- Modify: `web/src/i18n/locales/en.json`, `es.json` (`providers.igdbReleases.description`)

**Interfaces:**
- Consumes: `home.ReleasesProvider`, `home.OwnedRef` (Task 2); `game.Release` (Task 1); `provider.KindReleases`; the package's `Client.query`, `credentials`, `platformIDs`, `imageURL`, `reUID`, `steamSource`, `fields()`, `CoversID`.
- Produces: `igdb.ReleasesID provider.ID = "igdb-releases"`; `func igdb.NewReleases(c *igdb.Client) *igdb.Releases` (implements `home.ReleasesProvider` and `media.ImageHoster`); constants `maxAnticipated = 40`, `maxBatch = 500` (IGDB's largest `limit`).

IGDB facts (api-docs.igdb.com, read 2026-10-15 through its public documentation; no credentials used): `games` fields `first_release_date` (Unix time), `hypes` ("number of follows a game gets before release"), `franchise` (main franchise id), `franchises` (array), `collections` (array; `collection` is deprecated), `version_parent`, `parent_game`, `platforms` (array of ids), `cover`, `url`. `follows` and `category` are deprecated. The largest `limit` is 500. In Apicalypse, `field = (1,2)` on an array field matches games having any of the ids.

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Teach the fake IGDB the two release queries** (`internal/adapters/outbound/igdb/fake_test.go`)

Add two fields to `fakeIGDB` after `byID`:

```go
	// anticipated answers the anticipated releases query (sorted by hypes); series answers the
	// upcoming entries of series (sorted by date).
	anticipated string
	series      string
```

Initialize them in `newFakeIGDB` with `anticipated: "[]",` and `series: "[]",`. In `serve`, add two cases at the top of the final `switch`, before `case endpoint == "external_games":`:

```go
	case endpoint == "games" && strings.Contains(string(body), "sort hypes desc"):
		w.Write([]byte(f.anticipated))
	case endpoint == "games" && strings.Contains(string(body), "sort first_release_date asc"):
		w.Write([]byte(f.series))
```

- [ ] **Step 3: Write the failing tests** (`internal/adapters/outbound/igdb/releases_test.go`)

```go
package igdb

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// releaseNow is the clock of the release tests: 2026-10-15 00:00 UTC (Unix 1792022400).
var releaseNow = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

// Upcoming games as a games query with the release fields returns them (invented values).
const (
	harborLights = `{"id": 3001, "name": "Harbor Lights", "first_release_date": 1796083200,
  "url": "https://www.igdb.com/games/harbor-lights", "cover": {"id": 1, "image_id": "co-hl"},
  "platforms": [6, 167, 999], "franchises": [], "collections": []}`

	lanternKeepII = `{"id": 2001, "name": "Lantern Keep II", "first_release_date": 1795132800,
  "url": "https://www.igdb.com/games/lantern-keep-ii", "cover": {"id": 2, "image_id": "co-lk2"},
  "platforms": [6], "franchise": 40, "franchises": [40], "collections": [70]}`

	echoValleyRemake = `{"id": 2002, "name": "Echo Valley Remake", "first_release_date": 1796860800,
  "url": "https://www.igdb.com/games/echo-valley-remake", "platforms": [48, 167], "franchises": [41], "collections": []}`
)

func TestReleases_anticipated(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN IGDB knows one anticipated game", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.anticipated = "[" + harborLights + "]"
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the releases of the next 60 days are asked for", func(t *testing.T) {
			got, err := r.Anticipated(ctx, goodSettings, 60*24*time.Hour)
			require.NoError(t, err)

			t.Run("THEN one query asks for hyped main games in the window, most hyped first", func(t *testing.T) {
				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], "where first_release_date >= 1792022400 & first_release_date < 1797206400")
				assert.Contains(t, sent[0], "hypes > 0")
				assert.Contains(t, sent[0], "version_parent = null & parent_game = null")
				assert.Contains(t, sent[0], "sort hypes desc; limit 40;")
			})

			t.Run("AND the game comes with its date, known systems, cover and page", func(t *testing.T) {
				require.Len(t, got, 1)
				assert.Equal(t, game.Release{
					ProviderGameID: "3001",
					Title:          "Harbor Lights",
					Date:           "2026-12-01",
					Platforms:      []string{game.SystemPC, game.SystemPS5},
					CoverURL:       "https://images.igdb.com/igdb/image/upload/t_cover_big/co-hl.jpg",
					URL:            "https://www.igdb.com/games/harbor-lights",
				}, got[0])
			})
		})
	})

	t.Run("GIVEN no credentials", func(t *testing.T) {
		f := newFakeIGDB(t)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the anticipated releases are asked for", func(t *testing.T) {
			_, err := r.Anticipated(ctx, schema.Settings{}, 60*24*time.Hour)

			t.Run("THEN the settings are missing, without a request", func(t *testing.T) {
				assert.ErrorIs(t, err, provider.ErrBadSettings)
				assert.Empty(t, f.sent("games"))
			})
		})
	})
}

func TestReleases_sequels(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	owned := []home.OwnedRef{
		{Game: "g1", Title: "Lantern Keep", Links: game.Links{game.LinkSteam: "4410"}},
		{Game: "g2", Title: "Echo Valley", Links: game.Links{game.LinkSteam: "4420", game.LinkGOG: "1207658924"}},
		{Game: "g3", Title: "Copper Coast"}, // a disc: no store link
	}

	t.Run("GIVEN two owned games IGDB knows by their Steam apps, in a collection and a franchise", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.external = `[
  {"id": 5, "uid": "4410", "game": {"id": 1001, "franchise": 40, "franchises": [40], "collections": [70]}},
  {"id": 6, "uid": "4420", "game": {"id": 1002, "franchises": [41], "collections": []}}
]`
		// IGDB also lists an owned game itself as upcoming in its series when it is re-released:
		// 1001 must not come back.
		f.series = "[" + lanternKeepII + "," + echoValleyRemake + `,{"id": 1001, "name": "Lantern Keep", "first_release_date": 1794000000}]`
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the sequels of the owned games are asked for", func(t *testing.T) {
			got, err := r.Sequels(ctx, goodSettings, owned)
			require.NoError(t, err)

			t.Run("THEN the games are found by their Steam apps only, in one query", func(t *testing.T) {
				sent := f.sent("external_games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], `where external_game_source = 1 & uid = ("4410","4420");`)
				assert.Contains(t, sent[0], "game.franchise,game.franchises,game.collections")
			})

			t.Run("AND one query asks for upcoming main games of their series, soonest first", func(t *testing.T) {
				sent := f.sent("games")
				require.Len(t, sent, 1)
				assert.Contains(t, sent[0], "collections = (70) | franchises = (40,41) | franchise = (40,41)")
				assert.Contains(t, sent[0], "first_release_date > 1792022400")
				assert.Contains(t, sent[0], "sort first_release_date asc;")
			})

			t.Run("AND each release names the owned game it continues, the collection's before the franchise's", func(t *testing.T) {
				require.Len(t, got, 2)
				assert.Equal(t, "Lantern Keep II", got[0].Title)
				assert.Equal(t, game.ID("g1"), got[0].Because)
				assert.Equal(t, "Lantern Keep", got[0].BecauseTitle)
				assert.Equal(t, "Echo Valley Remake", got[1].Title)
				assert.Equal(t, game.ID("g2"), got[1].Because)
				assert.Equal(t, []string{game.SystemPS4, game.SystemPS5}, got[1].Platforms)
				assert.Empty(t, got[1].CoverURL)
			})
		})
	})

	t.Run("GIVEN no owned game has a Steam link", func(t *testing.T) {
		f := newFakeIGDB(t)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the sequels are asked for, for an empty collection too", func(t *testing.T) {
			got, err := r.Sequels(ctx, goodSettings, owned[2:])
			require.NoError(t, err)
			none, err := r.Sequels(ctx, goodSettings, nil)
			require.NoError(t, err)

			t.Run("THEN IGDB is not asked at all", func(t *testing.T) {
				assert.Empty(t, got)
				assert.Empty(t, none)
				assert.Empty(t, f.sent("external_games"))
				assert.Empty(t, f.sent("games"))
			})
		})
	})

	t.Run("GIVEN 501 owned games on Steam", func(t *testing.T) {
		f := newFakeIGDB(t)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		many := make([]home.OwnedRef, 501)
		for i := range many {
			many[i] = home.OwnedRef{
				Game:  game.ID(fmt.Sprintf("g%d", i)),
				Links: game.Links{game.LinkSteam: fmt.Sprint(10000 + i)},
			}
		}

		t.Run("WHEN the sequels are asked for", func(t *testing.T) {
			_, err := r.Sequels(ctx, goodSettings, many)
			require.NoError(t, err)

			t.Run("THEN they are looked up in batches of IGDB's largest limit", func(t *testing.T) {
				sent := f.sent("external_games")
				require.Len(t, sent, 2)
				assert.Equal(t, 500, strings.Count(sent[0], `"`)/2)
				assert.Equal(t, 1, strings.Count(sent[1], `"`)/2)
			})
		})
	})

	t.Run("GIVEN IGDB asks to slow down", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.setBusy(true)
		r := NewReleases(f.client(func() time.Time { return releaseNow }))

		t.Run("WHEN the sequels are asked for", func(t *testing.T) {
			_, err := r.Sequels(ctx, goodSettings, owned)

			t.Run("THEN the provider is rate-limited", func(t *testing.T) {
				assert.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})
}
```

The timestamps are 2026-12-01 (Harbor Lights), 2026-11-20 (Lantern Keep II) and 2026-12-10 (Echo Valley Remake) at 00:00 UTC; the anticipated window is `1792022400` (2026-10-15) to `1792022400 + 60 × 86400 = 1797206400`.

- [ ] **Step 4: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/igdb/ -run TestReleases -v`
Expected: FAIL to compile: `undefined: NewReleases`.

- [ ] **Step 5: Write the provider** (`internal/adapters/outbound/igdb/releases.go`)

```go
package igdb

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/application/home"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ReleasesID identifies IGDB in the releases chain. It shares the Twitch credentials with the cover
// and details providers (same settings group).
const ReleasesID provider.ID = "igdb-releases"

const (
	// maxAnticipated is how many anticipated releases are asked for.
	maxAnticipated = 40

	// maxBatch is IGDB's largest limit: Steam apps and series ids are sent this many at a time.
	maxBatch = 500
)

// releaseFields are the game fields a release card needs. Platforms stay ids: they are mapped to
// Game Vault's systems, unknown ones left out.
const releaseFields = "name,first_release_date,url,cover.image_id,platforms,franchise,franchises,collections"

// mainGames keeps editions and add-ons out: a deluxe edition or a DLC is not a new game.
const mainGames = "version_parent = null & parent_game = null"

// Releases implements home.ReleasesProvider: the most anticipated games of the next weeks, and the
// upcoming entries of the series the user's games belong to.
type Releases struct{ c *Client }

var (
	_ home.ReleasesProvider = (*Releases)(nil)
	_ media.ImageHoster     = (*Releases)(nil)
)

// NewReleases builds the releases provider on c, shared with the cover and details providers.
func NewReleases(c *Client) *Releases { return &Releases{c: c} }

// Descriptor implements providers.Impl.
func (r *Releases) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             ReleasesID,
		Kind:           provider.KindReleases,
		Name:           "IGDB",
		DescriptionKey: "providers.igdbReleases.description",
		Fields:         fields(),
		SettingsGroup:  string(CoversID),
		// Off until the user turns it on, even with the Twitch application entered: it runs on a
		// schedule (provider.Kind.Scheduled).
		DefaultOrder: 10,
	}
}

// Test implements providers.Impl.
func (r *Releases) Test(ctx context.Context, s schema.Settings) error { return r.c.Test(ctx, s) }

// ImageHosts implements media.ImageHoster.
func (r *Releases) ImageHosts() []string { return []string{"images.igdb.com"} }

// releaseJSON is a game as the release queries return it.
type releaseJSON struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	FirstReleaseDate int64  `json:"first_release_date"`
	URL              string `json:"url"`
	Cover            struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
	Platforms   []int   `json:"platforms"`
	Franchise   int64   `json:"franchise"`
	Franchises  []int64 `json:"franchises"`
	Collections []int64 `json:"collections"`
}

// Anticipated implements home.ReleasesProvider with one query: main games whose first release falls
// within window, with followers before release, most followed first.
func (r *Releases) Anticipated(ctx context.Context, s schema.Settings, window time.Duration) ([]game.Release, error) {
	from := r.c.now().Unix()
	to := from + int64(window/time.Second)

	body := fmt.Sprintf("fields %s; where first_release_date >= %d & first_release_date < %d & hypes > 0 & %s; sort hypes desc; limit %d;",
		releaseFields, from, to, mainGames, maxAnticipated)

	var out []releaseJSON
	if err := r.c.query(ctx, s, "games", body, &out); err != nil {
		return nil, err
	}

	releases := make([]game.Release, 0, len(out))
	for _, g := range out {
		releases = append(releases, g.release())
	}

	return releases, nil
}

// series is what the owned games' series map to: the owned game that explains each.
type series struct {
	collections map[int64]home.OwnedRef
	franchises  map[int64]home.OwnedRef

	// owned holds the IGDB ids of the owned games, so they are never listed as upcoming.
	owned map[int64]bool
}

// Sequels implements home.ReleasesProvider. The owned games are found by their Steam apps (IGDB's
// external games, source 1): the details cache keeps no IGDB ids, and Steam ids are the store ids
// whose format IGDB was checked against. Games without a Steam link do not contribute.
func (r *Releases) Sequels(ctx context.Context, s schema.Settings, owned []home.OwnedRef) ([]game.Release, error) {
	byApp := map[string]home.OwnedRef{}

	var apps []string

	for _, o := range owned {
		// The uid goes into the query: anything but a plain id is skipped rather than escaped.
		if app := o.Links[game.LinkSteam]; reUID.MatchString(app) && byApp[app].Game == "" {
			byApp[app] = o
			apps = append(apps, app)
		}
	}

	if len(apps) == 0 {
		return nil, nil
	}

	ser, err := r.seriesOf(ctx, s, apps, byApp)
	if err != nil || (len(ser.collections) == 0 && len(ser.franchises) == 0) {
		// No series known: an empty id list would be a syntax error, and there is nothing to ask.
		return nil, err
	}

	return r.upcoming(ctx, s, ser)
}

// seriesOf finds the collections and franchises of the owned games, in batches of maxBatch apps.
func (r *Releases) seriesOf(ctx context.Context, s schema.Settings, apps []string, byApp map[string]home.OwnedRef) (series, error) {
	ser := series{
		collections: map[int64]home.OwnedRef{},
		franchises:  map[int64]home.OwnedRef{},
		owned:       map[int64]bool{},
	}

	for batch := range slices.Chunk(apps, maxBatch) {
		quoted := make([]string, len(batch))
		for i, a := range batch {
			quoted[i] = strconv.Quote(a)
		}

		body := fmt.Sprintf("fields uid,game.franchise,game.franchises,game.collections; where external_game_source = %d & uid = (%s); limit %d;",
			steamSource, strings.Join(quoted, ","), maxBatch)

		var out []struct {
			UID  string       `json:"uid"`
			Game *releaseJSON `json:"game"`
		}
		if err := r.c.query(ctx, s, "external_games", body, &out); err != nil {
			return series{}, err
		}

		for _, e := range out {
			ref, ok := byApp[e.UID]
			if !ok || e.Game == nil {
				continue
			}

			ser.owned[e.Game.ID] = true

			for _, id := range e.Game.Collections {
				if _, seen := ser.collections[id]; !seen {
					ser.collections[id] = ref
				}
			}

			for _, id := range append(slices.Clone(e.Game.Franchises), e.Game.Franchise) {
				if _, seen := ser.franchises[id]; id != 0 && !seen {
					ser.franchises[id] = ref
				}
			}
		}
	}

	return ser, nil
}

// upcoming asks for the main games of the series that come out after now, soonest first, in
// batches of maxBatch ids of each kind, and names the owned game each continues: its collection's
// (the closer relation) before its franchise's.
func (r *Releases) upcoming(ctx context.Context, s schema.Settings, ser series) ([]game.Release, error) {
	collections := sortedKeys(ser.collections)
	franchises := sortedKeys(ser.franchises)
	now := r.c.now().Unix()

	var (
		out  []game.Release
		seen = map[int64]bool{}
	)

	for i := 0; i < max(len(collections), len(franchises)); i += maxBatch {
		var conds []string

		if ids := window(collections, i); ids != "" {
			conds = append(conds, "collections = ("+ids+")")
		}

		if ids := window(franchises, i); ids != "" {
			conds = append(conds, "franchises = ("+ids+") | franchise = ("+ids+")")
		}

		body := fmt.Sprintf("fields %s; where (%s) & first_release_date > %d & %s; sort first_release_date asc; limit %d;",
			releaseFields, strings.Join(conds, " | "), now, mainGames, maxBatch)

		var games []releaseJSON
		if err := r.c.query(ctx, s, "games", body, &games); err != nil {
			return nil, err
		}

		for _, g := range games {
			if ser.owned[g.ID] || seen[g.ID] {
				continue
			}

			ref, ok := because(g, ser)
			if !ok {
				continue
			}

			seen[g.ID] = true
			rel := g.release()
			rel.Because, rel.BecauseTitle = ref.Game, ref.Title
			out = append(out, rel)
		}
	}

	slices.SortStableFunc(out, func(a, b game.Release) int { return cmp.Compare(a.Date, b.Date) })

	return out, nil
}

// because returns the owned game a release continues: through a shared collection first.
func because(g releaseJSON, ser series) (home.OwnedRef, bool) {
	for _, id := range g.Collections {
		if ref, ok := ser.collections[id]; ok {
			return ref, true
		}
	}

	for _, id := range append(slices.Clone(g.Franchises), g.Franchise) {
		if ref, ok := ser.franchises[id]; ok {
			return ref, true
		}
	}

	return home.OwnedRef{}, false
}

// sortedKeys returns the ids in increasing order, so queries are the same from one day to the next.
func sortedKeys(m map[int64]home.OwnedRef) []int64 {
	out := make([]int64, 0, len(m))
	for id := range m {
		out = append(out, id)
	}

	slices.Sort(out)

	return out
}

// window returns ids[i:i+maxBatch] joined by commas, "" when that part is empty.
func window(ids []int64, i int) string {
	if i >= len(ids) {
		return ""
	}

	part := ids[i:min(i+maxBatch, len(ids))]
	out := make([]string, len(part))

	for j, id := range part {
		out[j] = strconv.FormatInt(id, 10)
	}

	return strings.Join(out, ",")
}

// systemByPlatform maps IGDB platform ids back to Game Vault systems.
var systemByPlatform = func() map[int]string {
	out := map[int]string{}

	for system, ids := range platformIDs {
		for _, id := range ids {
			out[id] = system
		}
	}

	return out
}()

// release returns the game as a game.Release; Because is the caller's to fill.
func (g releaseJSON) release() game.Release {
	out := game.Release{
		ProviderGameID: strconv.FormatInt(g.ID, 10),
		Title:          strings.TrimSpace(g.Name),
		URL:            g.URL,
	}

	if g.FirstReleaseDate != 0 {
		out.Date = game.DateOf(time.Unix(g.FirstReleaseDate, 0).UTC())
	}

	if g.Cover.ImageID != "" {
		out.CoverURL = imageURL("cover_big", g.Cover.ImageID)
	}

	for _, id := range g.Platforms {
		if system, ok := systemByPlatform[id]; ok && !slices.Contains(out.Platforms, system) {
			out.Platforms = append(out.Platforms, system)
		}
	}

	return out
}
```

`game.DateOf` formats with `time.DateOnly` (`internal/domain/game/values.go`). `slices.Chunk` exists since Go 1.23.

- [ ] **Step 6: Add it to the plugin** (`internal/adapters/outbound/igdb/plugin.go`: import `"gamevault/internal/application/home"`, update the doc comment to "game sheets, covers and upcoming releases", and add the field)

```go
		Releases: []home.ReleasesProvider{NewReleases(c)},
```

- [ ] **Step 7: Translate its description** (inside `providers`, next to `igdbDetails`)

en: `"igdbReleases": { "description": "Upcoming games for the home page: the next entries of the series your games belong to (found through their Steam apps), and the most anticipated releases of the next 60 days. Uses the Twitch application from Covers; a few requests a day. IGDB receives the Steam ids of your games to find their series." },`

es: `"igdbReleases": { "description": "Próximos lanzamientos para la página de inicio: las siguientes entregas de las series a las que pertenecen tus juegos (encontradas por sus aplicaciones de Steam) y los lanzamientos más esperados de los próximos 60 días. Usa la aplicación de Twitch de Portadas; unas pocas peticiones al día. IGDB recibe los identificadores de Steam de tus juegos para encontrar sus series." },`

- [ ] **Step 8: Run the tests**

Run: `task go -- test ./internal/adapters/outbound/igdb/ ./cmd/gamevault/ -v -run 'TestReleases|TestPlugins|TestDetails|TestCovers'`
Expected: PASS (the existing IGDB tests still pass with the two new fake cases).

- [ ] **Step 9: Lint and the whole suite**

Run: `task lint:fix && task lint && task test`
Expected: 0 issues; all green.

- [ ] **Step 10: Commit**

```bash
git add internal/adapters/outbound/igdb web/src/i18n/locales
git commit -m "Add IGDB releases: anticipated games and the next entries of owned series"
```

### Task 6: The home service: reading, refreshing, and the owned-deal filter

**Files:**
- Create: `internal/application/home/options.go`, `service.go`, `owned.go`
- Create: `internal/application/home/fakes_test.go`, `service_test.go`, `internal_test.go`

**Interfaces:**
- Consumes: everything in `home/ports.go` (Task 2); `providers.View`, `providers.Impl`; `game.Deal`, `game.Release`, `game.SameTitle` via `game.TitleKey`, `game.Copy.Held`, `game.Copy.System`; `settings.Preferences`; `unattended.Rhythm`; `errkind`.
- Produces (package `home`):
  - ports `Configs { Enabled(ctx, provider.Kind) ([]providers.View, error); ReportResult(provider.ID, error) }`, `Catalog { List(ctx) ([]*game.Game, error) }`, `Preferences { Preferences(ctx) (settings.Preferences, error) }`
  - `func NewService(configs Configs, games Catalog, snaps Snapshots, prefs Preferences, opts ...Option) *Service`
  - `type Option func(*Service)`; `WithDeals(...DealsProvider)`, `WithReleases(...ReleasesProvider)`, `WithClock(port.Clock)`, `WithLogger(*slog.Logger)`, `WithRhythm(unattended.Rhythm)`
  - `type State string`: `StateFresh = "fresh"`, `StateStale = "stale"`, `StateNotConfigured = "not_configured"`, `StateNeverFetched = "never_fetched"`
  - `type BlockView struct { State State; Provider string; FetchedAt time.Time; FailedAt time.Time; Error string; ErrorKind ErrorKind; RefreshableAt time.Time }`
  - `type Home struct { Deals BlockView; Featured []game.Deal; HistoricalLows []game.Deal; Releases BlockView; FromSeries []game.Release; Anticipated []game.Release }`
  - `func (s *Service) Home(ctx context.Context) (Home, error)`
  - `func (s *Service) Refresh(ctx context.Context, b Block) (Home, error)`
  - sentinels `ErrUnknownBlock` (`errkind.Invalid`), `ErrTooSoon` (`errkind.Exhausted`), `ErrNotConfigured` (`errkind.Precondition`)
  - unexported for Task 7: `(s *Service) refresh(ctx, b Block) error` (no guard, caller holds `s.mu`), `every(b Block) time.Duration`, `lastAttempt(Snapshot) time.Time`, field `s.mu sync.Mutex`, `s.rhythm unattended.Rhythm`, `s.log *slog.Logger`, `s.now port.Clock`

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the test fakes** (`internal/application/home/fakes_test.go`)

```go
package home_test

import (
	"context"
	"time"

	"gamevault/internal/application/home"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/settings"
)

var start = time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)

// clock is a settable clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// fakeDeals is a deals provider with canned lists that records what it was asked.
type fakeDeals struct {
	featured []game.Deal
	lows     []game.Deal
	err      error
	calls    []home.DealQuery
}

func (f *fakeDeals) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:           "deals-co",
		Kind:         provider.KindDeals,
		Name:         "Deals Co",
		DefaultOrder: 10,
	}
}

func (f *fakeDeals) Test(context.Context, schema.Settings) error { return nil }

func (f *fakeDeals) Deals(_ context.Context, _ schema.Settings, q home.DealQuery) ([]game.Deal, error) {
	f.calls = append(f.calls, q)
	if f.err != nil {
		return nil, f.err
	}

	if q.List == home.DealsFeatured {
		return f.featured, nil
	}

	return f.lows, nil
}

// fakeReleases is a releases provider with canned lists that records what it was asked.
type fakeReleases struct {
	anticipated []game.Release
	series      []game.Release
	err         error
	windows     []time.Duration
	owned       [][]home.OwnedRef
}

func (f *fakeReleases) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:           "db-releases",
		Kind:         provider.KindReleases,
		Name:         "Game DB",
		DefaultOrder: 10,
	}
}

func (f *fakeReleases) Test(context.Context, schema.Settings) error { return nil }

func (f *fakeReleases) Anticipated(_ context.Context, _ schema.Settings, window time.Duration) ([]game.Release, error) {
	f.windows = append(f.windows, window)
	return f.anticipated, f.err
}

func (f *fakeReleases) Sequels(_ context.Context, _ schema.Settings, owned []home.OwnedRef) ([]game.Release, error) {
	f.owned = append(f.owned, owned)
	return f.series, f.err
}

func (f *fakeReleases) calls() int { return len(f.windows) + len(f.owned) }

// configs is the providers registry as the home service sees it: the enabled providers of each
// kind, and the answers reported.
type configs struct {
	enabled map[provider.Kind][]providers.View
	results []error
}

func (c *configs) Enabled(_ context.Context, kind provider.Kind) ([]providers.View, error) {
	return c.enabled[kind], nil
}

func (c *configs) ReportResult(_ provider.ID, err error) { c.results = append(c.results, err) }

// enable makes p an enabled provider of its kind.
func (c *configs) enable(p providers.Impl) {
	d := p.Descriptor()
	cfg := provider.New(d, 0, start)
	_ = cfg.Configure(d, true, schema.Settings{}, start)

	if c.enabled == nil {
		c.enabled = map[provider.Kind][]providers.View{}
	}

	c.enabled[d.Kind] = append(c.enabled[d.Kind], providers.View{Provider: cfg, Descriptor: d})
}

func (c *configs) disable(kind provider.Kind) { delete(c.enabled, kind) }

// snapshots is an in-memory home.Snapshots.
type snapshots map[home.Block]home.Snapshot

func (m snapshots) Snapshot(_ context.Context, b home.Block) (home.Snapshot, bool, error) {
	s, ok := m[b]
	if !ok {
		return home.Snapshot{Block: b}, false, nil
	}

	return s, true, nil
}

func (m snapshots) SaveSnapshot(_ context.Context, s home.Snapshot) error {
	m[s.Block] = s
	return nil
}

// catalog is an in-memory home.Catalog.
type catalog []*game.Game

func (c catalog) List(context.Context) ([]*game.Game, error) { return c, nil }

// prefs is the user's saved preferences.
type prefs settings.Preferences

func (p prefs) Preferences(context.Context) (settings.Preferences, error) { return settings.Preferences(p), nil }
```

- [ ] **Step 3: Write the failing service tests** (`internal/application/home/service_test.go`)

```go
package home_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// newGame builds a game with its links and copies. Every title is invented.
func newGame(t *testing.T, title string, links game.Links, copies ...game.CopyDetails) *game.Game {
	t.Helper()

	g, err := game.New(title, start)
	require.NoError(t, err)

	if links != nil {
		_, err = g.UpdateInfo(game.Info{Title: title, Links: links}, start)
		require.NoError(t, err)
	}

	for _, d := range copies {
		_, err := g.AddCopy(d, start)
		require.NoError(t, err)
	}

	return g
}

var (
	steamLibrary = game.CopyDetails{Kind: game.KindLibrary, Platform: "Steam"}
	gogLibrary   = game.CopyDetails{Kind: game.KindLibrary, Platform: "GOG"}
	ps4Disc      = game.CopyDetails{Kind: game.KindPhysical, Platform: "PS4"}
	soldDisc     = game.CopyDetails{Kind: game.KindPhysical, Platform: "PS4", Status: game.StatusSold}
)

func deal(title, steam string) game.Deal {
	return game.Deal{
		ProviderGameID: "itad-" + title,
		Title:          title,
		SteamAppID:     steam,
		Shop:           "Steam",
		Price:          game.Money{Amount: 499, Currency: "EUR"},
		Regular:        game.Money{Amount: 1999, Currency: "EUR"},
		Cut:            75,
	}
}

func titles(deals []game.Deal) []string {
	out := []string{}
	for _, d := range deals {
		out = append(out, d.Title)
	}

	return out
}

func TestHome_states(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN providers compiled in but none enabled", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		clk := &clock{start}
		svc := home.NewService(&configs{}, catalog{}, snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now))

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN both blocks say they are not configured, and nothing was asked", func(t *testing.T) {
				assert.Equal(t, home.StateNotConfigured, h.Deals.State)
				assert.Equal(t, home.StateNotConfigured, h.Releases.State)
				assert.Empty(t, deals.calls)
				assert.Zero(t, releases.calls())
			})
		})
	})

	t.Run("GIVEN both providers enabled and nothing gathered yet", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock((&clock{start}).now))

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN the blocks were never fetched, and can be refreshed at once", func(t *testing.T) {
				assert.Equal(t, home.StateNeverFetched, h.Deals.State)
				assert.Equal(t, "Deals Co", h.Deals.Provider)
				assert.True(t, h.Deals.RefreshableAt.IsZero())
				assert.Equal(t, home.StateNeverFetched, h.Releases.State)
			})

			t.Run("AND reading never calls a provider (with -no-unattended, nothing else would)", func(t *testing.T) {
				assert.Empty(t, deals.calls)
				assert.Zero(t, releases.calls())
			})
		})
	})

	t.Run("GIVEN deals stored, then ITAD turned off", func(t *testing.T) {
		deals := &fakeDeals{featured: []game.Deal{deal("Echo Valley", "")}}
		cfg := &configs{}
		cfg.enable(deals)
		clk := &clock{start}
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)
		cfg.disable(provider.KindDeals)

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN the block is not configured and the stored deals are not shown as current", func(t *testing.T) {
				assert.Equal(t, home.StateNotConfigured, h.Deals.State)
				assert.Empty(t, h.Featured)
			})
		})
	})

	t.Run("GIVEN deals fetched three days ago, without an error since", func(t *testing.T) {
		deals := &fakeDeals{featured: []game.Deal{deal("Echo Valley", "")}}
		cfg := &configs{}
		cfg.enable(deals)
		clk := &clock{start}
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)
		clk.advance(72 * time.Hour) // the server was off

		t.Run("WHEN the home page is read", func(t *testing.T) {
			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN it shows the last data as stale, with its age", func(t *testing.T) {
				assert.Equal(t, home.StateStale, h.Deals.State)
				assert.Equal(t, start, h.Deals.FetchedAt)
				assert.Empty(t, h.Deals.Error)
				assert.Equal(t, []string{"Echo Valley"}, titles(h.Featured))
			})
		})
	})
}

func TestRefresh_deals(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a collection and the deals of the day", func(t *testing.T) {
		games := catalog{
			newGame(t, "Lantern Keep", game.Links{game.LinkSteam: "4410"}, steamLibrary),
			newGame(t, "Echo Valley", nil, gogLibrary),        // owned on PC, not on Steam
			newGame(t, "Harbor Lights", nil, ps4Disc),         // only on a console
			newGame(t, "Copper Coast", nil, soldDisc),         // sold: not owned any more
			newGame(t, "星の灯台", nil, steamLibrary),            // a title in another script
			newGame(t, "Driftwood", game.Links{}, gogLibrary), // owned, no Steam link
		}
		deals := &fakeDeals{
			featured: []game.Deal{
				deal("Lantern Keep: Definitive", "4410"), // the same Steam app under another name
				deal("ECHO VALLEY™", ""),
				deal("Harbor Lights", "4430"),
				deal("Copper Coast", ""),
				deal("星の港", ""),
				deal("星の灯台", ""),
				deal("Driftwood Tales", ""), // a deal without a Steam id, for another game
			},
			lows: []game.Deal{deal("Harbor Lights", "4430")},
		}
		cfg := &configs{}
		cfg.enable(deals)
		svc := home.NewService(cfg, games, snapshots{}, prefs{Currency: "EUR"}, home.WithDeals(deals), home.WithClock((&clock{start}).now))

		t.Run("WHEN the deals block is refreshed", func(t *testing.T) {
			h, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN both lists are asked for, in the country of the preferred currency", func(t *testing.T) {
				assert.Equal(t, []home.DealQuery{
					{List: home.DealsFeatured, Country: "DE", Limit: 40},
					{List: home.DealsHistoricalLows, Country: "DE", Limit: 40},
				}, deals.calls)
			})

			t.Run("AND the block is fresh, and the answer reported to the registry", func(t *testing.T) {
				assert.Equal(t, home.StateFresh, h.Deals.State)
				assert.Equal(t, start, h.Deals.FetchedAt)
				assert.Equal(t, start.Add(15*time.Minute), h.Deals.RefreshableAt)
				assert.Equal(t, []error{nil}, cfg.results)
			})

			t.Run("AND games owned on PC are left out, by Steam id or by exact title", func(t *testing.T) {
				assert.Equal(t, []string{"Harbor Lights", "Copper Coast", "星の港", "Driftwood Tales"}, titles(h.Featured))
			})

			t.Run("AND a game owned only on a console stays, with where the user has it", func(t *testing.T) {
				assert.Equal(t, []string{game.SystemPS4}, h.Featured[0].OwnedOn)
				assert.Empty(t, h.Featured[1].OwnedOn, "a sold copy is not owned")
			})

			t.Run("AND titles in another script match only the same title", func(t *testing.T) {
				assert.Contains(t, titles(h.Featured), "星の港")
				assert.NotContains(t, titles(h.Featured), "星の灯台")
			})

			t.Run("AND a deal without a Steam id is never matched through an empty id", func(t *testing.T) {
				assert.Contains(t, titles(h.Featured), "Driftwood Tales")
			})
		})

		t.Run("WHEN the user then buys a game on sale and the page is read again", func(t *testing.T) {
			// The service reads the same slice: replacing the sold copy's game is what a purchase
			// imported by a scan looks like to it.
			games[3] = newGame(t, "Copper Coast", nil, steamLibrary)

			h, err := svc.Home(ctx)
			require.NoError(t, err)

			t.Run("THEN its deal is gone without waiting for the next refresh", func(t *testing.T) {
				assert.NotContains(t, titles(h.Featured), "Copper Coast")
				assert.Len(t, deals.calls, 2, "no provider was asked again")
			})
		})
	})

	t.Run("GIVEN more unowned deals than the page shows", func(t *testing.T) {
		many := &fakeDeals{}
		for i := range 20 {
			many.featured = append(many.featured, deal(fmt.Sprintf("Example %d", i), ""))
		}

		cfg := &configs{}
		cfg.enable(many)
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{}, home.WithDeals(many), home.WithClock((&clock{start}).now))

		t.Run("WHEN the block is refreshed without a preferred currency", func(t *testing.T) {
			h, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN twelve are shown, in the provider's order, and the United States' prices asked for", func(t *testing.T) {
				require.Len(t, h.Featured, 12)
				assert.Equal(t, "Example 0", h.Featured[0].Title)
				assert.Equal(t, "US", many.calls[0].Country)
			})
		})
	})

	t.Run("GIVEN an empty collection", func(t *testing.T) {
		deals := &fakeDeals{featured: []game.Deal{deal("Echo Valley", "")}}
		releases := &fakeReleases{anticipated: []game.Release{{Title: "Harbor Lights", Date: "2026-12-01"}}}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock((&clock{start}).now))

		t.Run("WHEN both blocks are refreshed", func(t *testing.T) {
			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)
			h, err := svc.Refresh(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN the deals are all shown, and the series not asked for", func(t *testing.T) {
				assert.Equal(t, []string{"Echo Valley"}, titles(h.Featured))
				assert.Empty(t, releases.owned)
				assert.Empty(t, h.FromSeries)
				assert.Len(t, h.Anticipated, 1)
			})
		})
	})
}

func TestRefresh_failureKeepsLastGoodData(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN deals gathered six hours ago", func(t *testing.T) {
		deals := &fakeDeals{featured: []game.Deal{deal("Echo Valley", "")}}
		cfg := &configs{}
		cfg.enable(deals)
		clk := &clock{start}
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)
		clk.advance(6 * time.Hour)

		t.Run("WHEN the service is down at the next refresh", func(t *testing.T) {
			down := fmt.Errorf("not answering (HTTP 503): %w", provider.ErrUnavailable)
			deals.err = down

			h, err := svc.Refresh(ctx, home.BlockDeals)

			t.Run("THEN the refresh itself does not fail: the block says what happened", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, home.StateStale, h.Deals.State)
				assert.Equal(t, home.ErrorUnavailable, h.Deals.ErrorKind)
				assert.Contains(t, h.Deals.Error, "HTTP 503")
				assert.Equal(t, start.Add(6*time.Hour), h.Deals.FailedAt)
			})

			t.Run("AND the last good deals stay, with their age", func(t *testing.T) {
				assert.Equal(t, start, h.Deals.FetchedAt)
				assert.Equal(t, []string{"Echo Valley"}, titles(h.Featured))
			})

			t.Run("AND the problem reaches the provider's status (System → Health)", func(t *testing.T) {
				assert.ErrorIs(t, cfg.results[len(cfg.results)-1], provider.ErrUnavailable)
			})
		})

		t.Run("WHEN it answers again later", func(t *testing.T) {
			deals.err = nil
			clk.advance(time.Hour)

			h, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN the error is gone", func(t *testing.T) {
				assert.Equal(t, home.StateFresh, h.Deals.State)
				assert.Empty(t, h.Deals.Error)
			})
		})
	})
}

func TestRefresh_guard(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN deals refreshed a moment ago", func(t *testing.T) {
		deals := &fakeDeals{}
		cfg := &configs{}
		cfg.enable(deals)
		clk := &clock{start}
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{}, home.WithDeals(deals), home.WithClock(clk.now))

		_, err := svc.Refresh(ctx, home.BlockDeals)
		require.NoError(t, err)

		t.Run("WHEN the user asks again ten minutes later", func(t *testing.T) {
			clk.advance(10 * time.Minute)
			_, err := svc.Refresh(ctx, home.BlockDeals)

			t.Run("THEN it is refused without a request", func(t *testing.T) {
				assert.ErrorIs(t, err, home.ErrTooSoon)
				assert.Len(t, deals.calls, 2, "only the first refresh's two lists")
			})
		})

		t.Run("WHEN the user asks again fifteen minutes after the first, and it fails", func(t *testing.T) {
			clk.advance(5 * time.Minute)
			deals.err = provider.ErrUnavailable

			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN the failed attempt counts too: the next one waits fifteen minutes", func(t *testing.T) {
				clk.advance(time.Minute)
				_, err := svc.Refresh(ctx, home.BlockDeals)
				assert.ErrorIs(t, err, home.ErrTooSoon)
			})
		})
	})

	t.Run("GIVEN no deals provider enabled", func(t *testing.T) {
		svc := home.NewService(&configs{}, catalog{}, snapshots{}, prefs{}, home.WithDeals(&fakeDeals{}))

		t.Run("WHEN the user refreshes deals, or a block that does not exist", func(t *testing.T) {
			_, notConfigured := svc.Refresh(ctx, home.BlockDeals)
			_, unknown := svc.Refresh(ctx, "weather")

			t.Run("THEN they are told to enable a provider, or that there is no such block", func(t *testing.T) {
				assert.ErrorIs(t, notConfigured, home.ErrNotConfigured)
				assert.ErrorIs(t, unknown, home.ErrUnknownBlock)
			})
		})
	})
}

func TestRefresh_releases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an owned game, a sold one, and the releases a database knows", func(t *testing.T) {
		keep := newGame(t, "Lantern Keep", game.Links{game.LinkSteam: "4410"}, steamLibrary)
		games := catalog{keep, newGame(t, "Copper Coast", game.Links{game.LinkSteam: "4420"}, soldDisc)}
		releases := &fakeReleases{
			series: []game.Release{
				{Title: "Lantern Keep: Origins", Date: "2026-10-01", Because: keep.ID(), BecauseTitle: "Lantern Keep"}, // out already
				{Title: "Lantern Keep II", Date: "2026-11-20", Because: keep.ID(), BecauseTitle: "Lantern Keep"},
				{Title: "Copper Coast 2", Date: "2026-11-01", Because: "deleted-game", BecauseTitle: "Copper Coast"},
			},
			anticipated: []game.Release{
				{Title: "Harbor Lights", Date: "2026-12-01"},
				{Title: "Echo Valley", Date: "2026-10-14"},
			},
		}
		cfg := &configs{}
		cfg.enable(releases)
		svc := home.NewService(cfg, games, snapshots{}, prefs{}, home.WithReleases(releases), home.WithClock((&clock{start}).now))

		t.Run("WHEN the releases block is refreshed", func(t *testing.T) {
			h, err := svc.Refresh(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN the next 60 days are asked for, and the series of the games still owned", func(t *testing.T) {
				assert.Equal(t, []time.Duration{60 * 24 * time.Hour}, releases.windows)
				require.Len(t, releases.owned, 1)
				assert.Equal(t, []home.OwnedRef{{Game: keep.ID(), Title: "Lantern Keep", Links: game.Links{game.LinkSteam: "4410"}}}, releases.owned[0])
			})

			t.Run("AND releases already out, or explained by a game no longer in the catalog, are left out", func(t *testing.T) {
				require.Len(t, h.FromSeries, 1)
				assert.Equal(t, "Lantern Keep II", h.FromSeries[0].Title)
				require.Len(t, h.Anticipated, 1)
				assert.Equal(t, "Harbor Lights", h.Anticipated[0].Title)
			})
		})
	})
}
```

And the internal tests (`internal/application/home/internal_test.go`):

```go
package home

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"gamevault/internal/domain/provider"
)

func TestCountryFor(t *testing.T) {
	for currency, want := range map[string]string{
		"EUR": "DE", "USD": "US", "GBP": "GB", "JPY": "JP", "BRL": "BR", "eur": "DE", "": "", "XYZ": "",
	} {
		assert.Equal(t, want, countryFor(currency), currency)
	}
}

func TestKindOf(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want ErrorKind
	}{
		{provider.ErrQuotaExhausted, ErrorQuota},
		{provider.BadSettings("check the key"), ErrorSettings},
		{provider.ErrUnavailable, ErrorUnavailable},
		{provider.ErrRateLimited, ErrorRateLimited},
		{fmt.Errorf("unexpected answer"), ErrorFailed},
	} {
		assert.Equal(t, tc.want, kindOf(fmt.Errorf("deals: %w", tc.err)), tc.err.Error())
	}
}
```

- [ ] **Step 4: Run them to see them fail**

Run: `task go -- test ./internal/application/home/ -v`
Expected: FAIL to compile: `undefined: home.NewService`, `undefined: countryFor`.

- [ ] **Step 5: Write the options** (`internal/application/home/options.go`)

```go
package home

import (
	"log/slog"

	"gamevault/internal/application/port"
	"gamevault/internal/application/unattended"
)

// Option configures a Service.
type Option func(*Service)

// WithDeals sets the deals providers compiled in (none by default).
func WithDeals(ps ...DealsProvider) Option {
	return func(s *Service) {
		for _, p := range ps {
			s.deals[p.Descriptor().ID] = p
		}
	}
}

// WithReleases sets the releases providers compiled in (none by default).
func WithReleases(ps ...ReleasesProvider) Option {
	return func(s *Service) {
		for _, p := range ps {
			s.releases[p.Descriptor().ID] = p
		}
	}
}

// WithClock sets the clock (time.Now by default).
func WithClock(now port.Clock) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger (discarded by default).
func WithLogger(log *slog.Logger) Option { return func(s *Service) { s.log = log } }

// WithRhythm sets how the job draws and waits its pauses (random and a real timer by default);
// tests make it deterministic.
func WithRhythm(r unattended.Rhythm) Option { return func(s *Service) { s.rhythm = r } }
```

- [ ] **Step 6: Write the service** (`internal/application/home/service.go`)

```go
package home

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/settings"
)

// Errors returned by the service.
var (
	// ErrUnknownBlock means the block asked for is not deals or releases.
	ErrUnknownBlock = errkind.New(errkind.Invalid, "unknown home block: refresh deals or releases")

	// ErrTooSoon means the block was refreshed (or tried) less than 15 minutes ago.
	ErrTooSoon = errkind.New(errkind.Exhausted, "this block was refreshed less than 15 minutes ago: try again in a few minutes")

	// ErrNotConfigured means no provider of the block's kind is enabled.
	ErrNotConfigured = errkind.New(errkind.Precondition, "no provider is enabled for this block: enable one on the Providers page")
)

const (
	// dealsEvery and releasesEvery are the job's intervals (each varied ±10 %): deals change
	// within the day, release dates rarely.
	dealsEvery    = 6 * time.Hour
	releasesEvery = 24 * time.Hour

	// manualGap is the shortest time between two refreshes the user asks for, counted from the
	// last attempt, failed ones included: the services ask for restraint.
	manualGap = 15 * time.Minute

	// fetchedDeals is how many deals of each list are gathered, so enough remain once the owned
	// games are left out; shownDeals is how many the page shows.
	fetchedDeals = 40
	shownDeals   = 12

	// anticipationWindow is how far ahead the most anticipated releases are looked for.
	anticipationWindow = 60 * 24 * time.Hour

	// refreshTimeout bounds one block's refresh, every provider of its chain included.
	refreshTimeout = 2 * time.Minute
)

// Configs is the port that keeps the providers' configuration and status (implemented by the
// providers registry).
type Configs interface {
	// Enabled returns the enabled providers of a kind in priority order.
	Enabled(ctx context.Context, kind provider.Kind) ([]providers.View, error)

	// ReportResult records what a provider's answer says about the provider: a provider-level
	// problem becomes its status on System → Health, an answer clears it.
	ReportResult(id provider.ID, err error)
}

// Catalog is the port that reads the games. A game.Repository satisfies it.
type Catalog interface {
	// List returns every game with its copies.
	List(ctx context.Context) ([]*game.Game, error)
}

// Preferences is the port that reads the user's preferences (the default currency, which gives
// the deals' country). A settings.Repository satisfies it.
type Preferences interface {
	// Preferences returns the saved preferences, empty when none were saved.
	Preferences(ctx context.Context) (settings.Preferences, error)
}

// State is what an external block can show.
type State string

// Values of State.
const (
	StateFresh         State = "fresh"          // recent data, the last attempt worked
	StateStale         State = "stale"          // the last attempt failed, or the data is old
	StateNotConfigured State = "not_configured" // no provider enabled: the stored data is hidden
	StateNeverFetched  State = "never_fetched"  // a provider is enabled, nothing gathered yet
)

// BlockView is an external block's state as the page shows it.
type BlockView struct {
	State State

	// Provider names the first enabled provider ("IsThereAnyDeal"), for the messages and the
	// credit; "" when none is.
	Provider string

	// FetchedAt is when the data shown was gathered; FailedAt, Error and ErrorKind describe the
	// last attempt when it failed.
	FetchedAt time.Time
	FailedAt  time.Time
	Error     string
	ErrorKind ErrorKind

	// RefreshableAt is when the user may refresh the block again; zero: now.
	RefreshableAt time.Time
}

// Home is the home page's external blocks, ready to show.
type Home struct {
	Deals          BlockView
	Featured       []game.Deal
	HistoricalLows []game.Deal

	Releases    BlockView
	FromSeries  []game.Release
	Anticipated []game.Release
}

// Service gathers and serves the home page's external blocks.
type Service struct {
	configs  Configs
	games    Catalog
	snaps    Snapshots
	prefs    Preferences
	deals    map[provider.ID]DealsProvider
	releases map[provider.ID]ReleasesProvider
	now      port.Clock
	log      *slog.Logger

	// rhythm draws and waits the job's pauses (see Run).
	rhythm unattended.Rhythm

	// mu makes refreshes one at a time: the job's and the user's never overlap, and a user's
	// refresh right after the job's is refused by the guard instead of asking again.
	mu sync.Mutex
}

// NewService builds the service. Without providers (WithDeals, WithReleases) both blocks say they
// are not configured.
func NewService(configs Configs, games Catalog, snaps Snapshots, prefs Preferences, opts ...Option) *Service {
	s := &Service{
		configs:  configs,
		games:    games,
		snaps:    snaps,
		prefs:    prefs,
		deals:    map[provider.ID]DealsProvider{},
		releases: map[provider.ID]ReleasesProvider{},
		now:      time.Now,
		log:      slog.New(slog.DiscardHandler),
	}
	for _, o := range opts {
		o(s)
	}

	return s
}

// kindOfBlock is the provider kind that feeds each block.
var kindOfBlock = map[Block]provider.Kind{
	BlockDeals:    provider.KindDeals,
	BlockReleases: provider.KindReleases,
}

// every returns the job's interval for a block.
func every(b Block) time.Duration {
	if b == BlockDeals {
		return dealsEvery
	}

	return releasesEvery
}

// lastAttempt returns when the block was last refreshed or tried; zero when never.
func lastAttempt(s Snapshot) time.Time {
	if s.FailedAt.After(s.FetchedAt) {
		return s.FailedAt
	}

	return s.FetchedAt
}

// enabled returns the block's enabled providers that are compiled in, in the user's order.
func (s *Service) enabled(ctx context.Context, b Block) ([]providers.View, error) {
	views, err := s.configs.Enabled(ctx, kindOfBlock[b])
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(views, func(v providers.View) bool {
		if b == BlockDeals {
			return s.deals[v.ID()] == nil
		}

		return s.releases[v.ID()] == nil
	}), nil
}

// Home returns the external blocks. It only reads: no provider is asked, so a server started
// with -no-unattended never reaches a service on its own. Games owned on PC are left out of the
// deals now, not when they were gathered, so a game bought since is gone at once.
func (s *Service) Home(ctx context.Context) (Home, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return Home{}, err
	}

	var out Home

	view, snap, err := s.block(ctx, BlockDeals)
	if err != nil {
		return Home{}, err
	}

	out.Deals = view
	if view.State != StateNotConfigured {
		owned := newOwnedIndex(games)
		out.Featured = owned.filter(snap.Featured, shownDeals)
		out.HistoricalLows = owned.filter(snap.HistoricalLows, shownDeals)
	}

	view, snap, err = s.block(ctx, BlockReleases)
	if err != nil {
		return Home{}, err
	}

	out.Releases = view
	if view.State != StateNotConfigured {
		today := game.DateOf(s.now())
		ids := map[game.ID]bool{}

		for _, g := range games {
			ids[g.ID()] = true
		}

		out.FromSeries = upcoming(snap.FromSeries, today, ids)
		out.Anticipated = upcoming(snap.Anticipated, today, nil)
	}

	return out, nil
}

// block returns a block's view and its stored snapshot.
func (s *Service) block(ctx context.Context, b Block) (BlockView, Snapshot, error) {
	views, err := s.enabled(ctx, b)
	if err != nil {
		return BlockView{}, Snapshot{}, err
	}

	if len(views) == 0 {
		return BlockView{State: StateNotConfigured}, Snapshot{}, nil
	}

	snap, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		return BlockView{}, Snapshot{}, err
	}

	v := BlockView{
		Provider:  views[0].Descriptor.Name,
		FetchedAt: snap.FetchedAt,
		FailedAt:  snap.FailedAt,
		Error:     snap.Error,
		ErrorKind: snap.ErrorKind,
	}

	if last := lastAttempt(snap); !last.IsZero() {
		v.RefreshableAt = last.Add(manualGap)
	}

	switch {
	case snap.FetchedAt.IsZero() && snap.Error == "":
		v.State = StateNeverFetched
	case snap.Error != "" || s.now().Sub(snap.FetchedAt) > 2*every(b):
		// Old data after a long downtime is shown with its age, like data whose refresh failed.
		v.State = StateStale
	default:
		v.State = StateFresh
	}

	return v, snap, nil
}

// upcoming keeps the releases not out yet (a release day is still upcoming) and, when owned is
// given, those whose owned game is still in the catalog.
func upcoming(in []game.Release, today game.Date, owned map[game.ID]bool) []game.Release {
	out := make([]game.Release, 0, len(in))

	for _, r := range in {
		if r.Date != "" && r.Date < today {
			continue
		}

		if owned != nil && r.Because != "" && !owned[r.Because] {
			continue
		}

		out = append(out, r)
	}

	return out
}

// Refresh gathers a block now, for the user, at most once every 15 minutes (failed attempts
// count). A failure of the service is not an error: the returned home shows it on the block, with
// the last good data.
func (s *Service) Refresh(ctx context.Context, b Block) (Home, error) {
	if _, ok := kindOfBlock[b]; !ok {
		return Home{}, ErrUnknownBlock
	}

	if err := s.refreshGuarded(ctx, b); err != nil {
		return Home{}, err
	}

	return s.Home(ctx)
}

// refreshGuarded is Refresh's refresh, under the lock and the guard.
func (s *Service) refreshGuarded(ctx context.Context, b Block) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		return err
	}

	if last := lastAttempt(snap); !last.IsZero() && s.now().Before(last.Add(manualGap)) {
		return ErrTooSoon
	}

	return s.refresh(ctx, b)
}

// refresh asks the block's providers in the user's order until one answers, and stores what it
// gave; when all fail, it stores the last failure next to the last good data. It returns
// ErrNotConfigured when no provider is enabled, and only storage errors otherwise. s.mu is held.
func (s *Service) refresh(ctx context.Context, b Block) error {
	views, err := s.enabled(ctx, b)
	if err != nil {
		return err
	}

	if len(views) == 0 {
		return ErrNotConfigured
	}

	asking, cancel := context.WithTimeout(ctx, refreshTimeout)
	defer cancel()

	var last error

	for _, v := range views {
		gathered, err := s.gather(asking, b, v)
		s.configs.ReportResult(v.ID(), err)

		if err == nil {
			gathered.Block, gathered.FetchedAt = b, s.now()
			return s.snaps.SaveSnapshot(ctx, gathered)
		}

		last = err

		if asking.Err() != nil {
			break
		}
	}

	if ctx.Err() != nil {
		return ctx.Err() // shutting down: nothing failed on the service's side
	}

	s.log.Warn("refreshing the home page", "block", b, "error", last)

	snap, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		return err
	}

	snap.Block, snap.Error, snap.ErrorKind, snap.FailedAt = b, last.Error(), kindOf(last), s.now()

	return s.snaps.SaveSnapshot(ctx, snap)
}

// gather asks one provider for a block's lists. Both lists must come, or neither is kept.
func (s *Service) gather(ctx context.Context, b Block, v providers.View) (Snapshot, error) {
	if b == BlockDeals {
		p := s.deals[v.ID()]
		country := s.country(ctx)

		featured, err := p.Deals(ctx, v.Settings(), DealQuery{List: DealsFeatured, Country: country, Limit: fetchedDeals})
		if err != nil {
			return Snapshot{}, err
		}

		lows, err := p.Deals(ctx, v.Settings(), DealQuery{List: DealsHistoricalLows, Country: country, Limit: fetchedDeals})
		if err != nil {
			return Snapshot{}, err
		}

		return Snapshot{
			Featured:       featured,
			HistoricalLows: lows,
		}, nil
	}

	p := s.releases[v.ID()]

	anticipated, err := p.Anticipated(ctx, v.Settings(), anticipationWindow)
	if err != nil {
		return Snapshot{}, err
	}

	owned, err := s.owned(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	var series []game.Release

	// An empty collection has no series: the provider is not asked with an empty list.
	if len(owned) > 0 {
		if series, err = p.Sequels(ctx, v.Settings(), owned); err != nil {
			return Snapshot{}, err
		}
	}

	return Snapshot{
		FromSeries:  series,
		Anticipated: anticipated,
	}, nil
}

// owned returns the games the user still has (a held copy), as releases providers need them.
func (s *Service) owned(ctx context.Context) ([]OwnedRef, error) {
	games, err := s.games.List(ctx)
	if err != nil {
		return nil, err
	}

	var out []OwnedRef

	for _, g := range games {
		if slices.ContainsFunc(g.Copies(), game.Copy.Held) {
			out = append(out, OwnedRef{
				Game:  g.ID(),
				Title: g.Title(),
				Links: g.Links(),
			})
		}
	}

	return out, nil
}

// country returns the deals' country from the preferred currency; "" lets the provider decide.
// Preferences that cannot be read only lose that hint.
func (s *Service) country(ctx context.Context) string {
	p, err := s.prefs.Preferences(ctx)
	if err != nil {
		return ""
	}

	return countryFor(p.Currency)
}

// countries maps currencies to the country whose prices to ask for. Euro-zone shops mostly charge
// one price, so the euro asks for Germany's; a user elsewhere sets the country in the provider.
var countries = map[string]string{
	"EUR": "DE", "USD": "US", "GBP": "GB", "CAD": "CA", "AUD": "AU", "NZD": "NZ", "JPY": "JP",
	"BRL": "BR", "MXN": "MX", "PLN": "PL", "CHF": "CH", "SEK": "SE", "NOK": "NO", "DKK": "DK",
	"CZK": "CZ", "HUF": "HU", "TRY": "TR", "INR": "IN", "KRW": "KR", "CNY": "CN", "ARS": "AR",
	"CLP": "CL", "COP": "CO", "ZAR": "ZA",
}

// countryFor returns the country of a currency, "" when there is no table entry.
func countryFor(currency string) string { return countries[strings.ToUpper(strings.TrimSpace(currency))] }

// kindOf classifies a refresh's error for a message the page translates.
func kindOf(err error) ErrorKind {
	switch {
	case errors.Is(err, provider.ErrQuotaExhausted):
		return ErrorQuota
	case errors.Is(err, provider.ErrBadSettings):
		return ErrorSettings
	case errors.Is(err, provider.ErrUnavailable):
		return ErrorUnavailable
	case errors.Is(err, provider.ErrRateLimited):
		return ErrorRateLimited
	}

	return ErrorFailed
}
```

`game.Copy.Held` used as a method expression works because `Held` has a value receiver. `ErrQuotaExhausted` and `ErrUnavailable` both match `ErrRateLimited`, so their cases come first.

- [ ] **Step 7: Write the owned filter** (`internal/application/home/owned.go`)

```go
package home

import (
	"slices"

	"gamevault/internal/domain/game"
)

// ownedIndex finds the owned games a deal is for: by Steam app, and by exact title (SameTitle's
// key, which compares every script). Only games with a held copy are in it.
type ownedIndex struct {
	bySteam map[string][]*game.Game
	byTitle map[string][]*game.Game
}

func newOwnedIndex(games []*game.Game) ownedIndex {
	ix := ownedIndex{
		bySteam: map[string][]*game.Game{},
		byTitle: map[string][]*game.Game{},
	}

	for _, g := range games {
		if !slices.ContainsFunc(g.Copies(), game.Copy.Held) {
			continue
		}

		// An empty id or title is never a key: a deal without a Steam id must not match every
		// game without a Steam link.
		if id := g.Links()[game.LinkSteam]; id != "" {
			ix.bySteam[id] = append(ix.bySteam[id], g)
		}

		if k := game.TitleKey(g.Title()); k != "" {
			ix.byTitle[k] = append(ix.byTitle[k], g)
		}
	}

	return ix
}

// filter returns up to limit deals, in order, without the games owned on PC; a game held only on
// consoles stays, with those systems in OwnedOn. Matching by Steam id or by title: a game owned on
// GOG or on disc has no Steam link, and its deal must go all the same.
func (ix ownedIndex) filter(deals []game.Deal, limit int) []game.Deal {
	out := make([]game.Deal, 0, min(len(deals), limit))

	for _, d := range deals {
		var games []*game.Game
		if d.SteamAppID != "" {
			games = append(games, ix.bySteam[d.SteamAppID]...)
		}

		if k := game.TitleKey(d.Title); k != "" {
			games = append(games, ix.byTitle[k]...)
		}

		systems := heldSystems(games)
		if slices.Contains(systems, game.SystemPC) {
			continue
		}

		d.OwnedOn = systems

		out = append(out, d)
		if len(out) == limit {
			break
		}
	}

	return out
}

// heldSystems returns the systems of the games' held copies, sorted, nil when there are none.
func heldSystems(games []*game.Game) []string {
	var out []string

	for _, g := range games {
		for _, c := range g.Copies() {
			if s := c.System(); c.Held() && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}

	slices.Sort(out)

	return out
}
```

- [ ] **Step 8: Run the tests**

Run: `task go -- test ./internal/application/home/ -v`
Expected: PASS.

- [ ] **Step 9: Lint and the whole suite**

Run: `task lint:fix && task lint && task test`
Expected: 0 issues. `TestSentinels_haveACode` (rpc) now fails until the three sentinels are listed: add them to `sentinels` in `internal/adapters/inbound/rpc/errors_internal_test.go` (import `"gamevault/internal/application/home"`):

```go
	"application/home.ErrUnknownBlock":  {home.ErrUnknownBlock, connect.CodeInvalidArgument},
	"application/home.ErrTooSoon":       {home.ErrTooSoon, connect.CodeResourceExhausted},
	"application/home.ErrNotConfigured": {home.ErrNotConfigured, connect.CodeFailedPrecondition},
```

Run `task test` again. Expected: all green.

- [ ] **Step 10: Commit**

```bash
git add internal/application/home internal/adapters/inbound/rpc/errors_internal_test.go
git commit -m "Add the home service: snapshot reading, guarded refresh and the owned-deal filter"
```

### Task 7: The unattended refresh job

**Files:**
- Create: `internal/application/home/scheduler.go`
- Create: `internal/application/home/scheduler_test.go`

**Interfaces:**
- Consumes: from Task 6, `(s *Service) refresh(ctx, b) error`, `every(b)`, `lastAttempt(snap)`, `s.mu`, `s.rhythm`, `s.snaps`, `s.now`, `s.log`, `ErrNotConfigured`; `unattended.Rhythm` (`Vary`, `Between`, `Wait`).
- Produces: `func (s *Service) Run(ctx context.Context)` — blocks until ctx ends; `cmd/gamevault/main.go` starts it unless `-no-unattended` (Task 8).

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the failing tests** (`internal/application/home/scheduler_test.go`)

```go
package home_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/game"
)

// steps drives Run deterministically: every random draw is 0.5 (Between gives the middle, Vary
// the duration itself), every wait moves the clock, and the job stops after a number of waits.
type steps struct {
	clk   *clock
	left  int
	waits []time.Duration

	// during runs inside the first wait, after the clock moved: what a person does meanwhile.
	during func()
}

func (st *steps) rhythm() unattended.Rhythm {
	return unattended.Rhythm{
		Random: func() float64 { return 0.5 },
		Sleep: func(_ context.Context, d time.Duration) bool {
			if st.left == 0 {
				return false // stops Run, as a cancelled context does
			}

			st.left--
			st.waits = append(st.waits, d)
			st.clk.advance(d)

			if st.during != nil {
				st.during()
				st.during = nil
			}

			return true
		},
	}
}

func TestRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN both blocks configured and never gathered", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)
		clk := &clock{start}
		st := &steps{clk: clk, left: 7}
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the job runs for a day", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN both are gathered 3 minutes after start, then deals every 6 hours and releases every 24", func(t *testing.T) {
				// deals and releases at +3m, deals at +6h03, +12h03, +18h03, +24h03, releases at +24h03.
				assert.Equal(t, []time.Duration{3 * time.Minute, 0, 6 * time.Hour, 6 * time.Hour, 6 * time.Hour, 6 * time.Hour, 0}, st.waits)
				assert.Len(t, deals.calls, 10, "five refreshes of two lists")
				assert.Len(t, releases.windows, 2)
			})
		})
	})

	t.Run("GIVEN snapshots gathered an hour and two hours ago", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)
		store := snapshots{
			home.BlockDeals:    {Block: home.BlockDeals, FetchedAt: start.Add(-time.Hour)},
			home.BlockReleases: {Block: home.BlockReleases, FetchedAt: start.Add(-2 * time.Hour)},
		}
		clk := &clock{start}
		st := &steps{clk: clk, left: 1}
		svc := home.NewService(cfg, catalog{}, store, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the server starts", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN nothing is asked at start: deals wait for their interval to end", func(t *testing.T) {
				assert.Equal(t, []time.Duration{5 * time.Hour}, st.waits)
				assert.Len(t, deals.calls, 2)
				assert.Zero(t, releases.calls())
			})
		})
	})

	t.Run("GIVEN snapshots older than their interval (the server was off for days)", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		cfg := &configs{}
		cfg.enable(deals)
		cfg.enable(releases)
		store := snapshots{
			home.BlockDeals:    {Block: home.BlockDeals, FetchedAt: start.Add(-72 * time.Hour), Featured: []game.Deal{{Title: "Echo Valley"}}},
			home.BlockReleases: {Block: home.BlockReleases, FetchedAt: start.Add(-30 * time.Hour)},
		}
		clk := &clock{start}
		st := &steps{clk: clk, left: 2}
		svc := home.NewService(cfg, catalog{}, store, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the server starts", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN both are gathered at a random moment 1–5 minutes later, not at once", func(t *testing.T) {
				assert.Equal(t, []time.Duration{3 * time.Minute, 0}, st.waits)
				assert.Len(t, deals.calls, 2)
				assert.Len(t, releases.windows, 1)
			})
		})
	})

	t.Run("GIVEN no provider configured", func(t *testing.T) {
		deals, releases := &fakeDeals{}, &fakeReleases{}
		clk := &clock{start}
		st := &steps{clk: clk, left: 3}
		svc := home.NewService(&configs{}, catalog{}, snapshots{}, prefs{},
			home.WithDeals(deals), home.WithReleases(releases), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		t.Run("WHEN the job runs", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN it sends nothing, and looks again in about an hour", func(t *testing.T) {
				assert.Empty(t, deals.calls)
				assert.Zero(t, releases.calls())
				assert.Equal(t, []time.Duration{3 * time.Minute, 0, time.Hour}, st.waits)
			})
		})
	})

	t.Run("GIVEN deals never gathered, and no releases provider", func(t *testing.T) {
		deals := &fakeDeals{}
		cfg := &configs{}
		cfg.enable(deals)
		clk := &clock{start}
		st := &steps{clk: clk, left: 3}
		svc := home.NewService(cfg, catalog{}, snapshots{}, prefs{},
			home.WithDeals(deals), home.WithClock(clk.now), home.WithRhythm(st.rhythm()))

		st.during = func() {
			_, err := svc.Refresh(ctx, home.BlockDeals)
			require.NoError(t, err)
		}

		t.Run("WHEN the user refreshes deals just before the job's turn", func(t *testing.T) {
			svc.Run(ctx)

			t.Run("THEN the job does not ask again: deals' next turn is a whole interval after the user's refresh", func(t *testing.T) {
				assert.Len(t, deals.calls, 2, "the user's refresh only")
				// deals at +3m (skipped), releases (not configured) at +3m and +1h03; deals next at +6h03.
				assert.Equal(t, []time.Duration{3 * time.Minute, 0, time.Hour}, st.waits)
			})
		})
	})
}
```

- [ ] **Step 3: Run them to see them fail**

Run: `task go -- test ./internal/application/home/ -run TestRun -v`
Expected: FAIL to compile: `svc.Run undefined`.

- [ ] **Step 4: Write the job** (`internal/application/home/scheduler.go`)

```go
package home

import (
	"context"
	"errors"
	"time"
)

const (
	// startMin and startMax bound when an overdue block is gathered after start-up: a random
	// moment, so a restart never asks the services at once or at a fixed second.
	startMin = time.Minute
	startMax = 5 * time.Minute

	// jitter varies every interval by up to ±10 %.
	jitter = 0.1

	// idleCheck is how often a block without an enabled provider is looked at again; nothing is
	// sent meanwhile.
	idleCheck = time.Hour
)

// Run gathers the blocks on their own schedule until ctx ends: deals every 6 hours and releases
// every 24, each varied ±10 %, an overdue block 1–5 minutes after start-up. It is the only caller of
// the providers besides the user's Refresh, and cmd/gamevault does not start it with
// -no-unattended.
func (s *Service) Run(ctx context.Context) {
	next := map[Block]time.Time{}
	for _, b := range Blocks() {
		next[b] = s.firstTurn(ctx, b)
	}

	for {
		b := Blocks()[0]
		for _, c := range Blocks() {
			if next[c].Before(next[b]) {
				b = c
			}
		}

		if !s.rhythm.Wait(ctx, max(next[b].Sub(s.now()), 0)) {
			return
		}

		next[b] = s.now().Add(s.turn(ctx, b))
	}
}

// firstTurn returns when a block is first gathered: when its interval ends, varied, and never
// sooner than 1–5 minutes after start-up.
func (s *Service) firstTurn(ctx context.Context, b Block) time.Time {
	now := s.now()
	soon := now.Add(s.rhythm.Between(startMin, startMax))

	snap, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		s.log.Warn("reading the home page's snapshot", "block", b, "error", err)
		return soon
	}

	last := lastAttempt(snap)
	if last.IsZero() {
		return soon
	}

	at := last.Add(s.rhythm.Vary(every(b), jitter))
	if at.Before(now.Add(startMin)) {
		return soon
	}

	return at
}

// turn gathers a block, unless the user refreshed it recently, and returns how long to wait for
// its next turn.
func (s *Service) turn(ctx context.Context, b Block) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	// A refresh the user asked for in the meantime counts as this turn: the next one is a whole
	// interval after it.
	if snap, _, err := s.snaps.Snapshot(ctx, b); err == nil {
		if last := lastAttempt(snap); !last.IsZero() && s.now().Sub(last) < every(b)/2 {
			return last.Add(s.rhythm.Vary(every(b), jitter)).Sub(s.now())
		}
	}

	err := s.refresh(ctx, b)

	switch {
	case errors.Is(err, ErrNotConfigured):
		return s.rhythm.Vary(idleCheck, jitter)
	case err != nil && ctx.Err() == nil:
		s.log.Warn("refreshing the home page", "block", b, "error", err)
	}

	return s.rhythm.Vary(every(b), jitter)
}
```

With `Random` = 0.5: `Between(1m, 5m)` = 3 m and `Vary(d, 0.1)` = `d`, which is what the expected waits use.

- [ ] **Step 5: Run the tests**

Run: `task go -- test ./internal/application/home/ -v`
Expected: PASS. Then the race detector over the package: `task test:race` (or `task go -- test -race ./internal/application/home/`). Expected: PASS.

- [ ] **Step 6: Lint and the whole suite**

Run: `task lint:fix && task lint && task test`
Expected: 0 issues; all green.

- [ ] **Step 7: Commit**

```bash
git add internal/application/home
git commit -m "Gather the home page's blocks on a jittered schedule, overdue ones soon after start"
```

### Task 8: `HomeService`, wiring and the job's start

**Files:**
- Create: `proto/gamevault/v1/home.proto`
- Create: `internal/adapters/inbound/rpc/home_handler.go`
- Create: `internal/adapters/inbound/rpc/home_test.go`
- Modify: `internal/adapters/inbound/rpc/server.go` (`Handlers.Home`, mount)
- Modify: `internal/adapters/inbound/rpc/harness_test.go` (`testPlugins.deals/releases`, `fakeDeals`, `clients.home`)
- Modify: `internal/app/app.go` (`Plugins.Deals/Releases`, `noPlugins`, `App.Home`, the service and handler)
- Modify: `cmd/gamevault/main.go` (start `vault.Home.Run` unless `-no-unattended`)
- Generated: `internal/gen/gamevault/v1/home.pb.go`, `internal/gen/gamevault/v1/gamevaultv1connect/home.connect.go`, `web/src/gen/gamevault/v1/home_pb.ts` (by `task generate`)

**Interfaces:**
- Consumes: `home.NewService`, `home.WithDeals`, `home.WithReleases`, `home.WithClock`, `home.WithLogger`, `(*home.Service).Home`, `Refresh`, `Run`, `home.Home`, `home.BlockView`, `home.Block` (Tasks 6–7); `sqlite.NewHomeSnapshots` (Task 3); `plugin.Registry.Deals/Releases` (Task 2).
- Produces:
  - proto `gamevault.v1.HomeService` with `GetHome(GetHomeRequest) → GetHomeResponse` and `RefreshHome(RefreshHomeRequest{block}) → RefreshHomeResponse{home}`; messages `HomeBlock{state, provider, fetched_at, failed_at, error_kind, error, refreshable_at}`, `HomeDeal{title, shop, price, regular, cut, historical_low, expires_at, url, image_url, owned_on, steam_app_id}`, `HomeRelease{title, release_date, platforms, cover_url, url, because_game_id, because_title}`, `GetHomeResponse{deals, featured, historical_lows, releases, from_series, anticipated}`.
  - TypeScript (generated): `HomeService`, `GetHomeResponse` with `deals?: HomeBlock`, `featured: HomeDeal[]`, `historicalLows: HomeDeal[]`, `releases?: HomeBlock`, `fromSeries: HomeRelease[]`, `anticipated: HomeRelease[]`; `HomeBlock.state: string`, `fetchedAt?/failedAt?/refreshableAt?: Timestamp`, `errorKind: string`, `error: string`, `provider: string`; `HomeDeal.price?/regular?: Money` (unset for a free game), `cut: number`, `historicalLow: boolean`, `expiresAt?: Timestamp`, `url`, `imageUrl`, `ownedOn: string[]`; `HomeRelease.releaseDate: string`, `platforms: string[]`, `coverUrl`, `url`, `becauseGameId`, `becauseTitle`.
  - `app.Plugins` gains `Deals() []home.DealsProvider` and `Releases() []home.ReleasesProvider`; `app.App.Home *home.Service`.

- [ ] **Step 1: Load the `write-go` skill.**

- [ ] **Step 2: Write the API** (`proto/gamevault/v1/home.proto`)

```proto
syntax = "proto3";

package gamevault.v1;

import "gamevault/v1/game.proto";
import "google/protobuf/timestamp.proto";

option go_package = "gamevault/internal/gen/gamevault/v1;gamevaultv1";

// HomeBlock is the state of one of the home page's external blocks (deals, releases).
message HomeBlock {
  // "fresh", "stale" (the last attempt failed, or the data is old), "not_configured" (no provider
  // enabled: nothing is shown) or "never_fetched"; the server sends no other value.
  string state = 1;
  // The first enabled provider's name ("IsThereAnyDeal"), for messages and the credit line.
  string provider = 2;
  // When the data shown was gathered; unset when never.
  google.protobuf.Timestamp fetched_at = 3;
  // When the last attempt failed, when it came after the data; unset otherwise.
  google.protobuf.Timestamp failed_at = 4;
  // That failure's kind, which clients translate: "rate_limited", "quota", "settings",
  // "unavailable" or "failed".
  string error_kind = 5;
  // That failure's message, in English (often the service's own words).
  string error = 6;
  // When the user may refresh the block again; unset: now.
  google.protobuf.Timestamp refreshable_at = 7;
}

// HomeDeal is a shop's current price for a game the user does not own on PC, as the deals service
// gave it (its terms forbid altering prices and links).
message HomeDeal {
  string title = 1;
  // The shop that sells it ("Steam", "GOG"…).
  string shop = 2;
  // The price now and the regular price; unset for a free game.
  Money price = 3;
  Money regular = 4;
  // The discount, in percent.
  int32 cut = 5;
  // Whether the price is the lowest the service has seen.
  bool historical_low = 6;
  // When the deal ends; unset when the shop does not say.
  google.protobuf.Timestamp expires_at = 7;
  // Opens the deal through the service's link (https only); empty when there is none.
  string url = 8;
  // The game's art on the service, loaded through the image proxy; empty when there is none.
  string image_url = 9;
  // The systems the user owns the game on, when only consoles ("PS4").
  repeated string owned_on = 10;
  // The game's Steam app; empty when the service knows none.
  string steam_app_id = 11;
}

// HomeRelease is a game that comes out soon.
message HomeRelease {
  string title = 1;
  // The first release date, YYYY-MM-DD; empty when unknown.
  string release_date = 2;
  // The Game Vault systems it is announced for ("PC", "PS5").
  repeated string platforms = 3;
  // Its box art on the database, loaded through the image proxy; empty when there is none.
  string cover_url = 4;
  // Its page on the database; empty when there is none.
  string url = 5;
  // The owned game whose series it continues, and its title; empty for an anticipated release.
  string because_game_id = 6;
  string because_title = 7;
}

message GetHomeRequest {}

message GetHomeResponse {
  HomeBlock deals = 1;
  // At most 12 each, in the service's order.
  repeated HomeDeal featured = 2;
  repeated HomeDeal historical_lows = 3;
  HomeBlock releases = 4;
  // Upcoming entries of the user's series, soonest first.
  repeated HomeRelease from_series = 5;
  // The most anticipated releases of the next 60 days.
  repeated HomeRelease anticipated = 6;
}

message RefreshHomeRequest {
  // "deals" or "releases".
  string block = 1;
}

message RefreshHomeResponse {
  // The whole home after the refresh: a failure of the service shows on its block.
  GetHomeResponse home = 1;
}

// HomeService serves the home page's external blocks: current deals and upcoming releases, gathered
// in the background and stored, so reading never calls a service.
service HomeService {
  rpc GetHome(GetHomeRequest) returns (GetHomeResponse);
  // Gathers one block now. Refused within 15 minutes of the last attempt (resource exhausted), and
  // when no provider is enabled for it (failed precondition).
  rpc RefreshHome(RefreshHomeRequest) returns (RefreshHomeResponse);
}
```

- [ ] **Step 3: Generate the code**

Run: `task generate && task lint`
Expected: new files under `internal/gen` and `web/src/gen`; buf lint passes. If buf lint names a rule (comment or field naming), fix the proto, not the rule.

- [ ] **Step 4: Write the failing end-to-end test** (`internal/adapters/inbound/rpc/home_test.go`)

```go
package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

func TestHome(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a server with a deals provider and a game owned on Steam", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		_, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Lantern Keep",
			Links: map[string]string{"steam": "4410"},
			Copies: []*pb.CopyDetails{{
				Kind:     pb.CopyKind_COPY_KIND_LIBRARY,
				Platform: "Steam",
			}},
		}))
		require.NoError(t, err)

		t.Run("WHEN the home page is read", func(t *testing.T) {
			res, err := c.home.GetHome(ctx, connect.NewRequest(&pb.GetHomeRequest{}))
			require.NoError(t, err)

			t.Run("THEN deals were never fetched and releases are not configured", func(t *testing.T) {
				assert.Equal(t, "never_fetched", res.Msg.Deals.GetState())
				assert.Equal(t, "Fake deals", res.Msg.Deals.GetProvider())
				assert.Equal(t, "not_configured", res.Msg.Releases.GetState())
				assert.Empty(t, res.Msg.Featured)
			})
		})

		t.Run("WHEN the user refreshes the deals", func(t *testing.T) {
			res, err := c.home.RefreshHome(ctx, connect.NewRequest(&pb.RefreshHomeRequest{Block: "deals"}))
			require.NoError(t, err)

			h := res.Msg.Home

			t.Run("THEN the block is fresh, without the game the user owns", func(t *testing.T) {
				assert.Equal(t, "fresh", h.Deals.GetState())
				assert.NotNil(t, h.Deals.GetFetchedAt())
				assert.NotNil(t, h.Deals.GetRefreshableAt())
				require.Len(t, h.Featured, 1)

				d := h.Featured[0]
				assert.Equal(t, "Harbor Lights", d.Title)
				assert.Equal(t, int64(499), d.Price.GetAmountMinor())
				assert.Equal(t, "EUR", d.Price.GetCurrency())
				assert.Equal(t, int32(75), d.Cut)
				assert.Equal(t, "https://itad.link/fake/", d.Url)
			})
		})

		t.Run("WHEN the user refreshes the deals again at once", func(t *testing.T) {
			_, err := c.home.RefreshHome(ctx, connect.NewRequest(&pb.RefreshHomeRequest{Block: "deals"}))

			t.Run("THEN it is refused as too soon", func(t *testing.T) {
				assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
			})
		})

		t.Run("WHEN the user refreshes releases, or a block that does not exist", func(t *testing.T) {
			_, notConfigured := c.home.RefreshHome(ctx, connect.NewRequest(&pb.RefreshHomeRequest{Block: "releases"}))
			_, unknown := c.home.RefreshHome(ctx, connect.NewRequest(&pb.RefreshHomeRequest{Block: "weather"}))

			t.Run("THEN they are told to enable a provider, or that the request is wrong", func(t *testing.T) {
				assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(notConfigured))
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(unknown))
			})
		})
	})
}
```

- [ ] **Step 5: Add the fake deals provider to the harness** (`internal/adapters/inbound/rpc/harness_test.go`)

Add `home gamevaultv1connect.HomeServiceClient` to `clients` and `home: gamevaultv1connect.NewHomeServiceClient(http.DefaultClient, srv.URL),` to the returned struct. Add fields and methods to `testPlugins`:

```go
// testPlugins are the fakes the end-to-end tests run instead of real integrations.
type testPlugins struct {
	sources    []sync.Provider
	providers  []providers.Impl
	valuations []valuation.Provider
	deals      []home.DealsProvider
	releases   []home.ReleasesProvider
}

func (p testPlugins) Deals() []home.DealsProvider { return p.deals }

func (p testPlugins) Releases() []home.ReleasesProvider { return p.releases }
```

In `newServer`, add `fakeDeals{}` to `providers` and `deals: []home.DealsProvider{fakeDeals{}},`. Then the fake (after `fakePrices`):

```go
// fakeDeals is a deals provider, on by default, with one deal for a game the tests own (Lantern
// Keep, Steam 4410) and one for a game they do not.
type fakeDeals struct{}

func (fakeDeals) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "fake-deals",
		Kind:             provider.KindDeals,
		Name:             "Fake deals",
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (fakeDeals) Test(context.Context, schema.Settings) error { return nil }

func (fakeDeals) Deals(_ context.Context, _ schema.Settings, q home.DealQuery) ([]game.Deal, error) {
	if q.List != home.DealsFeatured {
		return nil, nil
	}

	return []game.Deal{
		{
			Title:      "Lantern Keep",
			SteamAppID: "4410",
			Price:      game.Money{Amount: 199, Currency: "EUR"},
		},
		{
			Title:   "Harbor Lights",
			Shop:    "Steam",
			Price:   game.Money{Amount: 499, Currency: "EUR"},
			Regular: game.Money{Amount: 1999, Currency: "EUR"},
			Cut:     75,
			URL:     "https://itad.link/fake/",
		},
	}, nil
}
```

- [ ] **Step 6: Run it to see it fail**

Run: `task go -- test ./internal/adapters/inbound/rpc/ -run TestHome -v`
Expected: FAIL to compile: `app.Plugins` has no `Deals` method / `c.home` is nil — then, once compiling, a 404 for `/gamevault.v1.HomeService/GetHome`.

- [ ] **Step 7: Write the handler** (`internal/adapters/inbound/rpc/home_handler.go`)

```go
package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

var _ gamevaultv1connect.HomeServiceHandler = (*HomeHandler)(nil)

// HomeHandler serves the home page's external blocks.
type HomeHandler struct{ home *home.Service }

// NewHomeHandler builds the handler.
func NewHomeHandler(h *home.Service) *HomeHandler { return &HomeHandler{home: h} }

// GetHome returns the stored blocks; it never calls a service.
func (h *HomeHandler) GetHome(ctx context.Context, _ *connect.Request[pb.GetHomeRequest]) (*connect.Response[pb.GetHomeResponse], error) {
	out, err := h.home.Home(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(homeToPB(out)), nil
}

// RefreshHome gathers one block now and returns the whole home.
func (h *HomeHandler) RefreshHome(ctx context.Context, req *connect.Request[pb.RefreshHomeRequest]) (*connect.Response[pb.RefreshHomeResponse], error) {
	out, err := h.home.Refresh(ctx, home.Block(req.Msg.Block))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.RefreshHomeResponse{Home: homeToPB(out)}), nil
}

func homeToPB(h home.Home) *pb.GetHomeResponse {
	return &pb.GetHomeResponse{
		Deals:          blockToPB(h.Deals),
		Featured:       dealsToPB(h.Featured),
		HistoricalLows: dealsToPB(h.HistoricalLows),
		Releases:       blockToPB(h.Releases),
		FromSeries:     releasesToPB(h.FromSeries),
		Anticipated:    releasesToPB(h.Anticipated),
	}
}

func blockToPB(b home.BlockView) *pb.HomeBlock {
	return &pb.HomeBlock{
		State:         string(b.State),
		Provider:      b.Provider,
		FetchedAt:     ts(b.FetchedAt),
		FailedAt:      ts(b.FailedAt),
		ErrorKind:     string(b.ErrorKind),
		Error:         b.Error,
		RefreshableAt: ts(b.RefreshableAt),
	}
}

func dealsToPB(in []game.Deal) []*pb.HomeDeal {
	out := make([]*pb.HomeDeal, 0, len(in))
	for _, d := range in {
		out = append(out, &pb.HomeDeal{
			Title:         d.Title,
			Shop:          d.Shop,
			Price:         moneyToPB(d.Price),
			Regular:       moneyToPB(d.Regular),
			Cut:           int32(d.Cut),
			HistoricalLow: d.HistoricalLow,
			ExpiresAt:     ts(d.Expires),
			Url:           d.URL,
			ImageUrl:      d.ImageURL,
			OwnedOn:       d.OwnedOn,
			SteamAppId:    d.SteamAppID,
		})
	}

	return out
}

func releasesToPB(in []game.Release) []*pb.HomeRelease {
	out := make([]*pb.HomeRelease, 0, len(in))
	for _, r := range in {
		out = append(out, &pb.HomeRelease{
			Title:         r.Title,
			ReleaseDate:   string(r.Date),
			Platforms:     r.Platforms,
			CoverUrl:      r.CoverURL,
			Url:           r.URL,
			BecauseGameId: string(r.Because),
			BecauseTitle:  r.BecauseTitle,
		})
	}

	return out
}
```

- [ ] **Step 8: Mount it** (`internal/adapters/inbound/rpc/server.go`)

Add to `Handlers`, after `Ownership`:

```go
	// Home serves the home page's external blocks.
	Home *HomeHandler
```

and in `NewHTTPHandler`, after the health handler:

```go
	mux.Handle(gamevaultv1connect.NewHomeServiceHandler(h.Home, ic))
```

- [ ] **Step 9: Wire the service** (`internal/app/app.go`)

Import `"gamevault/internal/application/home"`. Extend `Plugins`:

```go
	// Valuations are the price providers the valuation service asks.
	Valuations() []valuation.Provider

	// Deals and Releases are the home page's providers.
	Deals() []home.DealsProvider
	Releases() []home.ReleasesProvider
```

Add to `App`, after `System`:

```go
	// Home gathers the home page's deals and releases.
	Home *home.Service
```

In `Build`, after `valuationSvc`:

```go
	homeSvc := home.NewService(registry, games, sqlite.NewHomeSnapshots(db), settingsRepo,
		home.WithDeals(o.plugins.Deals()...), home.WithReleases(o.plugins.Releases()...),
		home.WithClock(now), home.WithLogger(log))
```

Add `Home: rpc.NewHomeHandler(homeSvc),` to `rpc.Handlers{…}` and `Home: homeSvc,` to the returned `App`. Add to `noPlugins`:

```go
func (noPlugins) Deals() []home.DealsProvider { return nil }

func (noPlugins) Releases() []home.ReleasesProvider { return nil }
```

- [ ] **Step 10: Start the job** (`cmd/gamevault/main.go`, the background jobs)

```go
	if cfg.NoUnattended {
		log.Warn("scheduled scans, keep-alives, price estimates, the background details scan and the home page's deals and releases are off (-no-unattended)")
	} else {
		go vault.Sync.RunScheduler(ctx, time.Minute, 30*time.Minute)
		go vault.Sync.RunKeepAlive(ctx, time.Minute, 10*time.Minute)
		go vault.Valuation.RunScheduler(ctx)
		go vault.Media.Details.RunScanner(ctx, 2*time.Second) // 2-4 s apart: gentle enough for the strictest store API (Steam: ~200 requests / 5 min)
		go vault.Home.Run(ctx)                                // deals every 6 h, releases every 24 h, ±10 %
	}
```

- [ ] **Step 11: Run the tests**

Run: `task go -- test ./internal/adapters/inbound/rpc/ ./internal/app/... ./cmd/gamevault/ -v -run 'TestHome|TestSentinels|TestPlugins'`
Expected: PASS.

- [ ] **Step 12: Lint and the whole suite**

Run: `task lint:fix && task lint && task test`
Expected: 0 issues; all green (the generated TypeScript type-checks; nothing uses it yet).

- [ ] **Step 13: Commit**

```bash
git add proto internal/gen web/src/gen internal/adapters/inbound/rpc internal/app cmd/gamevault/main.go
git commit -m "Serve the home page's blocks over HomeService and start their job unless -no-unattended"
```

### Task 9: The web's pure helpers, and Home as the start address

**Files:**
- Create: `web/src/lib/home.ts`
- Create: `web/tests/home.test.mjs`
- Modify: `web/src/lib/route.ts` (`'home'` page, the default)
- Modify: `web/tests/route.test.mjs`

**Interfaces:**
- Consumes: `EXPIRING_DAYS` (`lib/deadlines.ts`), `sinceText` (`lib/health.ts`); the API's enum numbers (`CopyKind.KEY = 1`, `PHYSICAL = 3`; `CopyStatus.UNREVEALED = 1`, `REVEALED = 2`, `GIFTED = 4`, `EXPIRED = 5`, `SOLD = 8`; `PlayStatus.BACKLOG = 1`).
- Produces (`web/src/lib/home.ts`, structural types only):
  - `interface CopyLike { details?: { kind: number; status: number; redeemBy: string }; redundant: boolean; effectiveSystem: string }`
  - `interface GameLike { id: string; title: string; copies: CopyLike[]; createdAt?: { seconds: bigint }; playStatus: number }`
  - `type PendingItem = { kind: 'expiring'; count: number; nearest: { title: string; days: number; gameId: string }; href: string } | { kind: 'pending' | 'redundant'; count: number; href: string } | { kind: 'health'; count: number; level: 'error' | 'warning'; href: string }`
  - `pendingItems(games: GameLike[], issues: { level: string }[], today: Date): PendingItem[]`
  - `interface CollectionSummary { games: number; copies: number; addedThisMonth: number; backlog: number; recent: string[]; systems: { system: string; count: number; rest: boolean }[] }`
  - `collectionSummary(games: GameLike[], now: Date): CollectionSummary`
  - `pickValue(totals: { copies: number; sell?: { amountMinor: bigint; currency: string } }[]): { amountMinor: bigint; currency: string } | null`
  - `type BlockMode = 'data' | 'notConfigured' | 'neverFetched'`; `blockMode(b?: { state: string }): BlockMode`
  - `staleLine(b: { state: string; errorKind: string; provider: string }, fetchedAt: Date | null, now: Date, locale: string, t: Translate): string | null`
  - `endsInDays(expires: Date | null, now: Date): number | null`
  - `canRefresh(refreshableAt: Date | null, now: Date): boolean`
  - `type LibraryPreset = { quick: 'pending' | 'expiring' | 'redundant' } | { play: number } | { kind: number } | { sort: 'added' }`; `libraryPreset(target: string): LibraryPreset | null`; addresses `#/library/pending`, `#/library/expiring`, `#/library/redundant`, `#/library/backlog`, `#/library/physical`, `#/library/recent`
  - `lib/route.ts`: `PAGES` starts with `'home'`; `parseHash('')`, `parseHash('#/')` and unknown addresses give `{ page: 'home', target: '', game: '' }`

- [ ] **Step 1: Write the failing tests** (`web/tests/home.test.mjs`)

```js
import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  blockMode, canRefresh, collectionSummary, endsInDays, libraryPreset, pendingItems, pickValue, staleLine,
} from '../src/lib/home.ts';

const t = (key, opts) => `${key}${opts ? ' ' + JSON.stringify(opts) : ''}`;
const today = new Date(2026, 9, 15, 10, 0, 0); // local time, as the page reads it
const ts = (d) => ({ seconds: BigInt(Math.floor(d.getTime() / 1000)) });

// Copies as the API sends them; every title is invented.
const key = (status, redeemBy = '', redundant = false) => ({ details: { kind: 1, status, redeemBy }, redundant, effectiveSystem: 'PC' });
const library = (system = 'PC') => ({ details: { kind: 2, status: 6, redeemBy: '' }, redundant: false, effectiveSystem: system });
const disc = (system, status = 6) => ({ details: { kind: 3, status, redeemBy: '' }, redundant: false, effectiveSystem: system });
const game = (id, title, copies, extra = {}) => ({ id, title, copies, playStatus: 0, createdAt: ts(new Date(2026, 0, 1)), ...extra });

test('the pending strip counts what the library filters show, nearest deadline first', () => {
  const games = [
    game('g1', 'Lantern Keep', [key(1, '2026-10-18'), library()]),
    game('g2', 'Echo Valley', [key(2, '2026-11-01', true)]),
    game('g3', 'Harbor Lights', [key(2, '2027-03-01'), key(3, '2026-10-16')]), // the redeemed one is done
    game('g4', 'Copper Coast', [key(1)]),
  ];
  const items = pendingItems(games, [{ level: 'warning' }, { level: 'error' }], today);
  assert.deepEqual(items, [
    { kind: 'expiring', count: 2, nearest: { title: 'Lantern Keep', days: 3, gameId: 'g1' }, href: '#/library/expiring' },
    { kind: 'pending', count: 4, href: '#/library/pending' },
    { kind: 'redundant', count: 1, href: '#/library/redundant' },
    { kind: 'health', count: 2, level: 'error', href: '#/system/health' },
  ]);
});

test('"Expiring" is the library\'s rule: within 30 days, a deadline already past included', () => {
  const items = pendingItems([
    game('g1', 'Lantern Keep', [key(1, '2026-11-14')]), // 30 days
    game('g2', 'Echo Valley', [key(1, '2026-11-15')]),  // 31 days
    game('g3', 'Harbor Lights', [key(1, '2026-10-12')]), // 3 days ago
  ], [], today);
  assert.deepEqual(items[0], { kind: 'expiring', count: 2, nearest: { title: 'Harbor Lights', days: -3, gameId: 'g3' }, href: '#/library/expiring' });
});

test('with nothing pending, the strip is empty (the page says "All caught up")', () => {
  assert.deepEqual(pendingItems([game('g1', 'Lantern Keep', [library(), key(3)])], [], today), []);
  assert.deepEqual(pendingItems([], [], today), []);
});

test('the collection in figures: held copies, this month in local time, the backlog, the newest, the systems', () => {
  const games = [
    game('g1', 'Lantern Keep', [library(), disc('PS4')], { createdAt: ts(new Date(2026, 9, 1, 0, 30)), playStatus: 1 }),
    game('g2', 'Echo Valley', [disc('Switch'), disc('PS4', 8)], { createdAt: ts(new Date(2026, 8, 30, 23, 30)) }),
    game('g3', 'Harbor Lights', [library()], { createdAt: ts(new Date(2026, 9, 14)), playStatus: 1 }),
  ];
  const s = collectionSummary(games, today);
  assert.equal(s.games, 3);
  assert.equal(s.copies, 4, 'the sold disc is not counted');
  assert.equal(s.addedThisMonth, 2, 'September 30th at 23:30 is last month');
  assert.equal(s.backlog, 2);
  assert.deepEqual(s.recent, ['g3', 'g1', 'g2']);
  assert.deepEqual(s.systems, [
    { system: 'PC', count: 2, rest: false },
    { system: 'PS4', count: 1, rest: false },
    { system: 'Switch', count: 1, rest: false },
  ]);
});

test('an empty collection is all zeros, not an error', () => {
  assert.deepEqual(collectionSummary([], today), { games: 0, copies: 0, addedThisMonth: 0, backlog: 0, recent: [], systems: [] });
});

test('the share by system keeps the five largest and adds the rest up', () => {
  const systems = ['PC', 'PS5', 'PS4', 'Switch', 'Xbox One', 'Wii', 'DS'];
  const games = systems.flatMap((s, i) => Array.from({ length: 7 - i }, (_, j) => game(`${s}-${j}`, `Example ${s} ${j}`, [disc(s)])));
  assert.deepEqual(collectionSummary(games, today).systems, [
    { system: 'PC', count: 7, rest: false },
    { system: 'PS5', count: 6, rest: false },
    { system: 'PS4', count: 5, rest: false },
    { system: 'Switch', count: 4, rest: false },
    { system: 'Xbox One', count: 3, rest: false },
    { system: '', count: 3, rest: true },
  ]);
});

test('"Recently added" keeps the eight newest', () => {
  const games = Array.from({ length: 12 }, (_, i) => game(`g${i}`, `Example ${i}`, [], { createdAt: ts(new Date(2026, 9, 1 + i)) }));
  assert.deepEqual(collectionSummary(games, today).recent, ['g11', 'g10', 'g9', 'g8', 'g7', 'g6', 'g5', 'g4']);
});

test("the collection's value is the first source that priced copies", () => {
  assert.equal(pickValue([]), null);
  assert.equal(pickValue([{ copies: 0, sell: { amountMinor: 0n, currency: 'EUR' } }]), null);
  assert.deepEqual(pickValue([{ copies: 0 }, { copies: 3, sell: { amountMinor: 4500n, currency: 'EUR' } }]), { amountMinor: 4500n, currency: 'EUR' });
});

test('an external block shows its data, a "not configured" card, or a "not fetched yet" card', () => {
  assert.equal(blockMode(undefined), 'notConfigured');
  assert.equal(blockMode({ state: 'not_configured' }), 'notConfigured');
  assert.equal(blockMode({ state: 'never_fetched' }), 'neverFetched');
  assert.equal(blockMode({ state: 'fresh' }), 'data');
  assert.equal(blockMode({ state: 'stale' }), 'data');
});

test('a stale block says how old its data is and why', () => {
  const now = new Date('2026-10-15T12:00:00Z');
  const sixHours = new Date('2026-10-15T06:00:00Z');
  assert.equal(staleLine({ state: 'fresh', errorKind: '', provider: 'IsThereAnyDeal' }, sixHours, now, 'en', t), null);
  assert.equal(staleLine({ state: 'stale', errorKind: 'unavailable', provider: 'IsThereAnyDeal' }, sixHours, now, 'en', t),
    'home.updated {"when":"6 hours ago"} · home.errors.unavailable {"provider":"IsThereAnyDeal"}');
  assert.equal(staleLine({ state: 'stale', errorKind: '', provider: 'IGDB' }, new Date('2026-10-12T12:00:00Z'), now, 'en', t),
    'home.updated {"when":"3 days ago"}', 'old data after a long downtime: no reason to give');
  assert.equal(staleLine({ state: 'stale', errorKind: 'settings', provider: 'IGDB' }, null, now, 'en', t),
    'home.neverUpdated · home.errors.settings {"provider":"IGDB"}');
});

test('a deal ends in whole days; none when it does not say or is over', () => {
  const now = new Date('2026-10-15T12:00:00Z');
  assert.equal(endsInDays(null, now), null);
  assert.equal(endsInDays(new Date('2026-10-15T11:00:00Z'), now), null);
  assert.equal(endsInDays(new Date('2026-10-15T20:00:00Z'), now), 0);
  assert.equal(endsInDays(new Date('2026-10-18T12:00:00Z'), now), 3);
});

test('a block can be refreshed once its 15 minutes are over', () => {
  const now = new Date('2026-10-15T12:00:00Z');
  assert.equal(canRefresh(null, now), true);
  assert.equal(canRefresh(new Date('2026-10-15T12:05:00Z'), now), false);
  assert.equal(canRefresh(new Date('2026-10-15T12:00:00Z'), now), true);
});

test('the home page links to library views by address', () => {
  assert.deepEqual(libraryPreset('pending'), { quick: 'pending' });
  assert.deepEqual(libraryPreset('expiring'), { quick: 'expiring' });
  assert.deepEqual(libraryPreset('redundant'), { quick: 'redundant' });
  assert.deepEqual(libraryPreset('backlog'), { play: 1 });
  assert.deepEqual(libraryPreset('physical'), { kind: 3 });
  assert.deepEqual(libraryPreset('recent'), { sort: 'added' });
  assert.equal(libraryPreset(''), null);
  assert.equal(libraryPreset('anything'), null);
});
```

In `web/tests/route.test.mjs`, replace the first and last tests:

```js
test('an address names a page and what to open on it', () => {
  assert.deepEqual(parseHash('#/sources/s%201'), { page: 'sources', target: 's 1', game: '' });
  assert.deepEqual(parseHash('#/system/backups'), { page: 'system', target: 'backups', game: '' });
  assert.deepEqual(parseHash('#/sources'), { page: 'sources', target: '', game: '' });
  assert.deepEqual(parseHash('#/scan'), { page: 'scan', target: '', game: '' });
  assert.deepEqual(parseHash('#/library/pending'), { page: 'library', target: 'pending', game: '' });
  assert.deepEqual(parseHash('#/home'), { page: 'home', target: '', game: '' });
});

test('the start address and unknown ones open the home page, and a broken escape opens nothing', () => {
  assert.deepEqual(parseHash(''), { page: 'home', target: '', game: '' });
  assert.deepEqual(parseHash('#/'), { page: 'home', target: '', game: '' });
  assert.deepEqual(parseHash('#/nowhere/x'), { page: 'home', target: '', game: '' });
  assert.deepEqual(parseHash('#/sources/%E0%A4%A'), { page: 'sources', target: '', game: '' });
});
```

- [ ] **Step 2: Run them to see them fail**

Run: `docker run --rm -v "$PWD:/src" -w /src/web -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 tests/home.test.mjs tests/route.test.mjs`
Expected: FAIL: `Cannot find module '../src/lib/home.ts'`; route: `'library' !== 'home'`.

- [ ] **Step 3: Make Home the start page** (`web/src/lib/route.ts`)

```ts
/** The app's pages, as their addresses name them; the first is the start page. */
export const PAGES = ['home', 'library', 'scan', 'sources', 'providers', 'fields', 'system', 'logs', 'settings'] as const;

export type Page = (typeof PAGES)[number];
```

In `parseHash`, change the two fallbacks from `'library'` to `'home'` and the doc comment to "anything unknown is the home page":

```ts
/** The route an address names; anything unknown is the home page. */
export function parseHash(hash: string): Route {
  const game = gameIdFromHash(hash);
  if (game) return { page: 'library', target: '', game };
  const [first = '', rest = ''] = hash.replace(/^#\/?/, '').split(/\/(.*)/s);
  const page = (PAGES as readonly string[]).includes(first) ? first as Page : 'home';
  if (page !== first) return { page, target: '', game: '' };
```

(the rest unchanged).

- [ ] **Step 4: Write the helpers** (`web/src/lib/home.ts`)

```ts
// The home page's pure helpers: what is pending, the collection in figures, and how an external
// block reads. The pending strip counts with the library's own rules (lib/deadlines.ts, the copies'
// `redundant` flag), so a chip always says what its library view shows. Structural types only,
// Node-tested (tests/home.test.mjs); the page is features/home/HomePage.tsx.
import { EXPIRING_DAYS } from './deadlines.ts';
import { sinceText } from './health.ts';

type Translate = (key: string, options?: Record<string, string>) => string;

// The API's numbers (proto/gamevault/v1/game.proto): CopyKind, CopyStatus, PlayStatus.
const KIND_KEY = 1;
const KIND_PHYSICAL = 3;
const STATUS_UNREVEALED = 1;
const STATUS_REVEALED = 2;
const GONE = new Set([4, 5, 8]); // gifted, expired, sold: the user no longer has the copy
const PLAY_BACKLOG = 1;

/** How many covers "Recently added" shows, and how many systems the share bar names. */
const RECENT = 8;
const TOP_SYSTEMS = 5;

const DAY = 86_400_000;

/** A copy as the helpers need it (see Copy in the API). */
export interface CopyLike {
  details?: { kind: number; status: number; redeemBy: string };
  redundant: boolean;
  effectiveSystem: string;
}

/** A game as the helpers need it (see Game in the API). */
export interface GameLike {
  id: string;
  title: string;
  copies: CopyLike[];
  createdAt?: { seconds: bigint };
  playStatus: number;
}

const isPendingKey = (c: CopyLike) =>
  c.details?.kind === KIND_KEY && (c.details.status === STATUS_UNREVEALED || c.details.status === STATUS_REVEALED);

const isHeld = (c: CopyLike) => !GONE.has(c.details?.status ?? 0);

/** Whole days from `today` (its local date) to a YYYY-MM-DD date, negative when it has passed. */
function daysTo(today: Date, date: string): number {
  const start = new Date(today);
  start.setHours(0, 0, 0, 0);
  return Math.round((new Date(`${date}T00:00:00`).getTime() - start.getTime()) / DAY);
}

/** One chip of the pending strip. */
export type PendingItem =
  | { kind: 'expiring'; count: number; nearest: { title: string; days: number; gameId: string }; href: string }
  | { kind: 'pending' | 'redundant'; count: number; href: string }
  | { kind: 'health'; count: number; level: 'error' | 'warning'; href: string };

/** The pending strip: keys that expire soon (the library's "Expiring": within EXPIRING_DAYS, past
 *  deadlines included) with the nearest one, keys still to redeem, keys not needed, and the
 *  Health issues. Empty when nothing is pending. */
export function pendingItems(games: GameLike[], issues: { level: string }[], today: Date): PendingItem[] {
  let pending = 0;
  let redundant = 0;
  let expiring = 0;
  let nearest: { title: string; days: number; gameId: string } | null = null;

  for (const g of games) {
    for (const c of g.copies) {
      if (!isPendingKey(c)) continue;
      pending++;
      if (c.redundant) redundant++;
      const by = c.details?.redeemBy;
      if (!by) continue;
      const days = daysTo(today, by);
      if (days > EXPIRING_DAYS) continue;
      expiring++;
      if (!nearest || days < nearest.days) nearest = { title: g.title, days, gameId: g.id };
    }
  }

  const out: PendingItem[] = [];
  if (nearest) out.push({ kind: 'expiring', count: expiring, nearest, href: '#/library/expiring' });
  if (pending) out.push({ kind: 'pending', count: pending, href: '#/library/pending' });
  if (redundant) out.push({ kind: 'redundant', count: redundant, href: '#/library/redundant' });
  if (issues.length) {
    const level = issues.some((i) => i.level === 'error') ? 'error' : 'warning';
    out.push({ kind: 'health', count: issues.length, level, href: '#/system/health' });
  }
  return out;
}

/** The collection in figures. */
export interface CollectionSummary {
  games: number;
  /** Copies the user still has. */
  copies: number;
  /** Games added in the current month, in local time. */
  addedThisMonth: number;
  /** Games whose play status is "Backlog". */
  backlog: number;
  /** The newest games' ids, newest first. */
  recent: string[];
  /** Editions (a game on a system) per system, largest first; `rest` adds up the smaller ones. */
  systems: { system: string; count: number; rest: boolean }[];
}

const createdMs = (g: GameLike) => Number(g.createdAt?.seconds ?? 0n) * 1000;

export function collectionSummary(games: GameLike[], now: Date): CollectionSummary {
  let copies = 0;
  let addedThisMonth = 0;
  let backlog = 0;
  const bySystem = new Map<string, number>();

  for (const g of games) {
    const held = g.copies.filter(isHeld);
    copies += held.length;
    for (const system of new Set(held.map((c) => c.effectiveSystem).filter(Boolean))) {
      bySystem.set(system, (bySystem.get(system) ?? 0) + 1);
    }
    const created = new Date(createdMs(g));
    if (g.createdAt && created.getFullYear() === now.getFullYear() && created.getMonth() === now.getMonth()) addedThisMonth++;
    if (g.playStatus === PLAY_BACKLOG) backlog++;
  }

  const ranked = [...bySystem].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  const systems = ranked.slice(0, TOP_SYSTEMS).map(([system, count]) => ({ system, count, rest: false }));
  const rest = ranked.slice(TOP_SYSTEMS).reduce((n, [, count]) => n + count, 0);
  if (rest) systems.push({ system: '', count: rest, rest: true });

  const recent = [...games].sort((a, b) => createdMs(b) - createdMs(a)).slice(0, RECENT).map((g) => g.id);

  return { games: games.length, copies, addedThisMonth, backlog, recent, systems };
}

/** The collection's value: the first price source that priced copies (System shows them all). */
export function pickValue(totals: { copies: number; sell?: { amountMinor: bigint; currency: string } }[]):
  { amountMinor: bigint; currency: string } | null {
  const first = totals.find((x) => x.copies > 0 && x.sell);
  return first?.sell ? { amountMinor: first.sell.amountMinor, currency: first.sell.currency } : null;
}

/** What an external block shows. */
export type BlockMode = 'data' | 'notConfigured' | 'neverFetched';

export function blockMode(b?: { state: string }): BlockMode {
  if (!b || b.state === 'not_configured') return 'notConfigured';
  return b.state === 'never_fetched' ? 'neverFetched' : 'data';
}

/** The line under a stale block: how old its data is, and why it was not refreshed when known. */
export function staleLine(b: { state: string; errorKind: string; provider: string }, fetchedAt: Date | null, now: Date,
  locale: string, t: Translate): string | null {
  if (b.state !== 'stale') return null;
  const age = fetchedAt ? t('home.updated', { when: sinceText(fetchedAt, now, locale) }) : t('home.neverUpdated');
  return b.errorKind ? `${age} · ${t(`home.errors.${b.errorKind}`, { provider: b.provider })}` : age;
}

/** Whole days until a deal ends (0: today); null when it does not say or is over. */
export function endsInDays(expires: Date | null, now: Date): number | null {
  if (!expires || expires.getTime() <= now.getTime()) return null;
  return Math.floor((expires.getTime() - now.getTime()) / DAY);
}

/** Whether the user may refresh a block now. */
export function canRefresh(refreshableAt: Date | null, now: Date): boolean {
  return !refreshableAt || refreshableAt.getTime() <= now.getTime();
}

/** A library view the home page links to ("#/library/<target>"). */
export type LibraryPreset =
  | { quick: 'pending' | 'expiring' | 'redundant' }
  | { play: number }
  | { kind: number }
  | { sort: 'added' };

export function libraryPreset(target: string): LibraryPreset | null {
  switch (target) {
    case 'pending':
    case 'expiring':
    case 'redundant':
      return { quick: target };
    case 'backlog':
      return { play: PLAY_BACKLOG };
    case 'physical':
      return { kind: KIND_PHYSICAL };
    case 'recent':
      return { sort: 'added' };
    default:
      return null;
  }
}
```

`sinceText` gives "6 hours ago" and "3 days ago" in English for the two stale cases (it rounds from 90 minutes to hours and from 36 hours to days).

- [ ] **Step 5: Run the tests**

Run: `docker run --rm -v "$PWD:/src" -w /src/web -v gamevault-node-modules:/src/web/node_modules gamevault-toolchain:446777a3814f node --test --test-timeout=10000 tests/home.test.mjs tests/route.test.mjs tests/health.test.mjs tests/gameAddress.test.mjs`
Expected: PASS.

- [ ] **Step 6: The whole suite**

Run: `task test`
Expected: green. (`App.tsx` renders nothing for `'home'` until Task 10: the address `''` now shows an empty main area. That is fixed in the next task; do not deploy between the two.)

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/home.ts web/src/lib/route.ts web/tests/home.test.mjs web/tests/route.test.mjs
git commit -m "Add the home page's pure helpers and make Home the start address"
```

### Task 10: The Home page: navigation, the pending strip and the collection

**Files:**
- Create: `web/src/features/home/HomePage.tsx`, `PendingStrip.tsx`, `CollectionBlock.tsx`
- Modify: `web/src/App.tsx` (Home first in the sidebar and the tab bar; the page), `web/src/components/Icon.tsx` (`home`)
- Modify: `web/src/features/library/LibraryPage.tsx` (a `target` prop that applies `libraryPreset`)
- Modify: `web/src/api/client.ts` (`homeClient`)
- Modify: `web/src/styles.css` (a "Home" section), `web/src/i18n/locales/en.json`, `es.json` (`nav.home`, `home.*`)

**Interfaces:**
- Consumes: `pendingItems`, `collectionSummary`, `pickValue`, `libraryPreset` (Task 9); `HomeService` (Task 8, generated); `useAppData().games`, `useHealth().issues`, `valuationClient.getCollectionValue`, `Cover`, `gameHash`, `platformLook`, `formatAmount`, `replaceRoute`, `PageHeader`.
- Produces: `homeClient` (`api/client.ts`); `HomePage` (default export, `features/home/HomePage.tsx`) rendering `<PendingStrip>`, a slot for the external blocks (Task 11) and `<CollectionBlock>`; `LibraryPage({ target }: { target: string })`; CSS classes `.home`, `.home-pending`, `.home-chip`, `.home-block`, `.home-figures`, `.home-figure`, `.home-recent`, `.home-share`, `.home-share-legend`.

- [ ] **Step 1: Add the client and the icon**

`web/src/api/client.ts`: `import { HomeService } from '../gen/gamevault/v1/home_pb';` and `export const homeClient = createClient(HomeService, transport);` next to `healthClient`.

`web/src/components/Icon.tsx`, in `paths` after `library`:

```ts
  home: 'M4 11l8-7 8 7M6 9.5V20h12V9.5M10 20v-5h4v5',
```

- [ ] **Step 2: Write the pending strip** (`web/src/features/home/PendingStrip.tsx`)

```tsx
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import type { HealthIssue } from '../../gen/gamevault/v1/health_pb';
import { pendingItems, type PendingItem } from '../../lib/home';

/** What needs the user's attention, as chips that open the library view (or System) behind each:
 *  the counts use the library's own rules, so a chip says what its view shows. */
export function PendingStrip({ games, issues }: { games: Game[]; issues: HealthIssue[] }) {
  const { t } = useTranslation();
  const items = useMemo(() => pendingItems(games, issues, new Date()), [games, issues]);

  if (!items.length) return <p className="muted home-caught-up">{t('home.pending.none')}</p>;

  const when = (days: number) => (days < 0 ? t('home.pending.expired') : days === 0 ? t('home.pending.today') : t('home.pending.inDays', { count: days }));
  const label = (it: PendingItem) => {
    switch (it.kind) {
      case 'expiring':
        return t('home.pending.expiring', { count: it.count, title: it.nearest.title, when: when(it.nearest.days) });
      case 'health':
        return t('home.pending.health', { count: it.count });
      default:
        return t(`home.pending.${it.kind}`, { count: it.count });
    }
  };
  const tone = (it: PendingItem) => (it.kind === 'expiring' || (it.kind === 'health' && it.level === 'error') ? 'danger' : it.kind === 'health' ? 'warn' : '');

  return (
    <nav className="home-pending" aria-label={t('home.pending.label')}>
      {items.map((it) => <a key={it.kind} className={`home-chip ${tone(it)}`} href={it.href}>{label(it)}</a>)}
    </nav>
  );
}
```

- [ ] **Step 3: Write the collection block** (`web/src/features/home/CollectionBlock.tsx`)

```tsx
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { valuationClient } from '../../api/client';
import { Cover } from '../../components/Cover';
import { platformLook } from '../../components/PlatformBadge';
import type { Game } from '../../gen/gamevault/v1/game_pb';
import { gameHash } from '../../lib/gameRoute';
import { collectionSummary, pickValue } from '../../lib/home';
import { formatAmount } from '../../lib/money';

/** The collection at a glance: four figures that open their library views, the newest covers, and
 *  the share of each system. */
export function CollectionBlock({ games }: { games: Game[] }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const summary = useMemo(() => collectionSummary(games, new Date()), [games]);
  const byId = useMemo(() => new Map(games.map((g) => [g.id, g])), [games]);
  const [value, setValue] = useState<{ amountMinor: bigint; currency: string } | null>(null);

  // The value is only shown when some copies are priced; a failure just leaves the figure out.
  useEffect(() => {
    valuationClient.getCollectionValue({}).then((res) => setValue(pickValue(res.totals)), () => setValue(null));
  }, []);

  const n = (x: number) => x.toLocaleString(lang);
  const systemName = (s: { system: string; rest: boolean }) => (s.rest ? t('home.collection.otherSystems') : s.system);

  return (
    <section className="card home-block" aria-labelledby="home-collection">
      <h2 id="home-collection">{t('home.collection.title')}</h2>
      {summary.games === 0 ? (
        <p className="muted">{t('home.collection.empty')} <a href="#/sources">{t('home.collection.start')}</a></p>
      ) : (
        <>
          <div className="home-figures">
            <a className="home-figure" href="#/library">
              <strong>{n(summary.games)}</strong>
              <span>{t('home.collection.games', { count: summary.games })}</span>
              <span className="muted small">{t('home.collection.copies', { count: summary.copies, n: n(summary.copies) })}</span>
            </a>
            <a className="home-figure" href="#/library/recent">
              <strong>{n(summary.addedThisMonth)}</strong>
              <span>{t('home.collection.added')}</span>
            </a>
            <a className="home-figure" href="#/library/backlog">
              <strong>{n(summary.backlog)}</strong>
              <span>{t('home.collection.backlog')}</span>
            </a>
            {value && (
              <a className="home-figure" href="#/library/physical">
                <strong>{formatAmount(value.amountMinor, value.currency, lang)}</strong>
                <span>{t('home.collection.value')}</span>
              </a>
            )}
          </div>

          <h3>{t('home.collection.recent')}</h3>
          <ul className="home-recent">
            {summary.recent.map((id) => byId.get(id)).filter((g): g is Game => !!g).map((g) => (
              <li key={g.id}><a href={gameHash(g.id)} title={g.title} aria-label={g.title}><Cover game={g} /></a></li>
            ))}
          </ul>

          <h3>{t('home.collection.systems')}</h3>
          <div className="home-share" role="img"
            aria-label={summary.systems.map((s) => `${systemName(s)}: ${n(s.count)}`).join(', ')}>
            {summary.systems.map((s) => (
              <span key={s.system || 'rest'} style={{ flexGrow: s.count, background: s.rest ? 'var(--surface-3)' : platformLook(s.system).bg }} />
            ))}
          </div>
          <ul className="home-share-legend">
            {summary.systems.map((s) => (
              <li key={s.system || 'rest'}>
                <i style={{ background: s.rest ? 'var(--surface-3)' : platformLook(s.system).bg }} aria-hidden="true" />
                {systemName(s)} <span className="muted">{n(s.count)}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}
```

- [ ] **Step 4: Write the page** (`web/src/features/home/HomePage.tsx`)

```tsx
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../../components/PageHeader';
import { useAppData } from '../../state/AppData';
import { useHealth } from '../../state/Health';
import { CollectionBlock } from './CollectionBlock';
import { PendingStrip } from './PendingStrip';

/** The start page: what needs attention, the external blocks (deals, releases) and the collection
 *  at a glance. Pending and collection come from what the app already holds; nothing here is
 *  fetched from a service when the page opens. */
export default function HomePage() {
  const { t } = useTranslation();
  const { games } = useAppData();
  const { issues } = useHealth();

  return (
    <div className="page home">
      <PageHeader title={t('home.title')} />
      <PendingStrip games={games} issues={issues} />
      <CollectionBlock games={games} />
    </div>
  );
}
```

- [ ] **Step 5: Put it first** (`web/src/App.tsx`)

Import `HomePage from './features/home/HomePage'`. Change the brand link to `href="#/home"`. In the sidebar, before the library link:

```tsx
          <NavLink route="home" icon="home" active={route === 'home'} />
```

In `<main>`, before the library:

```tsx
            {route === 'home' && <HomePage />}
            {route === 'library' && <LibraryPage target={target} />}
```

In the tab bar, first:

```tsx
        <BottomLink route="home" icon="home" active={route === 'home'} />
```

- [ ] **Step 6: Let the library open the views the home page links to** (`web/src/features/library/LibraryPage.tsx`)

Change the signature to `export default function LibraryPage({ target }: { target: string })`, import `libraryPreset` from `'../../lib/home'` and `replaceRoute` from `'../../lib/useHashRoute'`, and add after the `setFilters`/`setSortBy` state:

```tsx
  // A home page link names a ready view ("#/library/pending"): it is applied once, then the address
  // goes back to the plain library, so a later change of view is not undone by a re-render.
  useEffect(() => {
    const preset = libraryPreset(target);
    if (!preset) return;
    if ('quick' in preset) {
      setFilters(NO_FILTERS);
      setQuick(preset.quick);
    } else if ('play' in preset) {
      setQuick(null);
      setFilters({ ...NO_FILTERS, play: [preset.play] });
    } else if ('kind' in preset) {
      setQuick(null);
      setFilters({ ...NO_FILTERS, kind: preset.kind });
    } else {
      setSortBy(preset.sort);
    }
    replaceRoute('#/library');
  }, [target, setSortBy]);
```

- [ ] **Step 7: Style it** (`web/src/styles.css`, a new section after "Sources")

```css
/* ---------- Home ---------- */
.home { display: grid; gap: 20px; max-width: 1180px; }
.home .page-head { margin-bottom: 0; }
.home-block h2 { margin: 0 0 12px; }
.home-block h3 { margin: 18px 0 8px; font-size: 14px; }
.home-caught-up { margin: 0; }
.home-pending { display: flex; flex-wrap: wrap; gap: 8px; }
.home-chip {
  display: inline-flex; align-items: center; padding: 7px 14px; border-radius: var(--radius-pill);
  background: var(--surface); border: 1px solid var(--border); color: var(--text); text-decoration: none; font-weight: 600;
  transition: transform 160ms var(--ease-out);
}
.home-chip.warn { background: var(--warn-bg); color: var(--warn); border-color: transparent; }
.home-chip.danger { background: var(--danger-bg); color: var(--danger); border-color: transparent; }
.home-chip:active { transform: scale(0.97); }
.home-figures { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
.home-figure {
  display: grid; gap: 2px; align-content: start; padding: 12px 14px; border-radius: var(--radius);
  background: var(--surface-2); color: var(--text); text-decoration: none; transition: transform 160ms var(--ease-out);
}
.home-figure strong { font-size: 26px; font-weight: 800; font-stretch: 118%; line-height: 1.1; }
.home-figure:active { transform: scale(0.97); }
.home-recent { display: grid; grid-template-columns: repeat(8, minmax(0, 1fr)); gap: 10px; list-style: none; margin: 0; padding: 0; }
.home-recent a { display: block; border-radius: var(--radius-sm); }
.home-share { display: flex; height: 12px; border-radius: var(--radius-pill); overflow: hidden; background: var(--surface-2); }
.home-share > span { min-width: 3px; }
.home-share-legend { display: flex; flex-wrap: wrap; gap: 4px 16px; list-style: none; margin: 8px 0 0; padding: 0; font-size: 13px; }
.home-share-legend i { display: inline-block; width: 10px; height: 10px; border-radius: 3px; margin-inline-end: 6px; vertical-align: -1px; }

/* The page fades in once when it opens; nothing else on it moves by itself. */
@keyframes home-fade-in { from { opacity: 0; } }
.home > * { animation: home-fade-in 240ms var(--ease-out) both; }

@media (max-width: 860px) {
  .home-figures { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .home-recent { grid-template-columns: repeat(4, minmax(0, 1fr)); }
}

@media (prefers-reduced-motion: reduce) {
  .home > * { animation: none; }
}
```

- [ ] **Step 8: Translate** (`en.json`, `es.json`)

Add `"home": "Home"` / `"home": "Inicio"` to `nav`, and a top-level `home` object:

en:

```json
  "home": {
    "title": "Home",
    "pending": {
      "label": "Needs your attention",
      "none": "All caught up",
      "expiring_one": "1 key expires soon · {{title}}, {{when}}",
      "expiring_other": "{{count}} keys expire soon · first {{title}}, {{when}}",
      "today": "today",
      "inDays_one": "in {{count}} day",
      "inDays_other": "in {{count}} days",
      "expired": "deadline passed",
      "pending_one": "1 key to redeem",
      "pending_other": "{{count}} keys to redeem",
      "redundant_one": "1 key you don't need",
      "redundant_other": "{{count}} keys you don't need",
      "health_one": "1 problem to look at",
      "health_other": "{{count}} problems to look at"
    },
    "collection": {
      "title": "Your collection",
      "games_one": "game",
      "games_other": "games",
      "copies_one": "{{n}} copy",
      "copies_other": "{{n}} copies",
      "added": "added this month",
      "backlog": "in your backlog",
      "value": "your physical games are worth",
      "recent": "Recently added",
      "systems": "By system",
      "otherSystems": "Other systems",
      "empty": "Your collection is empty.",
      "start": "Connect a store or add your first game"
    }
  },
```

es:

```json
  "home": {
    "title": "Inicio",
    "pending": {
      "label": "Requiere tu atención",
      "none": "Todo al día",
      "expiring_one": "1 clave caduca pronto · {{title}}, {{when}}",
      "expiring_other": "{{count}} claves caducan pronto · la primera {{title}}, {{when}}",
      "today": "hoy",
      "inDays_one": "en {{count}} día",
      "inDays_other": "en {{count}} días",
      "expired": "plazo vencido",
      "pending_one": "1 clave por canjear",
      "pending_other": "{{count}} claves por canjear",
      "redundant_one": "1 clave que no necesitas",
      "redundant_other": "{{count}} claves que no necesitas",
      "health_one": "1 problema que revisar",
      "health_other": "{{count}} problemas que revisar"
    },
    "collection": {
      "title": "Tu colección",
      "games_one": "juego",
      "games_other": "juegos",
      "copies_one": "{{n}} copia",
      "copies_other": "{{n}} copias",
      "added": "añadidos este mes",
      "backlog": "pendientes de jugar",
      "value": "valen tus juegos físicos",
      "recent": "Añadidos recientemente",
      "systems": "Por sistema",
      "otherSystems": "Otros sistemas",
      "empty": "Tu colección está vacía.",
      "start": "Conecta una tienda o añade tu primer juego"
    }
  },
```

- [ ] **Step 9: Type check, translations, Node tests**

Run: `task test`
Expected: green (TS types, `task i18n`'s check, every Node test).

- [ ] **Step 10: Look at it on the test server**

Run: `task test-server`, then open `http://127.0.0.1:8093/?v=1` in the browser pane. Check: the page opens on Home; the sidebar and the tab bar list Home first; the pending chips open the library already filtered (Pending, Expiring, Not needed) and the address becomes `#/library`; the figures open All / sorted by date added / Backlog / Physical; a recent cover opens its sheet. Then `resize_window` preset `mobile`, reload, check the 2×2 figures and the four tabs; reset with preset `desktop`. Stop with `task test-server:stop`. Use only `get_page_text`, `find` and screenshots; report no counts or titles from the copy anywhere.

- [ ] **Step 11: Lint and commit**

Run: `task lint`
Expected: 0 issues (oxlint's hook rules included).

```bash
git add web/src
git commit -m "Open on a Home page with what needs attention and the collection at a glance"
```

### Task 11: The deals and releases blocks, their states, and their provider chains

**Files:**
- Create: `web/src/features/home/ExternalBlock.tsx`, `Carousel.tsx`, `DealsBlock.tsx`, `ReleasesBlock.tsx`
- Modify: `web/src/features/home/HomePage.tsx` (load `GetHome`, render both blocks)
- Modify: `web/src/features/providers/ProvidersPage.tsx` (chains `deals` and `releases`)
- Modify: `web/src/styles.css` (carousel, cards, states), `web/src/i18n/locales/en.json`, `es.json` (`home.*`, `providers.deals*`, `providers.releases*`)

**Interfaces:**
- Consumes: `homeClient` (Task 10); generated `GetHomeResponse`, `HomeBlock`, `HomeDeal`, `HomeRelease` (Task 8); `blockMode`, `staleLine`, `endsInDays`, `canRefresh` (Task 9); `proxiedImage`, `formatAmount`, `useFormatters`, `toDate`, `gameHash`, `useAction`, `Icon` (`prev`, `next`, `refresh`, `external`).
- Produces: `ExternalBlock` (block chrome: title, Refresh, stale line, the three empty states), `Carousel` (scroll-snap list with arrows), `DealsBlock`, `ReleasesBlock`; the Providers page lists "Deals" and "Upcoming releases" chains after "Second-hand prices".

- [ ] **Step 1: Write the block chrome** (`web/src/features/home/ExternalBlock.tsx`)

```tsx
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from '../../components/Icon';
import { Alert } from '../../components/ui';
import type { HomeBlock } from '../../gen/gamevault/v1/home_pb';
import { blockMode, canRefresh, staleLine } from '../../lib/home';
import { toDate } from '../../lib/model';

/** The frame of an external block: its title and Refresh button, then either a quiet skeleton
 *  (while the page or a refresh loads), a "not configured" card that links to Providers, a "not
 *  fetched yet" card, or the data with a line saying how old it is when it is stale. */
export function ExternalBlock(props: {
  id: string;
  title: string;
  block: HomeBlock | undefined;
  /** The page's first read is still running. */
  loading: boolean;
  refreshing: boolean;
  refreshError: string;
  onRefresh: () => void;
  /** What the block brings, shown when no provider is enabled. */
  notConfigured: string;
  children: ReactNode;
}) {
  const { t, i18n } = useTranslation();
  const { block } = props;
  const now = new Date();
  const mode = blockMode(block);
  const refreshable = !!block && mode !== 'notConfigured' && canRefresh(toDate(block.refreshableAt), now);
  const stale = block ? staleLine(block, toDate(block.fetchedAt), now, i18n.language, t) : null;

  return (
    <section className="card home-block" aria-labelledby={props.id} aria-busy={props.loading || props.refreshing}>
      <div className="home-block-head">
        <h2 id={props.id}>{props.title}</h2>
        {block && mode !== 'notConfigured' && (
          <button className="home-refresh" onClick={props.onRefresh} disabled={!refreshable || props.refreshing}
            title={refreshable ? undefined : t('home.refreshSoon')}>
            <Icon name="refresh" size={16} /> {props.refreshing ? t('home.refreshing') : t('home.refresh')}
          </button>
        )}
      </div>
      {props.refreshError && <Alert tone="error">{props.refreshError}</Alert>}
      {props.loading || (props.refreshing && mode === 'neverFetched') ? (
        <div className="home-skeleton" aria-hidden="true"><span /><span /><span /><span /></div>
      ) : mode === 'notConfigured' ? (
        <div className="home-empty">
          <p>{props.notConfigured}</p>
          <a className="button" href="#/providers">{t('home.configure')}</a>
        </div>
      ) : mode === 'neverFetched' ? (
        <div className="home-empty"><p className="muted">{t('home.neverFetched', { provider: block?.provider ?? '' })}</p></div>
      ) : (
        <>
          {stale && <p className="muted small home-stale">{stale}</p>}
          {props.children}
        </>
      )}
    </section>
  );
}
```

`toDate` (in `lib/model.ts`) takes an optional `Timestamp` and returns `Date | null`.

- [ ] **Step 2: Write the carousel** (`web/src/features/home/Carousel.tsx`)

```tsx
import { useRef, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Icon } from '../../components/Icon';

/** A row of cards that scrolls sideways with snap points: swiped on phones, with arrows on hover on
 *  desktops. The arrows scroll by most of a view; reduced motion jumps instead of gliding. */
export function Carousel({ label, children }: { label: string; children: ReactNode }) {
  const { t } = useTranslation();
  const track = useRef<HTMLUListElement>(null);

  const scroll = (direction: 1 | -1) => {
    const el = track.current;
    if (!el) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    el.scrollBy({ left: direction * el.clientWidth * 0.9, behavior: reduced ? 'auto' : 'smooth' });
  };

  return (
    <div className="carousel">
      <button className="carousel-arrow prev" onClick={() => scroll(-1)} aria-label={t('home.previous')}><Icon name="prev" size={18} /></button>
      <ul ref={track} className="carousel-track" aria-label={label}>{children}</ul>
      <button className="carousel-arrow next" onClick={() => scroll(1)} aria-label={t('home.next')}><Icon name="next" size={18} /></button>
    </div>
  );
}
```

- [ ] **Step 3: Write the deals block** (`web/src/features/home/DealsBlock.tsx`)

```tsx
import { useTranslation } from 'react-i18next';
import { proxiedImage } from '../../api/client';
import type { HomeBlock, HomeDeal } from '../../gen/gamevault/v1/home_pb';
import { endsInDays } from '../../lib/home';
import { toDate } from '../../lib/model';
import { formatAmount } from '../../lib/money';
import { Carousel } from './Carousel';
import { ExternalBlock } from './ExternalBlock';

/** ITAD's terms ask for a credit and links through ITAD; prices and links are shown as given. */
const ITAD_URL = 'https://isthereanydeal.com/';

export function DealsBlock(props: {
  block: HomeBlock | undefined;
  featured: HomeDeal[];
  lows: HomeDeal[];
  loading: boolean;
  refreshing: boolean;
  refreshError: string;
  onRefresh: () => void;
}) {
  const { t } = useTranslation();

  return (
    <ExternalBlock id="home-deals" title={t('home.deals.title')} block={props.block} loading={props.loading}
      refreshing={props.refreshing} refreshError={props.refreshError} onRefresh={props.onRefresh}
      notConfigured={t('home.deals.notConfigured')}>
      <h3>{t('home.deals.featured')}</h3>
      <DealRow deals={props.featured} label={t('home.deals.featured')} />
      <h3>{t('home.deals.lows')}</h3>
      <DealRow deals={props.lows} label={t('home.deals.lows')} />
      <p className="muted small home-credit">
        {t('home.deals.credit')} <a href={ITAD_URL} target="_blank" rel="noopener noreferrer">IsThereAnyDeal</a>
      </p>
    </ExternalBlock>
  );
}

function DealRow({ deals, label }: { deals: HomeDeal[]; label: string }) {
  const { t } = useTranslation();
  if (!deals.length) return <p className="muted">{t('home.deals.none')}</p>;
  return <Carousel label={label}>{deals.map((d, i) => <DealCard key={`${d.title}-${d.shop}-${i}`} deal={d} />)}</Carousel>;
}

function DealCard({ deal: d }: { deal: HomeDeal }) {
  const { t, i18n } = useTranslation();
  const lang = i18n.language;
  const ends = endsInDays(toDate(d.expiresAt), new Date());
  const price = d.price ? formatAmount(d.price.amountMinor, d.price.currency, lang) : t('home.deals.free');
  const regular = d.regular && d.cut > 0 ? formatAmount(d.regular.amountMinor, d.regular.currency, lang) : '';

  const body = (
    <>
      <div className="deal-art">{d.imageUrl ? <img src={proxiedImage(d.imageUrl)} alt="" loading="lazy" /> : null}</div>
      <strong className="deal-title">{d.title}</strong>
      <span className="muted small">{d.shop}</span>
      <span className="deal-price">
        <b>{price}</b>
        {regular && <s className="muted">{regular}</s>}
        {d.cut > 0 && <span className="badge">{t('home.deals.cut', { cut: d.cut })}</span>}
      </span>
      {d.historicalLow && <span className="badge low">{t('home.deals.historicalLow')}</span>}
      {ends !== null && <span className="muted small">{ends === 0 ? t('home.deals.endsToday') : t('home.deals.endsIn', { count: ends })}</span>}
      {d.ownedOn.length > 0 && <span className="small">{t('home.deals.ownedOn', { systems: d.ownedOn.join(', ') })}</span>}
    </>
  );

  return (
    <li className="deal-card">
      {d.url
        ? <a className="deal-link" href={d.url} target="_blank" rel="noopener noreferrer">{body}</a>
        : <div className="deal-link">{body}</div>}
    </li>
  );
}
```

- [ ] **Step 4: Write the releases block** (`web/src/features/home/ReleasesBlock.tsx`)

```tsx
import { useTranslation } from 'react-i18next';
import { proxiedImage } from '../../api/client';
import { useFormatters } from '../../components/ui';
import type { HomeBlock, HomeRelease } from '../../gen/gamevault/v1/home_pb';
import { gameHash } from '../../lib/gameRoute';
import { Carousel } from './Carousel';
import { ExternalBlock } from './ExternalBlock';

export function ReleasesBlock(props: {
  block: HomeBlock | undefined;
  fromSeries: HomeRelease[];
  anticipated: HomeRelease[];
  loading: boolean;
  refreshing: boolean;
  refreshError: string;
  onRefresh: () => void;
}) {
  const { t } = useTranslation();

  return (
    <ExternalBlock id="home-releases" title={t('home.releases.title')} block={props.block} loading={props.loading}
      refreshing={props.refreshing} refreshError={props.refreshError} onRefresh={props.onRefresh}
      notConfigured={t('home.releases.notConfigured')}>
      <h3>{t('home.releases.series')}</h3>
      <ReleaseRow releases={props.fromSeries} label={t('home.releases.series')} none={t('home.releases.noneSeries')} />
      <h3>{t('home.releases.anticipated')}</h3>
      <ReleaseRow releases={props.anticipated} label={t('home.releases.anticipated')} none={t('home.releases.noneAnticipated')} />
    </ExternalBlock>
  );
}

function ReleaseRow({ releases, label, none }: { releases: HomeRelease[]; label: string; none: string }) {
  if (!releases.length) return <p className="muted">{none}</p>;
  return <Carousel label={label}>{releases.map((r, i) => <ReleaseCard key={`${r.title}-${i}`} release={r} />)}</Carousel>;
}

function ReleaseCard({ release: r }: { release: HomeRelease }) {
  const { t } = useTranslation();
  const fmt = useFormatters();
  const when = r.releaseDate ? fmt.date(r.releaseDate) : t('home.releases.tba');

  const body = (
    <>
      <div className="release-cover">{r.coverUrl ? <img src={proxiedImage(r.coverUrl)} alt="" loading="lazy" /> : null}</div>
      <strong className="deal-title">{r.title}</strong>
      <span className="muted small">{[when, r.platforms.join(', ')].filter(Boolean).join(' · ')}</span>
    </>
  );

  // Two links side by side, never nested: the card opens the database's page, the reason the
  // owned game's sheet.
  return (
    <li className="deal-card release-card">
      {r.url
        ? <a className="deal-link" href={r.url} target="_blank" rel="noopener noreferrer">{body}</a>
        : <div className="deal-link">{body}</div>}
      {r.becauseGameId && <a className="small release-because" href={gameHash(r.becauseGameId)}>{t('home.releases.because', { title: r.becauseTitle })}</a>}
    </li>
  );
}
```

- [ ] **Step 5: Load and render them** (`web/src/features/home/HomePage.tsx`, replacing the file)

```tsx
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, homeClient } from '../../api/client';
import { PageHeader } from '../../components/PageHeader';
import { Alert } from '../../components/ui';
import type { GetHomeResponse } from '../../gen/gamevault/v1/home_pb';
import { useAction } from '../../lib/useAction';
import { useAppData } from '../../state/AppData';
import { useHealth } from '../../state/Health';
import { CollectionBlock } from './CollectionBlock';
import { DealsBlock } from './DealsBlock';
import { PendingStrip } from './PendingStrip';
import { ReleasesBlock } from './ReleasesBlock';

/** The start page: what needs attention, current deals, upcoming releases and the collection at a
 *  glance. Deals and releases are read from what the server stored (gathered in the background);
 *  opening the page never asks a service. Refresh asks one now. */
export default function HomePage() {
  const { t } = useTranslation();
  const { games } = useAppData();
  const { issues, reload: reloadHealth } = useHealth();
  const [home, setHome] = useState<GetHomeResponse | null>(null);
  const [error, setError] = useState('');
  const deals = useAction();
  const releases = useAction();

  const load = useCallback(async () => {
    try {
      setHome(await homeClient.getHome({}));
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // A refresh answers with the whole home; a provider problem it met reaches System → Health too.
  const refresh = (action: typeof deals, block: 'deals' | 'releases') => action.run(async () => {
    const res = await homeClient.refreshHome({ block });
    if (res.home) setHome(res.home);
    reloadHealth();
  });

  const loading = home === null && !error;

  return (
    <div className="page home">
      <PageHeader title={t('home.title')} />
      <PendingStrip games={games} issues={issues} />
      {error && <Alert tone="error">{error}</Alert>}
      <DealsBlock block={home?.deals} featured={home?.featured ?? []} lows={home?.historicalLows ?? []} loading={loading}
        refreshing={deals.busy} refreshError={deals.error} onRefresh={() => refresh(deals, 'deals')} />
      <ReleasesBlock block={home?.releases} fromSeries={home?.fromSeries ?? []} anticipated={home?.anticipated ?? []} loading={loading}
        refreshing={releases.busy} refreshError={releases.error} onRefresh={() => refresh(releases, 'releases')} />
      <CollectionBlock games={games} />
    </div>
  );
}
```

- [ ] **Step 6: Style them** (`web/src/styles.css`, appended to the "Home" section, before its media queries)

```css
.home-block-head { display: flex; align-items: center; gap: 12px; justify-content: space-between; }
.home-block-head h2 { margin: 0; }
.home-refresh { display: inline-flex; align-items: center; gap: 6px; }
.home-stale { margin: 4px 0 0; }
.home-credit { margin: 14px 0 0; }
.home-empty { display: grid; justify-items: start; gap: 10px; padding: 8px 0 4px; }
.home-empty p { margin: 0; max-width: 60ch; }
.home-skeleton { display: flex; gap: 12px; overflow: hidden; padding-top: 12px; }
.home-skeleton span { flex: 0 0 168px; height: 236px; border-radius: var(--radius); background: var(--surface-2); }

.carousel { position: relative; }
.carousel-track {
  display: grid; grid-auto-flow: column; grid-auto-columns: 168px; gap: 12px; overflow-x: auto; overscroll-behavior-x: contain;
  scroll-snap-type: x mandatory; list-style: none; margin: 0; padding: 2px 2px 8px; scrollbar-width: thin;
}
.carousel-track > li { scroll-snap-align: start; min-width: 0; }
.carousel-arrow {
  position: absolute; top: 40%; z-index: 1; display: none; width: 36px; height: 36px; border-radius: var(--radius-pill);
  background: var(--surface); border: 1px solid var(--border); box-shadow: var(--shadow-md); align-items: center; justify-content: center;
}
.carousel-arrow.prev { left: -10px; }
.carousel-arrow.next { right: -10px; }
@media (hover: hover) and (pointer: fine) {
  .carousel:hover .carousel-arrow { display: inline-flex; }
}

.deal-card { display: grid; gap: 4px; align-content: start; }
.deal-link { display: grid; gap: 4px; color: var(--text); text-decoration: none; border-radius: var(--radius); }
.deal-art, .release-cover { aspect-ratio: 460 / 215; border-radius: var(--radius-sm); background: var(--surface-2); overflow: hidden; }
.release-cover { aspect-ratio: 2 / 3; }
.deal-art img, .release-cover img { width: 100%; height: 100%; object-fit: cover; display: block; }
.deal-title { font-size: 14px; line-height: 1.25; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.deal-price { display: flex; flex-wrap: wrap; align-items: baseline; gap: 6px; }
.deal-price s { font-size: 13px; }
.release-because { color: var(--link); }
.badge.low { background: var(--ok-bg); color: var(--ok); justify-self: start; }
@media (hover: hover) and (pointer: fine) {
  .deal-link:hover .deal-title { text-decoration: underline; }
}
```

ITAD's box art is a 460×215 capsule; IGDB's covers are 2:3.

- [ ] **Step 7: List the two chains on the Providers page** (`web/src/features/providers/ProvidersPage.tsx`)

Widen `ProviderChain`'s `kind` to `'cover' | 'barcode' | 'metadata' | 'valuation' | 'deals' | 'releases'` and add after the valuation chain:

```tsx
      <ProviderChain kind="deals" healthKey={healthKey} title={t('providers.deals')} intro={t('providers.dealsIntro')} onNotice={setNotice} />

      <ProviderChain kind="releases" healthKey={healthKey} title={t('providers.releases')} intro={t('providers.releasesIntro')} onNotice={setNotice} />
```

- [ ] **Step 8: Translate** (`en.json`, `es.json`)

Inside `home`, next to `pending` and `collection`:

en:

```json
    "refresh": "Refresh",
    "refreshing": "Refreshing…",
    "refreshSoon": "Refreshed less than 15 minutes ago",
    "updated": "Updated {{when}}",
    "neverUpdated": "Not updated yet",
    "neverFetched": "Nothing here yet: {{provider}} is asked in the background every few hours, or now with Refresh.",
    "configure": "Set up on Providers",
    "previous": "Previous",
    "next": "Next",
    "errors": {
      "rate_limited": "{{provider}} asked to slow down",
      "quota": "{{provider}} has used up its allowance",
      "settings": "{{provider}} rejects the key: check it on Providers",
      "unavailable": "{{provider}} is not answering",
      "failed": "{{provider}} could not be read"
    },
    "deals": {
      "title": "Deals",
      "featured": "Featured now",
      "lows": "Historical lows",
      "none": "No deals right now for games you don't own.",
      "free": "Free",
      "cut": "-{{cut}}%",
      "historicalLow": "Historical low",
      "endsToday": "Ends today",
      "endsIn_one": "Ends in {{count}} day",
      "endsIn_other": "Ends in {{count}} days",
      "ownedOn": "You have it on {{systems}}",
      "credit": "Prices from",
      "notConfigured": "Add a free IsThereAnyDeal key to see the deals of the day, without the games you already own."
    },
    "releases": {
      "title": "Upcoming releases",
      "series": "From your series",
      "anticipated": "Most anticipated",
      "noneSeries": "Nothing announced in your series.",
      "noneAnticipated": "Nothing announced for the next 60 days.",
      "because": "Because you have {{title}}",
      "tba": "Date to be announced",
      "notConfigured": "Turn on IGDB releases to see what comes out soon in your series and the most anticipated games."
    },
```

es:

```json
    "refresh": "Actualizar",
    "refreshing": "Actualizando…",
    "refreshSoon": "Actualizado hace menos de 15 minutos",
    "updated": "Actualizado {{when}}",
    "neverUpdated": "Aún sin actualizar",
    "neverFetched": "Aún no hay nada: se pregunta a {{provider}} en segundo plano cada pocas horas, o ahora con Actualizar.",
    "configure": "Configurar en Proveedores",
    "previous": "Anterior",
    "next": "Siguiente",
    "errors": {
      "rate_limited": "{{provider}} pidió ir más despacio",
      "quota": "{{provider}} agotó su cupo de peticiones",
      "settings": "{{provider}} rechaza la clave: revísala en Proveedores",
      "unavailable": "{{provider}} no responde",
      "failed": "No se pudo leer {{provider}}"
    },
    "deals": {
      "title": "Ofertas",
      "featured": "Destacadas ahora",
      "lows": "Mínimos históricos",
      "none": "Ahora mismo no hay ofertas de juegos que no tengas.",
      "free": "Gratis",
      "cut": "-{{cut}} %",
      "historicalLow": "Mínimo histórico",
      "endsToday": "Termina hoy",
      "endsIn_one": "Termina en {{count}} día",
      "endsIn_other": "Termina en {{count}} días",
      "ownedOn": "Lo tienes en {{systems}}",
      "credit": "Precios de",
      "notConfigured": "Añade una clave gratuita de IsThereAnyDeal para ver las ofertas del día, sin los juegos que ya tienes."
    },
    "releases": {
      "title": "Próximos lanzamientos",
      "series": "De tus series",
      "anticipated": "Los más esperados",
      "noneSeries": "No hay nada anunciado en tus series.",
      "noneAnticipated": "No hay nada anunciado para los próximos 60 días.",
      "because": "Porque tienes {{title}}",
      "tba": "Fecha por anunciar",
      "notConfigured": "Activa los lanzamientos de IGDB para ver lo que sale pronto de tus series y los juegos más esperados."
    },
```

Inside `providers`: en `"deals": "Deals"`, `"dealsIntro": "Shown on the home page. Asked in the background every 6 hours, never when you open the page."`, `"releases": "Upcoming releases"`, `"releasesIntro": "Shown on the home page. Asked in the background once a day."`; es `"deals": "Ofertas"`, `"dealsIntro": "Se muestran en la página de inicio. Se piden en segundo plano cada 6 horas, nunca al abrir la página."`, `"releases": "Próximos lanzamientos"`, `"releasesIntro": "Se muestran en la página de inicio. Se piden en segundo plano una vez al día."`. (Neither key exists yet under `providers`.)

- [ ] **Step 9: Type check, translations, Node tests**

Run: `task test`
Expected: green.

- [ ] **Step 10: Look at it on the test server** (no real key is entered; the copy starts with `-no-unattended`)

Run: `task test-server`, open `http://127.0.0.1:8093/?v=2`. Check, desktop then the `mobile` preset (reload), then reset to `desktop`:
- Deals and releases show the "not configured" card with "Set up on Providers" (or, if the copy has IGDB credentials, releases stay "not configured" until IGDB releases is turned on: it does not switch on with its siblings).
- Providers lists "Deals" (IsThereAnyDeal, off) and "Upcoming releases" (IGDB, off) after "Second-hand prices".
- In ITAD's dialog, "Test connection" with the bogus key `bogus0000` says the key was rejected (the real 403, through `ErrBadKey`). Do not save a key.
- Enable nothing else. Do not press Refresh on releases with the copy's IGDB credentials: the copy must not send the user's library to IGDB on its own; checking it is the user's call (see the final task).

Stop with `task test-server:stop`.

- [ ] **Step 11: Lint and commit**

Run: `task lint`
Expected: 0 issues.

```bash
git add web/src
git commit -m "Show deals and upcoming releases on Home, with their states, Refresh and provider chains"
```

### Task 12: Docs, memory and the README line

**Files:**
- Modify: `docs/technical.md` (a new "Home page" section before "Browsing the library"; a line in "Browsing the library" for the `#/library/<view>` addresses)
- Modify: `docs/plugins.md` (two rows in the pieces table; a "Rules for home page providers" section)
- Modify: `README.md` (one bullet)
- Create: `.claude/memory/itad.md`
- Modify: `.claude/MEMORY.md`, `.claude/memory/provider-chains.md`, `.claude/memory/igdb.md`, `.claude/memory/unattended-requests.md`, `.claude/docs/ui.md`

**Interfaces:**
- Consumes: the behaviour of Tasks 1–11 (names, intervals, endpoints).
- Produces: documentation only.

- [ ] **Step 1: `docs/technical.md` — add the section** (before `## Browsing the library`)

```markdown
## Home page

Game Vault opens on **Home** (`#/home`; the empty address and unknown ones go there too). Top to
bottom:

- **Needs your attention** (shown only when something is pending; otherwise "All caught up"): keys
  that expire soon (the library's "Expiring" rule: within 30 days, past deadlines included) with the
  nearest one, keys still to redeem, keys you don't need, and the Health problems. Each chip opens
  the library already filtered (`#/library/expiring`, `#/library/pending`, `#/library/redundant`) or
  System → Health. The web counts them from the games it already holds, with the library's own
  helpers (`web/src/lib/home.ts`), so a chip always says what its view shows.
- **Deals** (provider kind `deals`, IsThereAnyDeal): "Featured now" and "Historical lows", 12 each,
  without the games you own on PC. A deal is yours when an owned game has its Steam app (ITAD's Steam
  lookup) or exactly its title (`game.SameTitle`, any script); owned means a copy you still have
  (not sold, given away or expired). A game you have only on consoles stays, with a note ("You have
  it on PS4"). The filter runs when the page is read, so a game bought since the last refresh is
  gone at once. Cards show ITAD's art (through the image proxy), the shop, the price with the cut
  and the regular price, a "Historical low" badge and "Ends in N days" when known, and open the deal
  through ITAD's own link (https only), unchanged. A credit line links to IsThereAnyDeal.
- **Upcoming releases** (provider kind `releases`, IGDB): "From your series" (upcoming entries of
  the collections and franchises of games you own, each with "Because you have X", which opens X's
  sheet) and "Most anticipated" (main games of the next 60 days with the most follows before
  release). Releases already out, or explained by a game no longer in the catalog, are left out
  when the page is read.
- **Your collection:** games and copies, games added this month (local time), games in the backlog,
  the value of physical copies (the first price source with estimates; hidden without any), the 8
  newest covers, and the share of each system (editions per system, the five largest and the rest).
  Each figure opens its library view (`#/library`, `#/library/recent`, `#/library/backlog`,
  `#/library/physical`).

**How deals and releases are gathered.** `internal/application/home` keeps one snapshot per block
in SQLite (`home_snapshots`: the lists as JSON, when they were gathered, and the last failure).
Reading the page (`HomeService/GetHome`) never asks a service. An unattended job asks the providers
of each block in the user's order until one answers: deals every 6 hours, releases every 24, each
±10 % (`unattended.Rhythm`); a block that is due at start-up is gathered at a random moment 1–5
minutes later, and a block without an enabled provider is looked at again about every hour, sending
nothing. `-no-unattended` turns the job off: the page shows what is stored, and only **Refresh**
asks a service. **Refresh** (`RefreshHome`) gathers one block now, at most once every 15 minutes
counted from the last attempt (failed ones too, and across restarts); a refresh the user asked for
counts as the job's next turn. A refresh is all or nothing: if a list fails, the previous snapshot
stays whole, the failure is recorded next to it and reaches the provider's status (System → Health).

**Block states:** *not configured* (no provider enabled; stored data is hidden, and a card links to
Providers), *never fetched* (a card with Refresh; the skeleton shows only while a request runs),
*stale* (the last attempt failed, or the data is older than twice its interval: "Updated 6 hours ago
· IsThereAnyDeal is not answering"), *fresh*.

**IsThereAnyDeal** (`internal/adapters/outbound/itad`, provider `itad`): ITAD's API v2 with your own
free API key (Providers → Deals; an app registered on isthereanydeal.com) and an optional country
(two letters; empty: the country of your default currency, euro → Germany, else the United States).
A refresh sends four requests: the deals list in ITAD's own order (`GET /deals/v2`, 40 games), the
deepest cuts (`sort=-cut`, 200) kept when at their historical low, and one Steam lookup per list
(`POST /lookup/shop/61/id/v1`). Only games are kept (no DLC or soundtracks), never mature ones. The
key travels in the `ITAD-API-Key` header. Requests are one a second at most, and a 429 or an outage
rests the provider for a minute or as long as `Retry-After` asks (ITAD allows 1000 requests per 5
minutes). ITAD's terms allow private personal use with your own key and forbid altering its data or
competing with it: prices and links are shown as given, and Game Vault has no price alerts of its
own. ITAD receives no collection data: only the deal requests, with the country, and lookups of its
own game ids.

**IGDB releases** (provider `igdb-releases`, settings shared with IGDB covers and details): off
until you turn it on, even with the Twitch application entered (it runs on a schedule). "Most
anticipated" is one `games` query; "From your series" finds your games by their Steam apps
(`external_games`, source 1, 500 at a time), then asks for the upcoming main games of their
collections and franchises (500 ids at a time). Games without a Steam link do not contribute series.
IGDB receives the Steam ids of the games you own to find their series (it already receives titles
and store ids for game sheets).
```

In `## Browsing the library`, after the `#/game/<id>` bullet:

```markdown
- `#/library/pending`, `#/library/expiring`, `#/library/redundant`, `#/library/backlog`,
  `#/library/physical` and `#/library/recent` open the library with that quick filter, play status,
  kind of copy or sort (the home page links to them); the address then goes back to `#/library`.
```

- [ ] **Step 2: `docs/plugins.md`**

Add two rows to the pieces table:

```markdown
| Deals provider | `home.DealsProvider` | Lists current deals for the home page | Providers → Deals |
| Releases provider | `home.ReleasesProvider` | Lists upcoming games for the home page | Providers → Upcoming releases |
```

and a section before `## Tests`:

```markdown
## Rules for home page providers

- A deals provider (`provider.KindDeals`) answers `Deals(ctx, settings, home.DealQuery)` for one
  list (`home.DealsFeatured`, `home.DealsHistoricalLows`) with the country and the size asked; it
  knows nothing of the collection (the home service removes owned games). It fills `SteamAppID` when
  the service knows it, so ownership is matched by Steam id as well as by title, and passes prices
  and links through unchanged.
- A releases provider (`provider.KindReleases`) answers `Anticipated(ctx, settings, window)` and
  `Sequels(ctx, settings, owned)`, where each `home.OwnedRef` carries an owned game's id, title and
  store links; every sequel names the owned game that explains it (`Release.Because`).
- Both kinds are asked on a schedule (`provider.Kind.Scheduled`): they never switch on with a
  sibling of the same settings group, and must stay well within the service's limits (a refresh is
  a handful of requests). Provider-level failures use the usual sentinels; they keep the last good
  data on the page and reach Health.
- Images they return are loaded through the image proxy: declare their hosts (`media.ImageHoster`).
```

- [ ] **Step 3: `README.md` — one bullet** (after "Keys are tracked…")

```markdown
- **A home page** that opens with what needs your attention, the deals of the day for games you don't
  own yet (with a free [IsThereAnyDeal](https://isthereanydeal.com) key) and the upcoming releases of
  your series (with IGDB).
```

- [ ] **Step 4: The new memory** (`.claude/memory/itad.md`)

```markdown
---
name: itad
description: IsThereAnyDeal API v2 — deals list, Steam lookup, key in a header, 403 on a bad key; terms (personal use, never alter data, never compete, cache, credit)
metadata:
  type: reference
---
As of 2026-10-15 (plugin `internal/adapters/outbound/itad`, provider `itad`, kind `deals`; see
[[provider-chains]], [[unattended-requests]]):
- **Terms (docs.isthereanydeal.com, read 2026-10-15):** private personal use is allowed with one's
  own key (commercial use only for publicly available apps; private non-personal use needs
  approval). Never alter the data: no removing affiliate tags, no changing prices (partial use and
  adding one's own data are fine). Never compete with ITAD or help a competitor: no price alerts or
  waitlist of Game Vault's own (alerts stay ITAD's). Cache rather than maxing the limit. Linking to
  IsThereAnyDeal or crediting its API is recommended (a "SHOULD"). Access may be revoked any time.
- **Limits:** 1000 requests per 5 minutes per account with a verified email; beyond, HTTP 429 with
  `Retry-After`; working around limits can lead to a ban. The adapter sends one request a second,
  rests a minute (or `Retry-After`) after a 429 or an outage, and a refresh is 4 requests every
  6 h ±10 %.
- **Auth:** key as `key` query parameter or `ITAD-API-Key` header; the adapter uses the header
  (request errors name URLs). Users get a key by registering an app at
  https://isthereanydeal.com/apps/my/ (redirects to /apps/ when signed out). OAuth with PKCE exists
  for user data (collection, waitlist): out of scope so far.
- **Probed with a bogus key (2026-10-15):** `GET /deals/v2` → HTTP 403
  `{"status_code":403,"reason_phrase":"Invalid or expired api key"}` (the key is checked before the
  parameters); no key → 403 `{"status_code":403,"reason_phrase":"Missing api key"}`;
  `POST /lookup/shop/61/id/v1` → the same 403; unknown path → 404 JSON. A bad key is 403, not 401.
- **Deals list:** `GET /deals/v2` with `country` (ISO alpha-2, default US), `offset`, `limit`
  (1–200, default 20), `sort` ("same as in deals list on the website"; documented: `-cut`, `price`),
  `nondeals`, `mature` (default false), `shops`, `filter` (lz-compressed base64 or URL-encoded JSON,
  semantics undocumented). Answer `{nextOffset, hasMore, list: [{id, slug, title, type ("game",
  "dlc", null…), mature, assets {boxart, banner145…600 on assets.isthereanydeal.com}, deal {shop {id,
  name}, price/regular/storeLow/historyLow/historyLow_1y/historyLow_3m {amount, amountInt,
  currency}, cut, voucher, flag ("H" in the sample at a historical low; null), drm, platforms,
  timestamp, expiry (RFC 3339 with offset, or null), url (https://itad.link/…)}}]}`. Featured =
  no `sort`; historical lows = `sort=-cut&limit=200`, kept when `flag == "H"` or price ≤ historyLow.
  Only `type == "game"` is kept. `assets` is decoded leniently (an empty array is plausible for a
  game without art).
- **Steam ids:** `POST /lookup/shop/61/id/v1` with a JSON array of ITAD ids →
  `{"<id>": ["app/…", "sub/…"] | null}`; the first `app/` is the Steam app.
- **Not verified with a real key yet:** the order of `/deals/v2` without `sort`, the meaning of
  `flag` beyond the sample, `assets` for games without art. Ask the user for the first real
  refresh's result before relying on them.
```

Add to `.claude/MEMORY.md` under "External services", after SteamGridDB:

```markdown
- [IsThereAnyDeal](memory/itad.md) — API v2 with the user's own key in `ITAD-API-Key`; bad key = 403; `/deals/v2` (featured = no sort, lows = `-cut` + historyLow) and Steam lookup; terms: personal use, never alter data, never compete (no alerts), cache, credit
```

- [ ] **Step 5: Update the other memories**

`.claude/memory/provider-chains.md`, a new bullet at the end:

```markdown
- **Home page kinds (2026-10-15):** `deals` (IsThereAnyDeal, [[itad]]) and `releases` (IGDB
  `igdb-releases`) joined `provider.Kinds()`. They feed `internal/application/home`, which keeps one
  snapshot per block (`home_snapshots`), refreshed by an unattended job (deals 6 h, releases 24 h,
  ±10 %) or the user's Refresh (15-minute guard, failed attempts included). `provider.Kind.Scheduled()`
  (valuation, deals, releases) keeps a provider off when a sibling of its settings group is on.
  Owned deals are removed when the page is read (Steam id or `game.SameTitle`; owned on PC = a held
  copy whose system is PC).
```

`.claude/memory/igdb.md`, a new bullet before "Awaits a test":

```markdown
- **Releases (2026-10-15):** `igdb-releases` (kind `releases`, off until turned on). Anticipated: one
  `games` query, `first_release_date` within 60 days, `hypes > 0`, `version_parent = null &
  parent_game = null`, `sort hypes desc; limit 40;`. Sequels: `external_games` by Steam uid
  (source 1) in batches of 500 with `game.franchise,game.franchises,game.collections`, then `games`
  with `collections = (…) | franchises = (…) | franchise = (…)` and `first_release_date > now`, 500 ids a
  batch. `hypes` = follows before release; `follows`, `category` and `collection` are deprecated;
  `limit` max 500. Only the documentation was read: no real query has run yet.
```

`.claude/memory/unattended-requests.md`, a new bullet:

```markdown
- The home page's job (`home.Service.Run`, 2026-10-15): deals every 6 h, releases every 24 h, ±10 %;
  an overdue block 1–5 min after start; a block without a provider is looked at about hourly and
  sends nothing; a user's Refresh counts as the next turn. Off with `-no-unattended`.
```

`.claude/docs/ui.md`, in "Structure", after the `App.tsx` bullet:

```markdown
- `features/home/`: `HomePage` (the start page: `PendingStrip`, `DealsBlock`, `ReleasesBlock`,
  `CollectionBlock`), `ExternalBlock` (title, Refresh, stale line and the not configured / never
  fetched / loading states), `Carousel` (scroll-snap row, arrows on hover). Pure helpers in
  `lib/home.ts` (Node-tested). Library views open from `#/library/<view>` (`libraryPreset`).
```

and change the `App.tsx` bullet's address list to start with `#/home` (the start page).

- [ ] **Step 6: Check nothing personal slipped in**

Run: `git diff --stat`, then read the diff of the docs and memory files.
Expected: only invented titles and ids, no names, accounts, library counts or values from the config copy.

- [ ] **Step 7: Translations and the whole suite**

Run: `task lint && task test`
Expected: green (the docs change nothing that runs; the run catches a stray edit).

- [ ] **Step 8: Commit**

```bash
git add docs README.md .claude
git commit -m "Document the home page, the deals and releases providers, and IsThereAnyDeal's terms"
```

### Task 13: The final check on the test server

**Files:**
- None created. Fixes found here go into the task that owns the code, as a new commit.

**Interfaces:**
- Consumes: the whole branch.
- Produces: a verified branch and a report for the user (what was verified for real, what only against fakes).

- [ ] **Step 1: The full suite, the race detector and generated code**

Run: `task lint && task test && task test:race && task generate && git status --porcelain`
Expected: 0 issues; all green; `git status` empty after `task generate` (the generated code is current).

- [ ] **Step 2: The bogus-key probes** (never a real key)

Run:

```bash
curl -sS -m 20 -w ' [%{http_code}]\n' -H 'ITAD-API-Key: bogus0000' 'https://api.isthereanydeal.com/deals/v2?limit=1'
curl -sS -m 20 -w ' [%{http_code}]\n' -X POST -H 'ITAD-API-Key: bogus0000' -H 'Content-Type: application/json' \
  -d '["018d937f-0000-7000-8000-000000000001"]' 'https://api.isthereanydeal.com/lookup/shop/61/id/v1'
```

Expected: both `{"status_code":403,"reason_phrase":"Invalid or expired api key"} [403]`.

- [ ] **Step 3: Start the test server and check that it asks nothing on its own**

Run: `task test-server` (port 8093, a copy of the config, `-no-unattended`); it prints `log: <dir>/server.log`. Then `grep -iE "isthereanydeal|igdb|home page" <dir>/server.log`.
Expected: the start-up warning lists "the home page's deals and releases" among the jobs that are off, and no request to ITAD or IGDB appears.

- [ ] **Step 4: Walk the page in the browser pane** (`http://127.0.0.1:8093/?v=3`)

Desktop:
- The app opens on Home; Home is first in the sidebar. `#/`, `#/nowhere` and the brand link open it.
- Pending chips (if the copy has any) open the right library view and the address becomes `#/library`; with nothing pending, "All caught up".
- Deals: "not configured" card (no key on the copy) linking to Providers. Releases: "not configured" unless IGDB releases was turned on in the copy.
- Your collection: figures open their views; recent covers open sheets; the share bar and legend read sensibly; the value figure is present only when the copy has estimates.
- Providers: "Deals" and "Upcoming releases" chains after "Second-hand prices", both providers off; ITAD's **Test** with `bogus0000` says the key was rejected; nothing saved.
- Switch the UI to Spanish: every new string is translated.

Then `resize_window` preset `mobile`, reload: four tabs with Home first; figures in two columns; carousels (if any data) swipe; no horizontal page scroll. Reset with preset `desktop`. Stop with `task test-server:stop`.

Use `get_page_text` and `find` for text, screenshots only for layout; never copy titles, counts or values from the copy into commits, docs or the report.

- [ ] **Step 5: Report to the user** (in Spanish, in chat; not in the repository)

Say plainly:
- Verified for real: the ITAD bogus-key answers (403, mapped to "the API key was rejected"), ITAD's asset host, the page and its states on a copy of the config, desktop and phone, en and es; that `-no-unattended` sends nothing.
- Only against fakes: ITAD's deals and lookup answers (shapes from its documentation), IGDB's release queries (no real Twitch application used), the job's timing (deterministic tests).
- To check on their own server with their own key (their call; `task run` is theirs): enter the ITAD key on Providers → Deals, press Refresh on Home, and confirm that "Featured now" looks like ITAD's deals page and that "Historical lows" are real lows; turn on IGDB releases if they want series and anticipated releases, knowing IGDB then receives their games' Steam ids.
- The open questions listed at the end of this plan.

---

## Open questions for the user

- **ITAD's lists, to confirm with a real key.** The documentation names only `-cut` and `price` as sort values, so "Featured now" uses ITAD's own order (no `sort`) and "Historical lows" filters the deepest cuts by `flag == "H"` or `historyLow`. After the first real refresh, does "Featured now" look like ITAD's deals page, and are the lows real lows? If ITAD has a better documented list for either (a sort such as `-trending`, or a `filter`), switching is a one-line change in `itad/deals.go`.
- **Series for games without a Steam link** (discs, GOG, Epic): IGDB's GOG and Epic id formats are still unverified (`igdb.md`), so only Steam apps find series. Worth adding once one real GOG or Epic match is seen, or an IGDB id could be cached with the game sheet later.
- **"Expires soon" uses the library's 30 days**, not the spec's 7, so the chip and the list it opens agree; the chip names the nearest deadline. Say if 7 days matters more than the match (it would need a separate "within a week" library filter).
- **IGDB releases is off until turned on**, even with the Twitch application entered, because it runs on a schedule and sends the owned games' Steam ids to IGDB. Confirm that, or say it should follow IGDB's covers like the details provider does.
- **Country from the currency:** the euro asks for Germany's prices. Users elsewhere in the euro zone can set the country in ITAD's settings; say if another default fits better.
