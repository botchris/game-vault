# Second-hand price estimates for physical copies

Status: approved design, waiting for the implementation plan. Branch `feature/valuation`.
Part 3 of the physical-copy work (part 1: grade, contents and price; part 2: photos).

## Why

Collectors want to know what their physical games are worth. Two public sources give a useful
reference for a European collection: CeX, a second-hand shop that publishes what it sells each
product for and what it pays for it, and eBay, where the active listings show what individuals ask.
Game Vault queries them for each physical copy with a barcode, keeps the latest estimate of each
source and adds them up into the value of the collection.

## Decisions taken with the user

- **Sources:** CeX and eBay. CeX's prices may now be stored, **only for the user's own copies**:
  one query per copy, never crawling the catalog, with source and date shown and no images
  (the user's decision of 2026-10-09; the CeX memory is updated). PriceCharting (official, paid,
  real sale prices with loose / complete / new, PAL sections, dollars) is left for a later plugin.
- **Which copies:** physical copies with a barcode only. CeX's title search sits behind a Cloudflare
  challenge (403, checked 2026-10-09 in ES and UK), and the safety rules forbid working around it, so
  copies without a barcode show a shortcut to add it instead.
- **When:** each copy has its own random update date, 20–40 days after its last estimate; never a
  batch run. A manual "Update price" button per copy.
- **What is kept:** the latest estimate of each source per copy (no history), and the collection's
  value per source in the default currency.

## Scope

In: domain estimates on copies, a `valuation` application module with a provider port, CeX and eBay
price providers, the scheduler, the API, the copy card line, a "Collection value" card in System,
docs.

Out: price history and charts, title search, currency conversion, PriceCharting, prices of keys or
library copies.

## Domain (`internal/domain/game`)

```go
// Estimate is a source's latest second-hand price for a copy.
type Estimate struct {
	Provider  string    // "cex", "ebay"
	Sell      Money     // what the shop sells it for (CeX) or the median asking price (eBay)
	BuyCash   Money     // CeX only: what it pays in cash
	BuyCredit Money     // CeX only: what it pays in store credit
	Listings  int       // eBay only: listings the median was taken from
	URL       string    // the product's page at the source
	FetchedAt time.Time
}
```

- All amounts of one estimate are in one currency (the source's market: EUR for CeX Spain, GBP for
  the UK, the marketplace's for eBay); `Money` is the part-1 type (minor units).
- `Copy` gains `Estimates []Estimate` (one per provider, sorted by provider) and
  `NextValuation time.Time` (zero: none planned).
- `Game` methods:
  - `SetEstimates(copyID, estimates []Estimate, next time.Time, now)`: replaces the estimates of the
    providers given, keeps the others, sets the next date.
  - `PlanValuation(copyID, next time.Time)`: sets only the next date.
- Rules, enforced in `normalize` / `UpdateCopy`: only physical copies with a barcode have estimates
  and a next date. Changing or clearing the barcode, or changing the kind, clears both (they were
  for another product). Scans (`applyImport`) and the CSV never touch them. Moving or merging a copy
  keeps them.

## Storage

`copyDoc` gains `estimates` (array of `{provider, currency, sell, buyCash, buyCredit, listings, url,
fetchedAt}`; amounts in minor units, zero ones omitted) and `nextValuation`. The game document
stays at version 2 (new fields only); no migration.

## Providers

### Port (`internal/application/valuation`)

```go
// Provider estimates the second-hand price of a product by its barcode.
type Provider interface {
	Descriptor() provider.Descriptor // Kind: provider.KindValuation
	Test(ctx context.Context, settings schema.Settings) error
	Estimate(ctx context.Context, settings schema.Settings, barcode game.Barcode) (game.Estimate, error)
}

// ErrNotListed means the source does not know the product: a normal outcome, not a failure.
var ErrNotListed = errors.New("not listed")
```

- `provider.KindValuation = "valuation"`. `plugin.Plugin` gains `Valuations []valuation.Provider`;
  `plugintest` checks them like the other kinds. Valuation providers are configured on the
  Providers page ("Prices"): enabled, order (display order only; every enabled provider is asked)
  and settings. `Test` makes one cheap real request.

### CeX prices (`internal/adapters/outbound/cex`, id `cex-prices`)

- `GET https://wss2.cex.{country}.webuy.io/v3/boxes/{EAN}/detail` (the endpoint the barcode provider
  already uses). Setting `countries` (default `es`; any of `es`, `uk`, `ie`, `pt`…): asked in
  order, the first that has the EAN wins. Only `superCatId == 1` (games) counts; anything else is
  `ErrNotListed`.
- Real answer (2026-10-09, Dead Space 3, ES): `sellPrice` 20, `cashPrice` 6, `exchangePrice` 10,
  in the country's currency (whole units in ES/UK; converted to minor units with
  `CurrencyDigits`). Unknown EAN: HTTP 200 with `"data": null`.
- A Cloudflare challenge (403 HTML) fails with "CeX is asking for a browser check; prices from CeX
  are unavailable for now" and is never worked around.
