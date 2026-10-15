# Home: a dashboard with fresh deals, releases and the state of the collection

Status: design for review. Branch `feature/home-dashboard`.

## Why

Game Vault opens on the library: a complete but static list. There is no place that says what is
new today, what needs attention, or what is worth buying or waiting for. A home page answers that
at a glance, with information that changes from one day to the next: current deals, upcoming
releases, and the state of the collection.

## Decisions

- **A new "Home" page** is the first menu entry and the page that opens on start; the library
  stays right below it. On phones it is the first tab.
- **Four blocks**, top to bottom: pending things, deals, upcoming releases, the collection at a
  glance.
- **Deals come from IsThereAnyDeal (ITAD)** with the user's own free API key, no OAuth: featured
  deals and historical lows, without the games the user already owns. No price alerts of Game
  Vault's own (ITAD's terms forbid competing with it; alerts stay ITAD's).
- **Releases come from IGDB**, already a provider: upcoming entries of franchises and collections
  the user owns, and the most anticipated releases of the next weeks. No platform filter.
- **Snapshot in the background:** the external blocks are gathered by an unattended, jittered job
  and stored, so the page loads at once, external services get few requests and a failure shows
  the last good data with its age.

## Architecture

### Two new provider kinds

They join `provider.Kinds()` (cover, metadata, barcode, valuation) and get everything the registry
gives every kind for free: configuration on the Providers page (enable, order, settings, test),
status and Health issues, the shared HTTP base and its rate limiter.

- **`deals`** — port `home.DealsProvider` (`internal/application/home`):
  - `Deals(ctx, settings, DealQuery) ([]game.Deal, error)`; `DealQuery` asks for one list
    (`Featured` or `HistoricalLows`), a country and a size.
  - `Test(ctx, settings) error`, like every provider.
  - The port knows nothing about ITAD.
- **`releases`** — port `home.ReleasesProvider`:
  - `Anticipated(ctx, settings, window) ([]game.Release, error)`;
  - `Sequels(ctx, settings, owned []home.OwnedRef) ([]game.Release, error)`, where `OwnedRef`
    carries a game's id, title and the provider's own id when known (from the details cache). The
    provider resolves franchises and collections and returns upcoming entries, each with the owned
    game that explains it.
  - `Test`.

Adding the kinds is a table entry (`provider.Kinds()`), a registry section on the Providers page
and their translations.

### Plugins

- **ITAD** — new plugin `internal/adapters/outbound/itad`:
  - its own API key (`schema.FieldSecret`) and a country (default derived from the preferred
    currency);
  - implements `deals`; requests go through `apiclient` with the `ratelimit` throttle (ITAD allows
    1000 requests per 5 minutes, asks for caching and answers 429 with `Retry-After`);
  - featured deals and historical lows each come from ITAD's deals list (sorted or filtered as
    their API provides; to be confirmed against the real API with a bogus key first), about 40
    each;
  - the games of those deals are translated to Steam app ids in one batch lookup, so ownership is
    matched locally;
  - deal data is passed through unchanged: prices, shops, links (affiliate tags included).
- **IGDB** — the existing plugin gains `releases`:
  - `Anticipated`: one query for games with a first release date in the next 60 days, ordered by
    hype or anticipation as IGDB exposes it;
  - `Sequels`: the IGDB ids of owned games already matched by the details chain, in batches of
    500, for their franchises and collections; then one query for upcoming games of those.

### The `home` use case

`internal/application/home` owns the home page:

- **Refresh job** (unattended): asks the deal providers, removes deals for games owned on PC and
  notes games owned only on consoles; asks the release providers. It stores one snapshot per
  block.
- **Reading**: `GetHome` returns all blocks in one call. The external ones come from the snapshot;
  "pending" and "collection" are computed when asked from existing services (catalog, Health, key
  deadlines, valuations).
- **Per-block state**: fresh, stale with an error, not configured, or never fetched yet.

### Storage

Migration `0012_home_snapshot.sql`: one row per block (`block`, `doc` JSON, `fetched_at`,
`error`). The last good data is kept when a refresh fails; the error and its time are recorded
next to it.

### API

`HomeService` (proto `gamevault/v1/home.proto`):

- `GetHome` → pending items, deals (featured, historical lows), releases (from your franchises,
  anticipated), collection summary; each external block with its state, `fetched_at` and
  translated-ready error kind.
- `RefreshHome(block)` → user-started refresh of one external block, refused within 15 minutes of
  the last one.

## Blocks

### 1. Pending (shown only when there is something)