- `URL`: the product page on the country's site (expected `https://{country}.webuy.com/product-detail?id={EAN}`;
  the exact form is checked against the real site during implementation). Currency per country: EUR
  for `es`, `ie`, `pt`; GBP for `uk`.

### eBay prices (`internal/adapters/outbound/ebay`, id `ebay-prices`)

- Browse API `GET /buy/browse/v1/item_summary/search?gtin={EAN}&filter=conditions:{USED}&limit=50`
  with the `X-EBAY-C-MARKETPLACE-ID` header. Settings: `client_id`, `client_secret` (the same free
  developer keys as the eBay barcode provider, entered again: plugin pieces never share state) and
  `marketplace` (default `EBAY_ES`).
- The application token (client credentials) is obtained and cached like the barcode provider's.
- Estimate: the median of the listings' item prices (shipping excluded) in the marketplace's
  currency, and how many listings there were; listings in another currency are ignored. No
  listings: `ErrNotListed`. `URL`: an eBay search for the EAN on that marketplace.
- Bad keys fail with "eBay rejected the keys: check the client ID and secret".

## Service and scheduler (`internal/application/valuation`)

- `EstimateCopy(ctx, gameID, copyID)`: asks every enabled provider for the copy's barcode, saves the
  estimates it got (a provider that failed keeps its previous estimate; `ErrNotListed` removes that
  provider's estimate), and plans the next date at now + uniform(20, 40) days. Refuses copies that
  are not physical or have no barcode. Returns the updated game.
- `CollectionValue(ctx)`: for each provider, the sum over copies of `Sell` (and, for CeX, of
  `BuyCash` and `BuyCredit`) for estimates in the default currency, how many copies that covers, and
  how many estimates were left out for being in another currency.
- `RunScheduler(ctx)`: started by `main` unless `-no-unattended`.
  - Every 3–7 minutes (random), it loads the catalog:
    - physical copies with a barcode and no next date get one at now + uniform(0, 30) days (so a
      first run, or a new barcode, spreads over the month instead of firing at once);
    - copies whose date has passed are estimated one by one with `EstimateCopy`, waiting a random
      20–60 seconds between them, until none is due or the context ends.
  - A failing provider never retries in a loop: the copy's date always moves forward.

## API (`proto/gamevault/v1`)

- `Copy` gains `repeated Estimate estimates` and `google.protobuf.Timestamp next_valuation`;
  `message Estimate { string provider; Money sell; Money buy_cash; Money buy_credit; int32 listings;
  string url; google.protobuf.Timestamp fetched_at; }`.
- New `ValuationService`: `EstimateCopy(game_id, copy_id) → Game` and `GetCollectionValue() →
  {currency, repeated ProviderTotal{provider, name, sell, buy_cash, buy_credit, copies,
  other_currency}}`.
- Provider kind `PROVIDER_KIND_VALUATION` on the existing provider service.

## UI

- **Copy card (physical, with barcode):** one line per source with an estimate, e.g. "CeX: sells
  20 €, pays 6 € cash / 10 € credit" and "eBay: 14 € (9 listings)", each linking to its page, then
  the date ("3 Oct") and a small "Update price" button. Without estimates: "Price pending (planned
  for 17 Oct)" and the button. Errors from the button show inline.
- **Physical copy without a barcode:** "Add the barcode to estimate its price", opening the copy's
  form.
- **System → Collection value:** per source, "CeX sells your collection for 1.240 € and would pay
  610 € in cash (850 € in credit) · 52 copies" and "On eBay it is listed at 1.380 € · 47 copies";
  "N estimates in other currencies are not included". Shown only when a valuation provider is
  enabled.
- **Providers page:** a "Prices" section with the two providers.
- Every text in `en.json` and `es.json`.

## Testing

- Domain: estimates replaced per provider, cleared on barcode or kind change, kept on scans and
  moves; next date rules.
- Documents: round trip with estimates and the next date.
- CeX prices: a fake server with the real answer, unknown EAN, a non-game product, the Cloudflare 403
  page, countries in order.
- eBay prices: a fake server with listings (median, other currencies ignored), no listings, a
  rejected token, the token cached.
- Service: one provider failing keeps its previous estimate, `ErrNotListed` removes it, the next
  date within 20–40 days; refusal for copies without a barcode; totals per currency.
- Scheduler: plans undated copies within 30 days, estimates only due copies, one at a time with the
  pause (injected clock and sleeper).
- Connect end-to-end: `EstimateCopy` and `GetCollectionValue` with fake providers.
- Real probes: one CeX query for the Dead Space 3 EAN; eBay with bogus keys gives the clean error.

## Delivery

Branch `feature/valuation`, one PR to `main`, CI green before merging. `docs/technical.md`
(sources, schedule, what is stored), the README (estimates second-hand value), `.claude/memory/cex.md`
(prices of own copies allowed by the user's decision) and the data-model memory updated. No version
is tagged until the user asks.