A strip of chips, each a link:

- keys that expire within 7 days, with the nearest one → the library filtered to them;
- unredeemed keys → the "pending keys" filter;
- keys the user does not need (redundant) → that filter;
- Health problems (a source signed out, a failed backup, a used-up quota) → System.

With nothing pending: one quiet line, "All caught up".

### 2. Deals (ITAD)

- Two carousels, "Featured now" and "Historical lows", about 12 cards each.
- A card: ITAD's image of the game, title, shop, price with the cut (−75 %) and the regular price
  struck through, a "Historical low" badge when it is one, and "ends in N days" when known.
  Clicking opens the deal in a new tab through ITAD's link as given.
- Games owned on PC are left out (matched by Steam app id when ITAD gives one, otherwise by exact
  title, `game.SameTitle`). A game owned only on a console stays, with a note ("You have it on
  PS4").
- A small "Prices from IsThereAnyDeal" credit under the block.

### 3. Upcoming releases (IGDB)

- "From your series": upcoming entries of franchises and collections the user owns: cover, title,
  date, platforms and the reason ("Because you have X", which opens X's sheet).
- "Most anticipated": releases of the next 60 days by anticipation, same card.
- Clicking a card opens its IGDB page.

### 4. Your collection

- Four figures, each a link to its view: games and copies; added this month; in the backlog (play
  status "Backlog"); value of physical copies (when there are estimates).
- "Recently added": the last 8 covers, each opening its sheet.
- Share by system (PC, PS5, Switch…) as one stacked horizontal bar.

### States of the external blocks

- **Not configured:** a card the size of the block saying what it brings and linking to Providers
  ("Add a free IsThereAnyDeal key to see deals").
- **Failed:** the last good data with its age and the reason ("Updated 6 h ago · IsThereAnyDeal is
  not answering").
- **First load:** a quiet skeleton.

### Look

The existing design system (tokens, surfaces, type). Desktop: one wide column; pending and the
collection in cards; carousels scroll sideways with snap and arrows on hover. Phone: one column,
swipeable carousels. Motion: only a soft fade-in on load (`.claude/docs/ui.md`). Every string in
en and es.

## Refresh, limits and errors

- **Deals every 6 h, releases every 24 h**, each ±10 % (`internal/application/unattended`).
- **On start**, a block whose snapshot is older than its interval is refreshed 1–5 minutes later,
  at a random moment.
- **`-no-unattended`** turns the job off: the page shows what is stored (a test server on a copy
  never calls the services on its own).
- **"Refresh" button** per external block, at most every 15 minutes.
- **ITAD** per refresh: about 3–4 requests. **IGDB** per day: about 4–5 queries, through its
  existing limiter.
- **Errors** are classified like every provider's (rate limited, quota used up, key rejected,
  unavailable) and reach Health through the registry. A failed block keeps its last good data.

## Privacy and terms

- ITAD receives no collection data: only public deal requests with the country, and lookups of its
  own game ids. Ownership is matched on the user's server.
- IGDB receives the IGDB ids of the user's games to find their franchises (it already receives
  their titles for sheets). Documented in `docs/technical.md`.
- ITAD's terms: the user's own key, data and links unchanged, caching, no competing alerts, a
  credit line.

## Testing

- **ITAD:** against a fake server with the real response shapes (taken from a bogus-key probe and
  the documentation) and its failures: 401 bad key, 429 with `Retry-After`, 5xx, no answer, an HTML
  page. A real-endpoint probe with a bogus key before calling it ready.
- **IGDB `releases`:** fake server for both queries and the batching.
- **`home`:** refresh with a controlled clock and random source; the owned-deal filter (Steam id,
  exact title, console-only note); a failed refresh keeping the last good data; the 15-minute
  refresh guard; `-no-unattended`.
- **SQLite:** the snapshot table round trip.
- **Web:** block-state and formatting helpers as pure functions with Node tests; the page checked
  on a test server, desktop and phone.

## Out of scope

- Connecting the user's ITAD account (OAuth): syncing the collection to ITAD, the ITAD waitlist,
  ITAD's waitlist webhook. A later step.
- Price alerts of Game Vault's own.
- Notifications (ntfy, webhook, email): a separate feature the pending block will link to later.

## Delivery

Branch `feature/home-dashboard`, one PR. Docs: `docs/technical.md` (home, the two provider kinds,
ITAD), `docs/plugins.md` (the new kinds), README (one line: the home page and ITAD deals), the
memory (`provider-chains.md`, a new `itad.md`).
