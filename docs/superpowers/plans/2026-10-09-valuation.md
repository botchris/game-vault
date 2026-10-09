# Second-hand price estimates — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Estimate the second-hand price of every physical copy with a barcode from CeX and eBay, refresh each copy on its own random date, show the estimates on the copy and add them up into the collection's value per source.

**Architecture:** The domain gains `game.Estimate`, `Copy.Estimates` and `Copy.NextValuation`, changed only through `Game`. A new plugin capability, `provider.KindValuation`, is configured by the media service like the other kinds (Providers page, settings, settings groups) and run by a new `internal/application/valuation` service, which asks every enabled provider for a copy's barcode, keeps the latest estimate per provider, plans the next date and runs a jittered scheduler. CeX and eBay each add a price provider to their plugin; eBay's shares the keys of its barcode provider through a settings group.

**Tech Stack:** Go 1.26, SQLite JSON documents, ConnectRPC/buf, React 19 + TypeScript, i18next, testify.

**Spec:** `docs/superpowers/specs/2026-10-09-valuation-design.md`

## Global Constraints

- Everything runs in the toolchain container through Task (`task lint`, `task test`, `task generate`, `task go -- test ./pkg/ -run X -v`); never Go or Node on the host.
- Load the `write-go` skill before writing Go. `task lint` enforces one struct field per line (`tools/fieldlines`), blank lines between documented members and docs on interface methods (`tools/docspacing`), golangci-lint.
- Go tests: GIVEN / WHEN / THEN subtests with testify; `context.WithTimeout(t.Context(), 10*time.Second)`; files under `t.TempDir()`; external services only through `httptest` fakes reproducing the real answers.
- No SQL migration: the game document stays at version 2 (fields are only added).
- Only physical copies with a barcode have estimates. One query per copy, never crawling a catalog; CeX images are never stored or shown; a Cloudflare challenge is never worked around.
- Next valuation: now + uniform(20, 40) days after an estimate; undated copies get now + uniform(0, 30) days; scheduler ticks every uniform(3, 7) minutes; uniform(20, 60) seconds between two estimates; skipped with `-no-unattended`.
- Repository text is English; every UI string through `t()` with keys in `en.json` and `es.json` (`task i18n`).
- Never open `config/`; test servers use copies (`task test-server`), and the scheduler is off there (`-no-unattended`).
- Branch `feature/valuation`, one PR to `main`, no tags.

## Review Focus

- A barcode changed from the copy form or by a CSV import must drop the old product's estimates and date (tests in Task 1).
- With no valuation provider enabled the scheduler must not plan dates nor query anything (test in Task 4).
- A provider that fails every time must never be retried in a loop: the copy's date moves forward even when every provider failed (test in Task 4).
- CeX prices with decimals (UK £4.50) and countries in different currencies must keep their real value in minor units (test in Task 5).
- Disabling a provider must drop its estimates on the next valuation and leave them out of the totals (test in Task 4).

---

## File structure

| File | Responsibility |
| --- | --- |
| `internal/domain/game/estimate.go`, `estimate_test.go` (create) | `Estimate`, `Copy.Valuable`, `SetEstimates`, `PlanValuation` |
| `internal/domain/game/copy.go`, `game.go`, `photo.go` (modify) | Copy fields, clearing rules, deep copies |
| `internal/domain/provider/provider.go` (modify) | `KindValuation` |
| `internal/adapters/outbound/sqlite/docs.go`, `docs_test.go` (modify) | `estimates`, `nextValuation` in the copy document |
| `internal/application/media/service.go` (modify) | `Providers.Valuations` configured like the other kinds |
| `internal/application/plugin/plugin.go`, `plugintest/plugintest.go` (modify) | `Plugin.Valuations`, registry, checks |
| `internal/application/valuation/service.go`, `scheduler.go`, `export_test.go`, `service_test.go` (create) | Port, `EstimateCopy`, `CollectionValue`, scheduler |
| `internal/adapters/outbound/cex/prices.go`, `prices_test.go`, `plugin.go`, `provider.go` (create/modify) | CeX price provider |
| `internal/adapters/outbound/ebay/prices.go`, `prices_test.go`, `plugin.go`, `provider.go` (create/modify) | eBay price provider, shared settings group |
| `proto/gamevault/v1/game.proto`, `valuation.proto` (modify/create) + generated | API |
| `internal/adapters/inbound/rpc/valuation_handler.go` (create), `mapper.go`, `server.go`, `server_test.go`, `valuation_test.go` (modify/create) | RPC |
| `cmd/gamevault/main.go` (modify) | Wiring, scheduler |
| `web/src/api/client.ts`, `web/src/lib/usePriceProviders.ts` (create), `web/src/features/library/CopyValue.tsx` (create), `GameDetail.tsx`, `web/src/features/providers/ProvidersPage.tsx`, `web/src/features/system/SystemPage.tsx`, `web/src/styles.css`, `web/src/i18n/locales/{en,es}.json` (modify) | UI |
| `docs/technical.md`, `README.md`, `.claude/memory/cex.md`, `.claude/memory/data-model.md`, spec (modify) | Docs |

---

### Task 1: Domain — estimates on copies

**Files:**
- Create: `internal/domain/game/estimate.go`, `internal/domain/game/estimate_test.go`
- Modify: `internal/domain/game/copy.go`, `internal/domain/game/game.go`, `internal/domain/game/photo.go` (`clone`), `internal/domain/provider/provider.go`

**Interfaces:**
- Produces:
  - `type Estimate struct { Provider string; Sell Money; BuyCash Money; BuyCredit Money; Listings int; URL string; FetchedAt time.Time }`
  - `Copy.Estimates []Estimate` (sorted by provider), `Copy.NextValuation time.Time`
  - `func (c Copy) Valuable() bool` (physical with a barcode)
  - `func (g *Game) SetEstimates(copyID ID, estimates []Estimate, next, now time.Time) (Copy, error)` — replaces all estimates, sets the date, touches `UpdatedAt`
  - `func (g *Game) PlanValuation(copyID ID, next time.Time) error` — sets only the date, does not touch `UpdatedAt` (no cover cache busting)
  - `provider.KindValuation Kind = "valuation"`

- [ ] **Step 1: Write the failing tests** — `internal/domain/game/estimate_test.go`:

```go
package game

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func eur(minor int64) Money { return Money{Amount: minor, Currency: "EUR"} }

func valuableGame(t *testing.T) (*Game, ID) {
	t.Helper()

	g, err := New("Dead Space 3", t0)
	require.NoError(t, err)

	c, err := g.AddCopy(CopyDetails{
		Kind:     KindPhysical,
		Platform: "Xbox 360",
		Barcode:  "5030934110075",
	}, t0)
	require.NoError(t, err)

	return g, c.ID
}

func TestEstimates(t *testing.T) {
	next := t0.Add(30 * 24 * time.Hour)

	t.Run("GIVEN a physical copy with a barcode", func(t *testing.T) {
		g, id := valuableGame(t)

		t.Run("WHEN estimates of two sources are set", func(t *testing.T) {
			c, err := g.SetEstimates(id, []Estimate{
				{Provider: "ebay-prices", Sell: eur(1400), Listings: 9, FetchedAt: t0},
				{Provider: "cex-prices", Sell: eur(2000), BuyCash: eur(600), BuyCredit: eur(1000), FetchedAt: t0},
			}, next, t0)
			require.NoError(t, err)

			t.Run("THEN they are kept sorted by source, with the next date", func(t *testing.T) {
				require.Len(t, c.Estimates, 2)
				assert.Equal(t, "cex-prices", c.Estimates[0].Provider)
				assert.Equal(t, next, c.NextValuation)
			})

			t.Run("AND changing other details keeps them", func(t *testing.T) {
				_, err := g.UpdateCopy(id, CopyDetails{Kind: KindPhysical, Platform: "Xbox 360", Barcode: "5030934110075", Notes: "x"}, t0)
				require.NoError(t, err)
				assert.Len(t, g.Copies()[0].Estimates, 2)
			})

			t.Run("AND changing the barcode drops them and the date", func(t *testing.T) {
				_, err := g.UpdateCopy(id, CopyDetails{Kind: KindPhysical, Platform: "Xbox 360", Barcode: "5030930112256"}, t0)
				require.NoError(t, err)
				assert.Empty(t, g.Copies()[0].Estimates)
				assert.True(t, g.Copies()[0].NextValuation.IsZero())
			})
		})

		t.Run("WHEN an estimate mixes currencies, has no price, or a source appears twice", func(t *testing.T) {
			cases := [][]Estimate{
				{{Provider: "cex-prices", Sell: eur(2000), BuyCash: Money{Amount: 500, Currency: "GBP"}}},
				{{Provider: "cex-prices"}},
				{{Provider: "cex-prices", Sell: eur(1)}, {Provider: "cex-prices", Sell: eur(2)}},
				{{Sell: eur(1)}},
			}

			t.Run("THEN each is refused", func(t *testing.T) {
				for _, c := range cases {
					_, err := g.SetEstimates(id, c, next, t0)

					var ve *ValidationError
					assert.ErrorAs(t, err, &ve)
				}
			})
		})
	})

	t.Run("GIVEN copies that cannot be priced: a key, and a physical copy without a barcode", func(t *testing.T) {
		g, err := New("Halo 3", t0)
		require.NoError(t, err)

		key, err := g.AddCopy(CopyDetails{Kind: KindKey}, t0)
		require.NoError(t, err)

		disc, err := g.AddCopy(CopyDetails{Kind: KindPhysical}, t0)
		require.NoError(t, err)

		t.Run("THEN neither accepts estimates nor a date", func(t *testing.T) {
			for _, id := range []ID{key.ID, disc.ID} {
				_, err := g.SetEstimates(id, []Estimate{{Provider: "cex-prices", Sell: eur(1)}}, next, t0)

				var ve *ValidationError
				assert.ErrorAs(t, err, &ve)
				assert.ErrorAs(t, g.PlanValuation(id, next), &ve)
			}
		})
	})

	t.Run("GIVEN a priced copy", func(t *testing.T) {
		g, id := valuableGame(t)
		_, err := g.SetEstimates(id, []Estimate{{Provider: "cex-prices", Sell: eur(2000)}}, next, t0)
		require.NoError(t, err)

		t.Run("WHEN it becomes a key", func(t *testing.T) {
			_, err := g.UpdateCopy(id, CopyDetails{Kind: KindKey}, t0)
			require.NoError(t, err)

			t.Run("THEN its estimates go", func(t *testing.T) {
				assert.Empty(t, g.Copies()[0].Estimates)
			})
		})
	})

	t.Run("GIVEN a priced copy imported by a CSV row", func(t *testing.T) {
		g, id := valuableGame(t)
		_, err := g.SetEstimates(id, []Estimate{{Provider: "cex-prices", Sell: eur(2000)}}, next, t0)
		require.NoError(t, err)

		c := &g.copies[0]

		t.Run("WHEN a re-import keeps the barcode", func(t *testing.T) {
			c.applyImport(CopyDetails{Kind: KindPhysical, Notes: "shelf"})

			t.Run("THEN the estimates stay", func(t *testing.T) {
				assert.Len(t, c.Estimates, 1)
			})
		})

		t.Run("WHEN a re-import brings another barcode", func(t *testing.T) {
			c.applyImport(CopyDetails{Kind: KindPhysical, Barcode: "5030930112256"})

			t.Run("THEN the estimates and the date go", func(t *testing.T) {
				assert.Empty(t, c.Estimates)
				assert.True(t, c.NextValuation.IsZero())
			})
		})
	})

	t.Run("GIVEN a priced copy", func(t *testing.T) {
		g, id := valuableGame(t)
		_, err := g.SetEstimates(id, []Estimate{{Provider: "cex-prices", Sell: eur(2000)}}, next, t0)
		require.NoError(t, err)

		t.Run("WHEN a caller changes the estimates it was given", func(t *testing.T) {
			g.Copies()[0].Estimates[0].Sell = eur(1)

			t.Run("THEN the game is not changed", func(t *testing.T) {
				assert.Equal(t, eur(2000), g.Copies()[0].Estimates[0].Sell)
			})
		})

		t.Run("WHEN only the next date is planned", func(t *testing.T) {
			before := g.UpdatedAt()
			require.NoError(t, g.PlanValuation(id, next.Add(time.Hour)))

			t.Run("THEN the date changes and the game's update time does not", func(t *testing.T) {
				assert.Equal(t, next.Add(time.Hour), g.Copies()[0].NextValuation)
				assert.Equal(t, before, g.UpdatedAt())
			})
		})
	})
}
```

Check the barcode values pass the existing checksum validation (`5030934110075` is CeX's Dead Space 3, real; `5030930112256` must be a valid EAN-13 — if `normalize` refuses it, compute a valid one with the check digit and use it).

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/domain/game/ -run TestEstimates`
Expected: FAIL (build: `Estimate`, `SetEstimates` undefined).

- [ ] **Step 3: Implement** — `internal/domain/game/estimate.go`:

```go
package game

import (
	"slices"
	"strings"
	"time"
)

// Estimate is a source's latest second-hand price for a copy. Its amounts share one currency, the
// source's market (EUR for CeX Spain, the marketplace's for eBay).
type Estimate struct {
	// Provider is the price provider's id ("cex-prices").
	Provider string

	// Sell is what the shop sells it for (CeX) or the median asking price (eBay).
	Sell Money

	// BuyCash and BuyCredit are what a shop pays for it, in cash or store credit; zero when the
	// source does not buy.
	BuyCash   Money
	BuyCredit Money

	// Listings is how many listings Sell was taken from; zero for a shop's price.
	Listings int

	// URL is the product's page at the source.
	URL       string
	FetchedAt time.Time
}

func (e Estimate) normalize() (Estimate, error) {
	e.Provider, e.URL = strings.TrimSpace(e.Provider), strings.TrimSpace(e.URL)
	if e.Provider == "" {
		return e, invalid("an estimate needs its provider")
	}

	for _, m := range []*Money{&e.Sell, &e.BuyCash, &e.BuyCredit} {
		n, err := m.normalize()
		if err != nil {
			return e, err
		}

		*m = n
	}

	if e.Sell.IsZero() {
		return e, invalid("an estimate needs a price")
	}

	for _, m := range []Money{e.BuyCash, e.BuyCredit} {
		if !m.IsZero() && m.Currency != e.Sell.Currency {
			return e, invalid("an estimate's amounts must share one currency")
		}
	}

	if e.Listings < 0 {
		return e, invalid("an estimate cannot have negative listings")
	}

	e.FetchedAt = e.FetchedAt.UTC()

	return e, nil
}

// Valuable reports whether the copy can have price estimates: a physical copy with a barcode,
// which is how the sources find the product.
func (c Copy) Valuable() bool { return c.Kind == KindPhysical && c.Barcode != "" }

// clearValuation drops the estimates and the next date (they were for another product).
func (c *Copy) clearValuation() {
	c.Estimates, c.NextValuation = nil, time.Time{}
}

// SetEstimates replaces a copy's price estimates (one per provider) and plans the next valuation.
func (g *Game) SetEstimates(copyID ID, estimates []Estimate, next, now time.Time) (Copy, error) {
	i, err := g.valuable(copyID)
	if err != nil {
		return Copy{}, err
	}

	out := make([]Estimate, 0, len(estimates))

	for _, e := range estimates {
		n, err := e.normalize()
		if err != nil {
			return Copy{}, err
		}

		if slices.ContainsFunc(out, func(o Estimate) bool { return o.Provider == n.Provider }) {
			return Copy{}, invalid("a copy has one estimate per provider")
		}

		out = append(out, n)
	}

	slices.SortFunc(out, func(a, b Estimate) int { return strings.Compare(a.Provider, b.Provider) })

	if len(out) == 0 {
		out = nil
	}

	g.copies[i].Estimates, g.copies[i].NextValuation = out, next.UTC()

	return g.touchCopy(i, now), nil
}

// PlanValuation sets when a copy's prices are next estimated. It does not count as a change of the
// game (its covers and caches stay valid).
func (g *Game) PlanValuation(copyID ID, next time.Time) error {
	i, err := g.valuable(copyID)
	if err != nil {
		return err
	}

	g.copies[i].NextValuation = next.UTC()

	return nil
}

func (g *Game) valuable(copyID ID) (int, error) {
	i := g.indexOf(copyID)
	if i < 0 {
		return 0, ErrCopyNotFound
	}

	if !g.copies[i].Valuable() {
		return 0, invalid("only physical copies with a barcode have price estimates: add the copy's barcode first")
	}

	return i, nil
}
```

In `copy.go`, add to `Copy` after `Photos` (one field per line, documented):

```go
	// Estimates are the latest second-hand prices, one per provider, sorted by provider. Only
	// physical copies with a barcode have them.
	Estimates []Estimate

	// NextValuation is when the prices are next estimated; zero when none is planned.
	NextValuation time.Time
```

At the end of `applyImport`, before `return`:

```go
	// Estimates belong to the product the barcode names.
	if c.Kind != KindPhysical || c.Barcode != before.Barcode {
		c.clearValuation()
	}
```

In `game.go` `UpdateCopy`, before assigning the new details:

```go
	if d.Kind != KindPhysical || d.Barcode != g.copies[i].Barcode {
		g.copies[i].clearValuation() // estimates belong to the product the barcode names
	}
```

In `photo.go`, `clone` also copies the estimates:

```go
func (c Copy) clone() Copy {
	c.Photos = slices.Clone(c.Photos)
	c.Estimates = slices.Clone(c.Estimates)

	return c
}
```

In `internal/domain/provider/provider.go`, the `Kind` values:

```go
	KindValuation Kind = "valuation" // second-hand price estimates of physical copies
```

- [ ] **Step 4: Run the domain tests**

Run: `task go -- test ./internal/domain/...`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

Run: `task lint` (0 issues).

```bash
git add internal/domain
git commit -m "Domain: second-hand price estimates on physical copies"
```

---

### Task 2: Documents — store estimates and the next date

**Files:**
- Modify: `internal/adapters/outbound/sqlite/docs.go`, `internal/adapters/outbound/sqlite/docs_test.go`

**Interfaces:**
- Consumes: `game.Estimate`, `Copy.Estimates`, `Copy.NextValuation` (Task 1).
- Produces: copy documents with `estimates` (`[{provider, currency, sell, buyCash, buyCredit, listings, url, fetchedAt}]`, amounts in minor units, zero ones omitted) and `nextValuation`; still version 2.

- [ ] **Step 1: Write the failing test** — append to `docs_test.go`:

```go
func TestDocuments_estimates(t *testing.T) {
	t.Run("GIVEN a physical copy with two estimates and a next date", func(t *testing.T) {
		next := docTime.Add(30 * 24 * time.Hour)
		g := game.Rehydrate("g1", game.Info{Title: "Dead Space 3"}, []game.Copy{{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind:    game.KindPhysical,
				Status:  game.StatusOwned,
				Barcode: "5030934110075",
			},
			Estimates: []game.Estimate{
				{
					Provider:  "cex-prices",
					Sell:      game.Money{Amount: 2000, Currency: "EUR"},
					BuyCash:   game.Money{Amount: 600, Currency: "EUR"},
					BuyCredit: game.Money{Amount: 1000, Currency: "EUR"},
					URL:       "https://es.webuy.com/product-detail/?id=5030934110075",
					FetchedAt: docTime,
				},
				{
					Provider:  "ebay-prices",
					Sell:      game.Money{Amount: 1400, Currency: "EUR"},
					Listings:  9,
					FetchedAt: docTime,
				},
			},
			NextValuation: next,
			CreatedAt:     docTime,
			UpdatedAt:     docTime,
		}}, docTime, docTime)

		t.Run("WHEN it is encoded and decoded", func(t *testing.T) {
			raw, err := encodeGame(g)
			require.NoError(t, err)

			got, err := decodeGame(g.ID(), raw)
			require.NoError(t, err)

			t.Run("THEN the estimates and the date come back, as version 2", func(t *testing.T) {
				assert.Equal(t, g.Copies(), got.Copies())
				assert.Contains(t, raw, `"v":2`)
				assert.Contains(t, raw, `"currency":"EUR"`)
			})

			t.Run("AND zero amounts and empty fields are left out", func(t *testing.T) {
				assert.Equal(t, 1, strings.Count(raw, `"buyCash"`))
				assert.Equal(t, 1, strings.Count(raw, `"listings"`))

				plain, err := encodeGame(sampleGame(t))
				require.NoError(t, err)
				assert.NotContains(t, plain, `"estimates"`)
				assert.NotContains(t, plain, `"nextValuation"`)
			})
		})
	})
}
```

- [ ] **Step 2: Run it to see it fail**

Run: `task go -- test ./internal/adapters/outbound/sqlite/ -run TestDocuments_estimates`
Expected: FAIL (estimates lost).

- [ ] **Step 3: Implement** in `docs.go`. `copyDoc` gains, after `Photos`:

```go
	Estimates     []estimateDoc `json:"estimates,omitempty"`
	NextValuation string        `json:"nextValuation,omitempty"`
```

New type after `photoDoc`:

```go
// estimateDoc is the stored form of a game.Estimate: one currency, amounts in its minor units.
type estimateDoc struct {
	Provider  string `json:"provider"`
	Currency  string `json:"currency"`
	Sell      int64  `json:"sell"`
	BuyCash   int64  `json:"buyCash,omitempty"`
	BuyCredit int64  `json:"buyCredit,omitempty"`
	Listings  int    `json:"listings,omitempty"`
	URL       string `json:"url,omitempty"`
	FetchedAt string `json:"fetchedAt"`
}
```

In `encodeGame`'s `copyDoc`: `Estimates: estimateDocs(c.Estimates),` and `NextValuation: optionalTime(c.NextValuation),`. In `decodeGame`'s `game.Copy`: `Estimates: estimatesOf(c.Estimates),` and `NextValuation: parseTime(c.NextValuation),`. Helpers at the end:

```go
func optionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return formatTime(t)
}

func estimateDocs(estimates []game.Estimate) []estimateDoc {
	if len(estimates) == 0 {
		return nil
	}

	out := make([]estimateDoc, 0, len(estimates))
	for _, e := range estimates {
		out = append(out, estimateDoc{
			Provider:  e.Provider,
			Currency:  e.Sell.Currency,
			Sell:      e.Sell.Amount,
			BuyCash:   e.BuyCash.Amount,
			BuyCredit: e.BuyCredit.Amount,
			Listings:  e.Listings,
			URL:       e.URL,
			FetchedAt: formatTime(e.FetchedAt),
		})
	}

	return out
}

func estimatesOf(docs []estimateDoc) []game.Estimate {
	if len(docs) == 0 {
		return nil
	}

	money := func(amount int64, currency string) game.Money {
		if amount == 0 {
			return game.Money{}
		}

		return game.Money{
			Amount:   amount,
			Currency: currency,
		}
	}

	out := make([]game.Estimate, 0, len(docs))
	for _, d := range docs {
		out = append(out, game.Estimate{
			Provider:  d.Provider,
			Sell:      money(d.Sell, d.Currency),
			BuyCash:   money(d.BuyCash, d.Currency),
			BuyCredit: money(d.BuyCredit, d.Currency),
			Listings:  d.Listings,
			URL:       d.URL,
			FetchedAt: parseTime(d.FetchedAt),
		})
	}

	return out
}
```

Use `optionalTime` for the photos' `TakenAt` too if `photoDocs` has its own zero check (keep one helper; replace the inline check).

- [ ] **Step 4: Run the sqlite tests**

Run: `task go -- test ./internal/adapters/outbound/sqlite/`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
git add internal/adapters/outbound/sqlite
git commit -m "Store price estimates and the next valuation date in copy documents"
```

---

### Task 3: Plugin plumbing — valuation providers are configured like the others

**Files:**
- Create: `internal/application/valuation/provider.go`
- Modify: `internal/application/media/service.go`, `internal/application/plugin/plugin.go`, `internal/application/plugin/plugin_test.go`, `internal/application/plugin/plugintest/plugintest.go`, `internal/application/plugin/plugintest/plugintest_test.go`

**Interfaces:**
- Produces:
  - `valuation.Provider` interface: `media.Provider` + `Estimate(ctx context.Context, settings schema.Settings, barcode game.Barcode) (game.Estimate, error)`; `var valuation.ErrNotListed`.
  - `media.Providers.Valuations []media.Provider` (configured by the media service: `Providers`, `ConfigureProvider`, `ReorderProviders`, `TestProvider`, settings groups).
  - `plugin.Plugin.Valuations []valuation.Provider`; `(*plugin.Registry).Valuations() []valuation.Provider`; `Registry.Media()` also fills `Valuations`.

- [ ] **Step 1: Write the failing tests** — append to `internal/application/plugin/plugin_test.go` (imports gain `gamevault/internal/application/valuation`):

```go
type fakePrices struct{ id provider.ID }

func (f fakePrices) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:   f.id,
		Kind: provider.KindValuation,
	}
}
func (fakePrices) Test(context.Context, schema.Settings) error { return nil }
func (fakePrices) Estimate(context.Context, schema.Settings, game.Barcode) (game.Estimate, error) {
	return game.Estimate{}, valuation.ErrNotListed
}

func TestRegistry_valuations(t *testing.T) {
	shop := plugin.Plugin{
		ID:         "shop",
		Valuations: []valuation.Provider{fakePrices{"shop-prices"}},
	}

	t.Run("GIVEN a plugin with a price provider", func(t *testing.T) {
		r, err := plugin.NewRegistry(shop)
		require.NoError(t, err)

		t.Run("THEN the valuation service gets it, and the media service configures it", func(t *testing.T) {
			require.Len(t, r.Valuations(), 1)
			require.Len(t, r.Media().Valuations, 1)
			assert.Equal(t, provider.ID("shop-prices"), r.Media().Valuations[0].Descriptor().ID)
		})
	})

	t.Run("GIVEN two plugins with the same price provider id", func(t *testing.T) {
		_, err := plugin.NewRegistry(shop, plugin.Plugin{
			ID:         "other",
			Valuations: []valuation.Provider{fakePrices{"shop-prices"}},
		})

		t.Run("THEN they are refused", func(t *testing.T) {
			assert.ErrorIs(t, err, plugin.ErrDuplicate)
		})
	})
}
```

Append to `internal/application/plugin/plugintest/plugintest_test.go` (imports gain `gamevault/internal/application/valuation`):

```go
// misfiledPrices is a price provider described as a barcode provider, without a default place.
type misfiledPrices struct{}

func (misfiledPrices) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:             "misfiled",
		Kind:           provider.KindBarcode,
		Name:           "Misfiled",
		DescriptionKey: "providers.misfiled.description",
	}
}
func (misfiledPrices) Test(context.Context, schema.Settings) error { return nil }
func (misfiledPrices) Estimate(context.Context, schema.Settings, game.Barcode) (game.Estimate, error) {
	return game.Estimate{}, valuation.ErrNotListed
}

func TestProblems_valuations(t *testing.T) {
	t.Run("GIVEN a plugin whose price provider is misfiled", func(t *testing.T) {
		tr := Translations{"en": {"providers.misfiled.description": true}}
		problems := Problems(plugin.Plugin{
			ID:         "misfiled",
			Name:       "Misfiled",
			Valuations: []valuation.Provider{misfiledPrices{}},
		}, tr)

		t.Run("THEN the wrong kind and the missing default place are reported", func(t *testing.T) {
			assert.Len(t, problems, 2)
			assert.Contains(t, problems[0]+problems[1], "valuation")
		})
	})
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/application/plugin/...`
Expected: FAIL (build: `valuation` package, `Plugin.Valuations` undefined).

- [ ] **Step 3: Implement** — `internal/application/valuation/provider.go`:

```go
// Package valuation estimates the second-hand price of physical copies: it asks every enabled price
// provider for a copy's barcode, keeps each provider's latest estimate on the copy, plans when to ask
// again and adds the estimates up into the collection's value.
package valuation

import (
	"context"
	"errors"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

// ErrNotListed means the source does not know the product: a normal outcome, not a failure.
var ErrNotListed = errors.New("not listed")

// Provider estimates the second-hand price of a product by its barcode. Its configuration (enabled,
// order, settings) is kept by the media service with the other providers (provider.KindValuation).
type Provider interface {
	media.Provider

	// Estimate returns the source's price for the product with that barcode, or ErrNotListed. The
	// service fills in Provider and FetchedAt.
	Estimate(ctx context.Context, settings schema.Settings, barcode game.Barcode) (game.Estimate, error)
}
```

In `media/service.go`, `Providers` gains (documented):

```go
	// Valuations are price providers: configured here like the other kinds, run by the valuation
	// service.
	Valuations []Provider
```

In `NewService`, register them like the metadata ones (`s.impls[id] = p`, `s.order = append(s.order, id)`), and add `provider.KindValuation` to `allKinds`.

In `plugin.go`: import `gamevault/internal/application/valuation`; `Plugin` gains

```go
	// Valuations estimate second-hand prices of physical copies.
	Valuations []valuation.Provider
```

`mediaDescriptors` also appends `v.Descriptor()` for each valuation (so duplicate ids are refused); `Media()` appends `for _, v := range p.Valuations { out.Valuations = append(out.Valuations, v) }`; new method:

```go
// Valuations returns every plugin's price providers, for the valuation service.
func (r *Registry) Valuations() []valuation.Provider {
	var out []valuation.Provider
	for _, p := range r.plugins {
		out = append(out, p.Valuations...)
	}

	return out
}
```

In `plugintest.go`: the "adds nothing" count includes `len(p.Valuations)`, and

```go
	for _, v := range p.Valuations {
		c.provider(p.ID, v, provider.KindValuation)
	}
```

(Update the package comment's list of what a plugin brings.)

- [ ] **Step 4: Run the tests**

Run: `task go -- test ./internal/application/... ./cmd/...`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
git add internal/application
git commit -m "Plugins can add price providers, configured like the other providers"
```

---

### Task 4: Valuation service and scheduler

**Files:**
- Create: `internal/application/valuation/service.go`, `internal/application/valuation/scheduler.go`, `internal/application/valuation/export_test.go`, `internal/application/valuation/service_test.go`

**Interfaces:**
- Consumes: `valuation.Provider`, `ErrNotListed` (Task 3); `media.ProviderView`; `game.SetEstimates`, `PlanValuation`, `Copy.Valuable` (Task 1); `settings.Repository.Preferences`.
- Produces:
  - `type Configs interface { Providers(ctx context.Context, kind provider.Kind) ([]media.ProviderView, error) }` (implemented by `*media.Service`)
  - `func NewService(games game.Repository, tx port.TxManager, configs Configs, prefs settings.Repository, now port.Clock, log *slog.Logger, providers ...Provider) *Service`
  - `func (s *Service) EstimateCopy(ctx context.Context, gameID, copyID game.ID) (*game.Game, []string, error)` — warnings are the providers that failed
  - `func (s *Service) CollectionValue(ctx context.Context) (CollectionValue, error)`; `type CollectionValue struct { Currency string; Totals []ProviderTotal }`; `type ProviderTotal struct { Provider provider.ID; Name string; Sell, BuyCash, BuyCredit int64; Copies int; OtherCurrency int }`
  - `func (s *Service) Tick(ctx context.Context) error`; `func (s *Service) RunScheduler(ctx context.Context)`
  - Errors: `ErrNotValuable`, `ErrNoProviders`, `ErrChanged`
  - Test hook (export_test.go): `func (s *Service) SetChance(random func() float64, sleep func(context.Context, time.Duration) error)`

- [ ] **Step 1: Write the failing tests** — `export_test.go`:

```go
package valuation

import (
	"context"
	"time"
)

// SetChance replaces the random source and the sleeper, for tests.
func (s *Service) SetChance(random func() float64, sleep func(context.Context, time.Duration) error) {
	s.random, s.sleep = random, sleep
}
```

`service_test.go`:

```go
package valuation_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/settings"
)

var now = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

// prices is a fake price provider: a fixed price per barcode, or a failure.
type prices struct {
	id    provider.ID
	sell  map[game.Barcode]game.Money
	fail  error
	calls []game.Barcode
}

func (p *prices) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               p.id,
		Kind:             provider.KindValuation,
		Name:             string(p.id),
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (p *prices) Test(context.Context, schema.Settings) error { return nil }

func (p *prices) Estimate(_ context.Context, _ schema.Settings, code game.Barcode) (game.Estimate, error) {
	p.calls = append(p.calls, code)
	if p.fail != nil {
		return game.Estimate{}, p.fail
	}

	m, ok := p.sell[code]
	if !ok {
		return game.Estimate{}, valuation.ErrNotListed
	}

	return game.Estimate{Sell: m}, nil
}

// configs enables the providers listed in on.
type configs struct {
	impls []valuation.Provider
	on    map[provider.ID]bool
}

func (c *configs) Providers(_ context.Context, kind provider.Kind) ([]media.ProviderView, error) {
	var out []media.ProviderView
	for i, p := range c.impls {
		d := p.Descriptor()
		out = append(out, media.ProviderView{
			Provider:   provider.Rehydrate(d.ID, kind, c.on[d.ID], i, nil, now),
			Descriptor: d,
		})
	}

	return out, nil
}

type world struct {
	svc    *valuation.Service
	games  game.Repository
	cex    *prices
	ebay   *prices
	cfg    *configs
	sleeps []time.Duration
}

const (
	deadSpace = game.Barcode("5030934110075")
	halo      = game.Barcode("0882224617016")
)

func newWorld(ctx context.Context, t *testing.T) *world {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	prefs := sqlite.NewSettingsRepository(db)
	require.NoError(t, prefs.SavePreferences(ctx, settings.Preferences{Currency: "EUR"}))

	w := &world{
		games: sqlite.NewGameRepository(db),
		cex:   &prices{id: "cex-prices", sell: map[game.Barcode]game.Money{deadSpace: {Amount: 2000, Currency: "EUR"}, halo: {Amount: 900, Currency: "GBP"}}},
		ebay:  &prices{id: "ebay-prices", sell: map[game.Barcode]game.Money{deadSpace: {Amount: 1400, Currency: "EUR"}}},
	}
	w.cfg = &configs{impls: []valuation.Provider{w.cex, w.ebay}, on: map[provider.ID]bool{"cex-prices": true, "ebay-prices": true}}
	w.svc = valuation.NewService(w.games, db, w.cfg, prefs, func() time.Time { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)), w.cex, w.ebay)
	w.svc.SetChance(func() float64 { return 0.5 }, func(_ context.Context, d time.Duration) error {
		w.sleeps = append(w.sleeps, d)
		return nil
	})

	return w
}

// physical adds a game with one physical copy and returns their ids.
func (w *world) physical(ctx context.Context, t *testing.T, title string, code game.Barcode) (game.ID, game.ID) {
	t.Helper()

	g, err := game.New(title, now)
	require.NoError(t, err)

	c, err := g.AddCopy(game.CopyDetails{Kind: game.KindPhysical, Barcode: code}, now)
	require.NoError(t, err)
	require.NoError(t, w.games.Save(ctx, g))

	return g.ID(), c.ID
}

func (w *world) copy(ctx context.Context, t *testing.T, id game.ID) game.Copy {
	t.Helper()

	g, err := w.games.Get(ctx, id)
	require.NoError(t, err)

	return g.Copies()[0]
}

func TestEstimateCopy(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a physical copy known to both sources", func(t *testing.T) {
		w := newWorld(ctx, t)
		gid, cid := w.physical(ctx, t, "Dead Space 3", deadSpace)

		t.Run("WHEN its price is estimated", func(t *testing.T) {
			_, warnings, err := w.svc.EstimateCopy(ctx, gid, cid)
			require.NoError(t, err)

			t.Run("THEN it has both estimates and its next date 30 days ahead (20–40 at random)", func(t *testing.T) {
				c := w.copy(ctx, t, gid)
				assert.Empty(t, warnings)
				require.Len(t, c.Estimates, 2)
				assert.Equal(t, "cex-prices", c.Estimates[0].Provider)
				assert.Equal(t, now, c.Estimates[0].FetchedAt)
				assert.Equal(t, now.Add(30*day), c.NextValuation)
			})
		})

		t.Run("WHEN eBay then fails and CeX no longer lists it", func(t *testing.T) {
			w.ebay.fail = errors.New("eBay is down")
			delete(w.cex.sell, deadSpace)

			_, warnings, err := w.svc.EstimateCopy(ctx, gid, cid)
			require.NoError(t, err)

			t.Run("THEN eBay's previous estimate stays, CeX's goes, and the failure is reported", func(t *testing.T) {
				c := w.copy(ctx, t, gid)
				require.Len(t, c.Estimates, 1)
				assert.Equal(t, "ebay-prices", c.Estimates[0].Provider)
				assert.Len(t, warnings, 1)
			})
		})

		t.Run("WHEN eBay is disabled and the price estimated again", func(t *testing.T) {
			w.cfg.on["ebay-prices"] = false
			_, _, err := w.svc.EstimateCopy(ctx, gid, cid)
			require.NoError(t, err)

			t.Run("THEN eBay's estimate goes", func(t *testing.T) {
				assert.Empty(t, w.copy(ctx, t, gid).Estimates)
			})
		})
	})

	t.Run("GIVEN copies that cannot be priced, or no provider enabled", func(t *testing.T) {
		w := newWorld(ctx, t)
		gid, cid := w.physical(ctx, t, "Halo 3", "")
		_, _, errNoBarcode := w.svc.EstimateCopy(ctx, gid, cid)

		w.cfg.on = map[provider.ID]bool{}
		gid2, cid2 := w.physical(ctx, t, "Dead Space 3", deadSpace)
		_, _, errNoProviders := w.svc.EstimateCopy(ctx, gid2, cid2)

		t.Run("THEN each says why", func(t *testing.T) {
			assert.ErrorIs(t, errNoBarcode, valuation.ErrNotValuable)
			assert.ErrorIs(t, errNoProviders, valuation.ErrNoProviders)
			assert.Empty(t, w.cex.calls)
		})
	})
}

func TestCollectionValue(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN two priced copies, one of them in pounds", func(t *testing.T) {
		w := newWorld(ctx, t)
		for _, c := range []struct {
			title string
			code  game.Barcode
		}{{"Dead Space 3", deadSpace}, {"Halo 3", halo}} {
			gid, cid := w.physical(ctx, t, c.title, c.code)
			_, _, err := w.svc.EstimateCopy(ctx, gid, cid)
			require.NoError(t, err)
		}

		t.Run("WHEN the collection's value is asked", func(t *testing.T) {
			v, err := w.svc.CollectionValue(ctx)
			require.NoError(t, err)

			t.Run("THEN each source adds up the euros and counts the rest apart", func(t *testing.T) {
				assert.Equal(t, "EUR", v.Currency)
				require.Len(t, v.Totals, 2)
				assert.Equal(t, valuation.ProviderTotal{Provider: "cex-prices", Name: "cex-prices", Sell: 2000, Copies: 1, OtherCurrency: 1}, v.Totals[0])
				assert.Equal(t, valuation.ProviderTotal{Provider: "ebay-prices", Name: "ebay-prices", Sell: 1400, Copies: 1}, v.Totals[1])
			})
		})

		t.Run("WHEN eBay is disabled", func(t *testing.T) {
			w.cfg.on["ebay-prices"] = false
			v, err := w.svc.CollectionValue(ctx)
			require.NoError(t, err)

			t.Run("THEN it is left out of the totals", func(t *testing.T) {
				require.Len(t, v.Totals, 1)
				assert.Equal(t, provider.ID("cex-prices"), v.Totals[0].Provider)
			})
		})
	})
}

func TestScheduler(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a copy never planned, two copies due and one planned for later", func(t *testing.T) {
		w := newWorld(ctx, t)
		undated, _ := w.physical(ctx, t, "Undated", deadSpace)
		due1, _ := w.physical(ctx, t, "Due 1", deadSpace)
		due2, _ := w.physical(ctx, t, "Due 2", halo)
		later, _ := w.physical(ctx, t, "Later", deadSpace)
		w.physical(ctx, t, "No barcode", "")

		for id, next := range map[game.ID]time.Time{due1: now.Add(-day), due2: now.Add(-time.Hour), later: now.Add(10 * day)} {
			g, err := w.games.Get(ctx, id)
			require.NoError(t, err)
			require.NoError(t, g.PlanValuation(g.Copies()[0].ID, next))
			require.NoError(t, w.games.Save(ctx, g))
		}

		t.Run("WHEN the scheduler ticks", func(t *testing.T) {
			require.NoError(t, w.svc.Tick(ctx))

			t.Run("THEN the undated copy gets a date within 30 days and is not asked yet", func(t *testing.T) {
				assert.Equal(t, now.Add(15*day), w.copy(ctx, t, undated).NextValuation)
				assert.Empty(t, w.copy(ctx, t, undated).Estimates)
			})

			t.Run("AND only the due copies are asked, oldest first, with a pause between them", func(t *testing.T) {
				assert.Equal(t, []game.Barcode{deadSpace, halo}, w.cex.calls)
				assert.Equal(t, []time.Duration{40 * time.Second}, w.sleeps)
				assert.Equal(t, now.Add(30*day), w.copy(ctx, t, due1).NextValuation)
				assert.Equal(t, now.Add(10*day), w.copy(ctx, t, later).NextValuation)
			})
		})
	})

	t.Run("GIVEN a due copy and every provider failing", func(t *testing.T) {
		w := newWorld(ctx, t)
		w.cex.fail, w.ebay.fail = errors.New("down"), errors.New("down")
		gid, _ := w.physical(ctx, t, "Dead Space 3", deadSpace)
		g, err := w.games.Get(ctx, gid)
		require.NoError(t, err)
		require.NoError(t, g.PlanValuation(g.Copies()[0].ID, now.Add(-day)))
		require.NoError(t, w.games.Save(ctx, g))

		t.Run("WHEN the scheduler ticks", func(t *testing.T) {
			require.NoError(t, w.svc.Tick(ctx))

			t.Run("THEN the copy's date still moves forward, so it is not retried in a loop", func(t *testing.T) {
				assert.Equal(t, now.Add(30*day), w.copy(ctx, t, gid).NextValuation)
			})
		})
	})

	t.Run("GIVEN no price provider enabled", func(t *testing.T) {
		w := newWorld(ctx, t)
		w.cfg.on = map[provider.ID]bool{}
		gid, _ := w.physical(ctx, t, "Dead Space 3", deadSpace)

		t.Run("WHEN the scheduler ticks", func(t *testing.T) {
			require.NoError(t, w.svc.Tick(ctx))

			t.Run("THEN nothing is planned nor asked", func(t *testing.T) {
				assert.True(t, w.copy(ctx, t, gid).NextValuation.IsZero())
				assert.Empty(t, w.cex.calls)
			})
		})
	})
}
```

`0882224617016` must be a valid barcode for the domain (Halo 3's UPC as EAN-13); if `normalize` refuses it, use any valid EAN-13.

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/application/valuation/`
Expected: FAIL (build: `NewService` undefined).

- [ ] **Step 3: Implement** — `internal/application/valuation/service.go`:

```go
package valuation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/application/port"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/settings"
)

// Errors returned by the service.
var (
	// ErrNotValuable means the copy is not a physical copy with a barcode.
	ErrNotValuable = errors.New("only physical copies with a barcode can be priced: add the copy's barcode first")

	// ErrNoProviders means no price provider is enabled.
	ErrNoProviders = errors.New("no price provider is enabled: enable one on the Providers page")

	// ErrChanged means the copy's barcode changed while its price was being estimated.
	ErrChanged = errors.New("the copy changed while its price was being estimated: try again")
)

// Days between two estimates of a copy (uniform), and within which an undated copy is first planned.
const (
	minDays   = 20
	maxDays   = 40
	firstDays = 30
)

// providerTimeout bounds one provider's answer, so a slow source never holds the scheduler.
const providerTimeout = 20 * time.Second

// Configs is the port that keeps the providers' configuration (implemented by the media service).
type Configs interface {
	// Providers returns the configured providers of a kind in priority order.
	Providers(ctx context.Context, kind provider.Kind) ([]media.ProviderView, error)
}

// Service exposes the valuation use cases.
type Service struct {
	games   game.Repository
	tx      port.TxManager
	configs Configs
	prefs   settings.Repository
	impls   map[provider.ID]Provider
	now     port.Clock
	log     *slog.Logger

	// random and sleep are replaced in tests (SetChance).
	random func() float64
	sleep  func(ctx context.Context, d time.Duration) error
}

// NewService builds the service with the price providers compiled in.
func NewService(games game.Repository, tx port.TxManager, configs Configs, prefs settings.Repository, now port.Clock,
	log *slog.Logger, providers ...Provider) *Service {
	impls := make(map[provider.ID]Provider, len(providers))
	for _, p := range providers {
		impls[p.Descriptor().ID] = p
	}

	return &Service{
		games:   games,
		tx:      tx,
		configs: configs,
		prefs:   prefs,
		impls:   impls,
		now:     now,
		log:     log,
		random:  rand.Float64,
		sleep:   sleepCtx,
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// between returns a random duration in [lo, hi).
func (s *Service) between(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(s.random()*float64(hi-lo))
}

func days(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

// enabled returns the enabled price providers that are compiled in, in the user's order.
func (s *Service) enabled(ctx context.Context) ([]media.ProviderView, error) {
	views, err := s.configs.Providers(ctx, provider.KindValuation)
	if err != nil {
		return nil, err
	}

	return slices.DeleteFunc(views, func(v media.ProviderView) bool {
		_, ok := s.impls[v.ID()]
		return !v.Enabled() || !ok
	}), nil
}

// answer is what one provider said about a copy.
type answer struct {
	id       provider.ID
	estimate game.Estimate
	listed   bool
	err      error
}

// EstimateCopy asks every enabled price provider for the copy's barcode, keeps the latest estimate
// of each (a provider that failed keeps its previous one; one that no longer lists the product
// loses it) and plans the next valuation 20–40 days ahead. The warnings name the providers that
// failed. The providers are asked outside the transaction, so a slow source never blocks writes.
func (s *Service) EstimateCopy(ctx context.Context, gameID, copyID game.ID) (*game.Game, []string, error) {
	g, err := s.games.Get(ctx, gameID)
	if err != nil {
		return nil, nil, err
	}

	c, err := copyOf(g, copyID)
	if err != nil {
		return nil, nil, err
	}

	views, err := s.enabled(ctx)
	if err != nil {
		return nil, nil, err
	}

	if len(views) == 0 {
		return nil, nil, ErrNoProviders
	}

	answers, warnings := s.ask(ctx, views, c.Barcode)

	var out *game.Game

	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		g, err := s.games.Get(ctx, gameID)
		if err != nil {
			return err
		}

		current, err := copyOf(g, copyID)
		if err != nil {
			return err
		}

		if current.Barcode != c.Barcode {
			return ErrChanged
		}

		next := s.now().Add(days(minDays) + time.Duration(s.random()*float64(days(maxDays-minDays))))
		if _, err := g.SetEstimates(copyID, merge(current.Estimates, answers), next, s.now()); err != nil {
			return err
		}

		out = g

		return s.games.Save(ctx, g)
	})

	return out, warnings, err
}

func copyOf(g *game.Game, id game.ID) (game.Copy, error) {
	for _, c := range g.Copies() {
		if c.ID == id {
			if !c.Valuable() {
				return c, ErrNotValuable
			}

			return c, nil
		}
	}

	return game.Copy{}, game.ErrCopyNotFound
}

func (s *Service) ask(ctx context.Context, views []media.ProviderView, code game.Barcode) ([]answer, []string) {
	answers := make([]answer, 0, len(views))

	var warnings []string

	for _, v := range views {
		pctx, cancel := context.WithTimeout(ctx, providerTimeout)
		e, err := s.impls[v.ID()].Estimate(pctx, v.Settings(), code)
		cancel()

		a := answer{id: v.ID()}

		switch {
		case errors.Is(err, ErrNotListed):
		case err != nil:
			a.err = err
			warnings = append(warnings, fmt.Sprintf("%s: %v", v.Descriptor.Name, err))
			s.log.Warn("estimating a price", "provider", v.ID(), "barcode", code, "error", err)
		default:
			e.Provider, e.FetchedAt = string(v.ID()), s.now()
			a.estimate, a.listed = e, true
		}

		answers = append(answers, a)
	}

	return answers, warnings
}

// merge builds a copy's new estimates: one per enabled provider that listed the product, the
// previous one for a provider that failed. Providers no longer enabled drop out.
func merge(current []game.Estimate, answers []answer) []game.Estimate {
	var out []game.Estimate

	for _, a := range answers {
		switch {
		case a.err != nil:
			if i := slices.IndexFunc(current, func(e game.Estimate) bool { return e.Provider == string(a.id) }); i >= 0 {
				out = append(out, current[i])
			}
		case a.listed:
			out = append(out, a.estimate)
		}
	}

	return out
}

// ProviderTotal is one source's view of the collection's value, in minor units of the currency.
type ProviderTotal struct {
	Provider  provider.ID
	Name      string
	Sell      int64
	BuyCash   int64
	BuyCredit int64

	// Copies is how many copies the totals cover.
	Copies int

	// OtherCurrency is how many of this source's estimates are in another currency and left out.
	OtherCurrency int
}

// CollectionValue is the collection's value per source, in the default currency.
type CollectionValue struct {
	// Currency is the default currency; empty when none is chosen (then nothing is added up).
	Currency string
	Totals   []ProviderTotal
}

// CollectionValue adds up, per enabled source, the latest estimates in the default currency. Sources
// are never mixed: a shop's price and an asking price are different things.
func (s *Service) CollectionValue(ctx context.Context) (CollectionValue, error) {
	prefs, err := s.prefs.Preferences(ctx)
	if err != nil {
		return CollectionValue{}, err
	}

	views, err := s.enabled(ctx)
	if err != nil {
		return CollectionValue{}, err
	}

	out := CollectionValue{Currency: prefs.Currency}
	index := map[string]int{}

	for i, v := range views {
		index[string(v.ID())] = i
		out.Totals = append(out.Totals, ProviderTotal{
			Provider: v.ID(),
			Name:     v.Descriptor.Name,
		})
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return CollectionValue{}, err
	}

	for _, g := range games {
		for _, c := range g.Copies() {
			for _, e := range c.Estimates {
				i, ok := index[e.Provider]
				if !ok {
					continue
				}

				t := &out.Totals[i]
				if out.Currency == "" || e.Sell.Currency != out.Currency {
					t.OtherCurrency++
					continue
				}

				t.Sell += e.Sell.Amount
				t.BuyCash += e.BuyCash.Amount
				t.BuyCredit += e.BuyCredit.Amount
				t.Copies++
			}
		}
	}

	return out, nil
}
```

`internal/application/valuation/scheduler.go`:

```go
package valuation

import (
	"context"
	"errors"
	"sort"
	"time"

	"gamevault/internal/domain/game"
)

// Scheduler timings (uniform at random, so requests never follow a fixed rhythm).
const (
	tickMin  = 3 * time.Minute
	tickMax  = 7 * time.Minute
	pauseMin = 20 * time.Second
	pauseMax = 60 * time.Second
)

// RunScheduler estimates copies on their own dates until ctx ends: every 3–7 minutes it plans the
// undated copies and estimates the due ones, one at a time.
func (s *Service) RunScheduler(ctx context.Context) {
	for {
		if err := s.sleep(ctx, s.between(tickMin, tickMax)); err != nil {
			return
		}

		if err := s.Tick(ctx); err != nil {
			s.log.Warn("estimating prices", "error", err)
		}
	}
}

type due struct {
	game game.ID
	copy game.ID
	at   time.Time
}

// Tick gives a date within the next 30 days to every priceable copy that has none (so the first
// round spreads over the month), then estimates the copies whose date has passed, oldest first,
// pausing 20–60 seconds between two of them. Nothing happens while no price provider is enabled.
func (s *Service) Tick(ctx context.Context) error {
	views, err := s.enabled(ctx)
	if err != nil || len(views) == 0 {
		return err
	}

	list, err := s.plan(ctx)
	if err != nil {
		return err
	}

	for i, d := range list {
		if i > 0 {
			if err := s.sleep(ctx, s.between(pauseMin, pauseMax)); err != nil {
				return nil // shutting down
			}
		}

		if _, warnings, err := s.EstimateCopy(ctx, d.game, d.copy); err != nil {
			if !errors.Is(err, ErrChanged) && !errors.Is(err, ErrNotValuable) && !errors.Is(err, game.ErrCopyNotFound) {
				s.log.Warn("estimating a copy's price", "game", d.game, "error", err)
			}
		} else if len(warnings) > 0 {
			s.log.Info("some price sources failed", "game", d.game, "warnings", warnings)
		}
	}

	return nil
}

// plan dates the undated priceable copies and returns the due ones, oldest first.
func (s *Service) plan(ctx context.Context) ([]due, error) {
	var list []due

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		games, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		now := s.now()

		for _, g := range games {
			changed := false

			for _, c := range g.Copies() {
				if !c.Valuable() {
					continue
				}

				at := c.NextValuation
				if at.IsZero() {
					at = now.Add(time.Duration(s.random() * float64(days(firstDays))))
					if err := g.PlanValuation(c.ID, at); err != nil {
						return err
					}

					changed = true

					continue // a new date is never due on the same tick
				}

				if !at.After(now) {
					list = append(list, due{game: g.ID(), copy: c.ID, at: at})
				}
			}

			if changed {
				if err := s.games.Save(ctx, g); err != nil {
					return err
				}
			}
		}

		return nil
	})

	sort.Slice(list, func(i, j int) bool { return list[i].at.Before(list[j].at) })

	return list, err
}
```

- [ ] **Step 4: Run the tests**

Run: `task go -- test ./internal/application/valuation/ -v`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
git add internal/application/valuation
git commit -m "Valuation service: estimate a copy, the collection's value, and a jittered scheduler"
```

---

### Task 5: CeX price provider

**Files:**
- Create: `internal/adapters/outbound/cex/prices.go`, `internal/adapters/outbound/cex/prices_test.go`
- Modify: `internal/adapters/outbound/cex/provider.go` (box fields), `internal/adapters/outbound/cex/plugin.go`, `web/src/i18n/locales/{en,es}.json` (provider texts, so `plugintest` passes)

**Interfaces:**
- Consumes: `valuation.Provider`, `valuation.ErrNotListed` (Task 3).
- Produces: `cex.PricesID provider.ID = "cex-prices"`; `cex.NewPrices() *Prices` (`Prices.API *apiclient.Client`, BaseURL with `%s` for the country); `cex.ErrPricesBlocked`.

- [ ] **Step 1: Write the failing tests** — `prices_test.go` (package `cex`):

```go
package cex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

var _ valuation.Provider = (*Prices)(nil)

// Real answer of GET https://wss2.cex.es.webuy.io/v3/boxes/5030934110075/detail (2026-10-09), cut.
const deadSpace3Prices = `{"response":{"ack":"Success","data":{"boxDetails":[{"boxId":"5030934110075",
	"boxName":"Dead Space 3 (2 Discs)","categoryName":"Xbox 360 Juegos","superCatId":1,
	"sellPrice":20,"cashPrice":6,"exchangePrice":10,"firstPrice":55,"previousPrice":18}]},"error":{}}}`

// The same box in the UK, with pence (the shape is the same in every country).
const deadSpace3PricesUK = `{"response":{"ack":"Success","data":{"boxDetails":[{"boxId":"5030934110075",
	"boxName":"Dead Space 3","categoryName":"Xbox 360 Software","superCatId":1,
	"sellPrice":4.5,"cashPrice":1,"exchangePrice":1.75}]},"error":{}}}`

const unknownBox = `{"response":{"ack":"Success","data":null,"error":{}}}`

// Cloudflare's challenge page, as CeX's search answered on 2026-10-09 (HTTP 403).
const cloudflarePage = `<!DOCTYPE html><html><head><title>Attention Required! | Cloudflare</title></head></html>`

func pricesServer(t *testing.T, answers map[string]string) (*Prices, *[]string) {
	t.Helper()

	var asked []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/") // country, boxes, ean, detail
		asked = append(asked, parts[0])

		body, ok := answers[parts[0]]
		switch {
		case !ok:
			w.Write([]byte(unknownBox))
		case strings.HasPrefix(body, "<"):
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(body))
		default:
			w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)

	p := NewPrices()
	p.API.BaseURL = srv.URL + "/%s"

	return p, &asked
}

func TestPrices(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN CeX Spain knows the box", func(t *testing.T) {
		p, _ := pricesServer(t, map[string]string{"es": deadSpace3Prices})

		t.Run("WHEN its price is asked with the default countries", func(t *testing.T) {
			e, err := p.Estimate(ctx, schema.Settings{}, "5030934110075")
			require.NoError(t, err)

			t.Run("THEN it gets the sell, cash and credit prices in euro cents, and the product page", func(t *testing.T) {
				assert.Equal(t, game.Money{Amount: 2000, Currency: "EUR"}, e.Sell)
				assert.Equal(t, game.Money{Amount: 600, Currency: "EUR"}, e.BuyCash)
				assert.Equal(t, game.Money{Amount: 1000, Currency: "EUR"}, e.BuyCredit)
				assert.Equal(t, "https://es.webuy.com/product-detail/?id=5030934110075", e.URL)
			})
		})
	})

	t.Run("GIVEN only the UK knows the box, with pence", func(t *testing.T) {
		p, asked := pricesServer(t, map[string]string{"uk": deadSpace3PricesUK})

		t.Run("WHEN Spain then the UK are asked", func(t *testing.T) {
			e, err := p.Estimate(ctx, schema.Settings{"countries": "es, uk"}, "5030934110075")
			require.NoError(t, err)

			t.Run("THEN the UK answers in pence, after Spain", func(t *testing.T) {
				assert.Equal(t, []string{"es", "uk"}, *asked)
				assert.Equal(t, game.Money{Amount: 450, Currency: "GBP"}, e.Sell)
				assert.Equal(t, game.Money{Amount: 175, Currency: "GBP"}, e.BuyCredit)
				assert.Equal(t, "https://uk.webuy.com/product-detail/?id=5030934110075", e.URL)
			})
		})
	})

	t.Run("GIVEN an unknown box, and a box that is not a game", func(t *testing.T) {
		film := strings.Replace(deadSpace3Prices, `"superCatId":1`, `"superCatId":2`, 1)
		p, _ := pricesServer(t, map[string]string{"es": film})
		_, errFilm := p.Estimate(ctx, schema.Settings{}, "5030934110075")

		q, _ := pricesServer(t, nil)
		_, errUnknown := q.Estimate(ctx, schema.Settings{}, "5030934110075")

		t.Run("THEN neither is listed", func(t *testing.T) {
			assert.ErrorIs(t, errFilm, valuation.ErrNotListed)
			assert.ErrorIs(t, errUnknown, valuation.ErrNotListed)
		})
	})

	t.Run("GIVEN CeX answers with a Cloudflare challenge", func(t *testing.T) {
		p, asked := pricesServer(t, map[string]string{"es": cloudflarePage})
		_, err := p.Estimate(ctx, schema.Settings{"countries": "es,uk"}, "5030934110075")

		t.Run("THEN it says CeX is checking for bots and stops", func(t *testing.T) {
			assert.ErrorIs(t, err, ErrPricesBlocked)
			assert.Equal(t, []string{"es"}, *asked)
		})
	})
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/cex/ -run TestPrices`
Expected: FAIL (build: `NewPrices` undefined).

- [ ] **Step 3: Implement** — in `provider.go`, `box` gains (one per line):

```go
	SellPrice     float64 `json:"sellPrice"`
	CashPrice     float64 `json:"cashPrice"`
	ExchangePrice float64 `json:"exchangePrice"`
```

`prices.go`:

```go
package cex

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// PricesID identifies the CeX price provider.
const PricesID provider.ID = "cex-prices"

// ErrPricesBlocked means CeX answered with a bot check instead of its API. It is never worked
// around (see the safety rules).
var ErrPricesBlocked = errors.New("CeX is asking for a browser check; prices from CeX are unavailable for now")

// currencies of the CeX stores with a box detail API.
var currencies = map[string]string{
	"uk": "GBP",
	"es": "EUR",
	"ie": "EUR",
	"pt": "EUR",
	"it": "EUR",
	"au": "AUD",
	"in": "INR",
	"mx": "MXN",
	"pl": "PLN",
}

// Prices implements valuation.Provider with CeX's prices: what the shop sells a product for and
// what it pays for it, in cash or store credit. By the user's decision (2026-10-09) only the prices
// of the user's own copies are asked and stored, one product at a time.
type Prices struct {
	// API calls CeX; its BaseURL has the country code in place of %s.
	API *apiclient.Client
}

// NewPrices returns the CeX price provider with its production endpoints.
func NewPrices() *Prices {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = 10 * time.Second

	return &Prices{API: api}
}

// Descriptor implements valuation.Provider.
func (p *Prices) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               PricesID,
		Kind:             provider.KindValuation,
		Name:             "CeX",
		DescriptionKey:   "providers.cexPrices.description",
		EnabledByDefault: true,
		Fields: schema.Fields{
			{
				Key:      settingCountries,
				LabelKey: "providers.cexPrices.countries",
				HelpKey:  "providers.cexPrices.countriesHelp",
				Kind:     schema.FieldText,
			},
		},
		DefaultOrder: 10,
	}
}

// priceCountries are the countries to ask, in the user's order; Spain when none is set.
func priceCountries(s schema.Settings) []string {
	if strings.TrimSpace(s[settingCountries]) == "" {
		return []string{"es"}
	}

	return allowed(s[settingCountries])
}

// Estimate implements valuation.Provider: the first country whose catalog has the barcode answers.
func (p *Prices) Estimate(ctx context.Context, s schema.Settings, code game.Barcode) (game.Estimate, error) {
	api := &Provider{API: p.API}

	for _, country := range priceCountries(s) {
		b, err := api.detail(ctx, country, code)
		if errors.Is(err, ErrBlocked) {
			return game.Estimate{}, ErrPricesBlocked
		}

		if err != nil {
			return game.Estimate{}, err
		}

		if b == nil {
			continue
		}

		if b.SuperCatID != gamingSuperCat || b.SellPrice <= 0 {
			return game.Estimate{}, valuation.ErrNotListed
		}

		cur := currencies[country]

		return game.Estimate{
			Sell:      money(b.SellPrice, cur),
			BuyCash:   money(b.CashPrice, cur),
			BuyCredit: money(b.ExchangePrice, cur),
			URL:       fmt.Sprintf("https://%s.webuy.com/product-detail/?id=%s", country, code),
		}, nil
	}

	return game.Estimate{}, valuation.ErrNotListed
}

// money converts CeX's decimal price into the currency's minor units (4.5 GBP → 450).
func money(v float64, currency string) game.Money {
	minor := int64(math.Round(v * math.Pow10(game.CurrencyDigits(currency))))
	if minor <= 0 {
		return game.Money{}
	}

	return game.Money{
		Amount:   minor,
		Currency: currency,
	}
}

// Test implements valuation.Provider with one light request (the top categories) to the first
// country.
func (p *Prices) Test(ctx context.Context, s schema.Settings) error {
	var out struct{}

	err := (&Provider{API: p.API}).get(ctx, priceCountries(s)[0], "/supercats", &out)
	if errors.Is(err, ErrBlocked) {
		return ErrPricesBlocked
	}

	return err
}
```

`plugin.go`:

```go
// Plugin is the CeX plugin: barcode lookups in CeX's catalog and CeX's second-hand prices.
func Plugin() plugin.Plugin {
	return plugin.Plugin{
		ID:         "cex",
		Name:       "CeX",
		Barcodes:   []media.BarcodeProvider{New()},
		Valuations: []valuation.Provider{NewPrices()},
	}
}
```

Translations (`en.json`, under `providers`): `"cexPrices": {"description": "What CeX sells your physical games for, and what it pays for them in cash or store credit. Only your own copies with a barcode are asked, one at a time.", "countries": "Countries", "countriesHelp": "CeX stores to ask, in order, separated by commas: es, uk, ie, pt, it… The first that sells the product gives its price, in its currency. Empty: es."}`. `es.json`: `"cexPrices": {"description": "Lo que CeX pide por tus juegos físicos y lo que paga por ellos en efectivo o en vale. Solo se consultan tus copias con código de barras, de una en una.", "countries": "Países", "countriesHelp": "Tiendas de CeX a consultar, en orden y separadas por comas: es, uk, ie, pt, it… La primera que venda el producto da su precio, en su moneda. Vacío: es."}`.

- [ ] **Step 4: Run the tests**

Run: `task go -- test ./internal/adapters/outbound/cex/ ./cmd/...`
Expected: PASS (`TestPlugins` checks the new provider and its texts).

- [ ] **Step 5: Probe the real endpoint** — one request, as a user would make:

```bash
curl -s -A "GameVault/1.0 (+https://github.com/botchris/game-vault)" "https://wss2.cex.es.webuy.io/v3/boxes/5030934110075/detail" | head -c 400
```

Expected: JSON with `sellPrice`, `cashPrice`, `exchangePrice`. If it is a Cloudflare page, note it in the ledger; the provider already fails cleanly.

- [ ] **Step 6: Lint and commit**

```bash
git add internal/adapters/outbound/cex web/src/i18n
git commit -m "CeX prices: what CeX sells and pays for a copy, by barcode"
```

---

### Task 6: eBay price provider

**Files:**
- Create: `internal/adapters/outbound/ebay/prices.go`, `internal/adapters/outbound/ebay/prices_test.go`
- Modify: `internal/adapters/outbound/ebay/provider.go` (settings group, listing price, search query), `internal/adapters/outbound/ebay/plugin.go`, `web/src/i18n/locales/{en,es}.json`

**Interfaces:**
- Consumes: `valuation.Provider`, `ErrNotListed` (Task 3).
- Produces: `ebay.PricesID provider.ID = "ebay-prices"`; `ebay.NewPrices() *Prices` (`Prices.API *Provider`); both eBay descriptors in `SettingsGroup: "ebay"` (keys entered once).

- [ ] **Step 1: Write the failing tests** — `prices_test.go` (package `ebay`):

```go
package ebay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

var _ valuation.Provider = (*Prices)(nil)

// Shape of the Browse API's item_summary/search answer (2026-10, fake values), cut to the fields
// used.
const usedListings = `{"total":5,"itemSummaries":[
	{"title":"Dead Space 3 Xbox 360","price":{"value":"12.00","currency":"EUR"}},
	{"title":"Dead Space 3 PAL","price":{"value":"14.99","currency":"EUR"}},
	{"title":"Dead Space 3 completo","price":{"value":"9.50","currency":"EUR"}},
	{"title":"Dead Space 3","price":{"value":"20.00","currency":"EUR"}},
	{"title":"Dead Space 3 UK","price":{"value":"8.00","currency":"GBP"}}]}`

var keys = schema.Settings{"client_id": "id", "client_secret": "secret"}

type fakeEbay struct {
	tokens   int
	searches []*http.Request
	listings string
	badKeys  bool
}

func (f *fakeEbay) serve(t *testing.T) *Prices {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/identity/v1/oauth2/token":
			f.tokens++
			if f.badKeys {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"invalid_client","error_description":"client authentication failed"}`))

				return
			}

			w.Write([]byte(`{"access_token":"tok","expires_in":7200,"token_type":"Application Access Token"}`))
		case "/buy/browse/v1/item_summary/search":
			f.searches = append(f.searches, r)
			w.Write([]byte(f.listings))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	p := NewPrices()
	p.API.BaseURL = srv.URL

	return p
}

func TestEbayPrices(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN five used listings, one of them in pounds", func(t *testing.T) {
		f := &fakeEbay{listings: usedListings}
		p := f.serve(t)

		t.Run("WHEN the price is asked twice on the Spanish marketplace", func(t *testing.T) {
			e, err := p.Estimate(ctx, keys, "5030934110075")
			require.NoError(t, err)
			_, err = p.Estimate(ctx, keys, "5030934110075")
			require.NoError(t, err)

			t.Run("THEN it is the median of the four in euros, with how many there were", func(t *testing.T) {
				assert.Equal(t, game.Money{Amount: 1350, Currency: "EUR"}, e.Sell) // (12.00 + 14.99) / 2, rounded
				assert.Equal(t, 4, e.Listings)
				assert.Contains(t, e.URL, "ebay.es")
			})

			t.Run("AND it searched used copies by barcode on EBAY_ES, with one token", func(t *testing.T) {
				r := f.searches[0]
				assert.Equal(t, "5030934110075", r.URL.Query().Get("gtin"))
				assert.Equal(t, "conditions:{USED}", r.URL.Query().Get("filter"))
				assert.Equal(t, "EBAY_ES", r.Header.Get("X-EBAY-C-MARKETPLACE-ID"))
				assert.Equal(t, 1, f.tokens)
			})
		})
	})

	t.Run("GIVEN no listings", func(t *testing.T) {
		p := (&fakeEbay{listings: `{"total":0}`}).serve(t)
		_, err := p.Estimate(ctx, keys, "5030934110075")

		t.Run("THEN it is not listed", func(t *testing.T) {
			assert.ErrorIs(t, err, valuation.ErrNotListed)
		})
	})

	t.Run("GIVEN keys eBay rejects", func(t *testing.T) {
		p := (&fakeEbay{badKeys: true}).serve(t)
		_, err := p.Estimate(ctx, keys, "5030934110075")

		t.Run("THEN it says the keys were rejected", func(t *testing.T) {
			assert.ErrorContains(t, err, "eBay rejected the keys")
		})
	})

	t.Run("GIVEN both eBay providers", func(t *testing.T) {
		t.Run("THEN they share one settings group, so the keys are entered once", func(t *testing.T) {
			assert.Equal(t, "ebay", NewPrices().Descriptor().SettingsGroup)
			assert.Equal(t, "ebay", New().Descriptor().SettingsGroup)
		})
	})
}
```

With four listings (12.00, 14.99, 9.50, 20.00 → sorted 9.50, 12.00, 14.99, 20.00), the median is (1200 + 1499) / 2 = 1349.5 → 1350 (round half up).

- [ ] **Step 2: Run them to see them fail**

Run: `task go -- test ./internal/adapters/outbound/ebay/ -run TestEbayPrices`
Expected: FAIL (build: `NewPrices` undefined).

- [ ] **Step 3: Implement** — in `provider.go`:
- `Descriptor()` gains `SettingsGroup: settingsGroup,` with `const settingsGroup = "ebay"` (comment: "both eBay providers use the same developer keys and marketplaces").
- `listing` gains

```go
	Price struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"price"`
```

- `search` takes the query: `func (p *Provider) search(ctx context.Context, tok, marketplace string, q url.Values) ([]listing, error)`; `Lookup` passes `url.Values{"gtin": {string(code)}, "limit": {fmt.Sprint(listingsPerLookup)}}`.
- Update the `marketplacesHelp` texts in `en.json` / `es.json` to say the first marketplace is also the one used for prices (keep the key).

`prices.go`:

```go
package ebay

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"

	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// PricesID identifies the eBay price provider.
const PricesID provider.ID = "ebay-prices"

// listingsPerEstimate is how many used listings the median is taken from.
const listingsPerEstimate = 50

// sites are the eBay domains of the marketplaces, for the link to the listings.
var sites = map[string]string{
	"EBAY_ES": "www.ebay.es",
	"EBAY_GB": "www.ebay.co.uk",
	"EBAY_DE": "www.ebay.de",
	"EBAY_FR": "www.ebay.fr",
	"EBAY_IT": "www.ebay.it",
	"EBAY_US": "www.ebay.com",
}

// Prices implements valuation.Provider with eBay's used listings: the median asking price of the
// listings with the product's barcode on the first configured marketplace. eBay keeps sold prices
// for approved partners, so these are asking prices.
type Prices struct {
	// API is the eBay client (token and search) the barcode provider uses too; its own instance.
	API *Provider
}

// NewPrices returns the eBay price provider with its production endpoints.
func NewPrices() *Prices { return &Prices{API: New()} }

// Descriptor implements valuation.Provider. Its settings are the barcode provider's (one group).
func (p *Prices) Descriptor() provider.Descriptor {
	d := p.API.Descriptor()

	return provider.Descriptor{
		ID:             PricesID,
		Kind:           provider.KindValuation,
		Name:           "eBay",
		DescriptionKey: "providers.ebayPrices.description",
		Fields:         d.Fields,
		SettingsGroup:  d.SettingsGroup,
		DefaultOrder:   20,
	}
}

// Test implements valuation.Provider by getting a token.
func (p *Prices) Test(ctx context.Context, s schema.Settings) error { return p.API.Test(ctx, s) }

// Estimate implements valuation.Provider.
func (p *Prices) Estimate(ctx context.Context, s schema.Settings, code game.Barcode) (game.Estimate, error) {
	tok, err := p.API.accessToken(ctx, s)
	if err != nil {
		return game.Estimate{}, err
	}

	marketplace := marketplaces(s)[0]
	q := url.Values{
		"gtin":   {string(code)},
		"filter": {"conditions:{USED}"},
		"limit":  {strconv.Itoa(listingsPerEstimate)},
	}

	items, err := p.API.search(ctx, tok, marketplace, q)
	if err != nil {
		return game.Estimate{}, err
	}

	currency, amounts := prices(items)
	if len(amounts) == 0 {
		return game.Estimate{}, valuation.ErrNotListed
	}

	site, ok := sites[marketplace]
	if !ok {
		site = "www.ebay.com"
	}

	return game.Estimate{
		Sell: game.Money{
			Amount:   median(amounts),
			Currency: currency,
		},
		Listings: len(amounts),
		URL:      fmt.Sprintf("https://%s/sch/i.html?_nkw=%s", site, code),
	}, nil
}

// prices returns the listings' prices in minor units of the currency most of them use; listings
// in other currencies (sellers abroad) are left out.
func prices(items []listing) (string, []int64) {
	count := map[string]int{}
	for _, it := range items {
		count[it.Price.Currency]++
	}

	currency := ""
	for c, n := range count {
		if c != "" && (n > count[currency] || (n == count[currency] && c < currency)) {
			currency = c
		}
	}

	var out []int64

	for _, it := range items {
		v, err := strconv.ParseFloat(it.Price.Value, 64)
		if err != nil || v <= 0 || it.Price.Currency != currency {
			continue
		}

		out = append(out, int64(math.Round(v*math.Pow10(game.CurrencyDigits(currency)))))
	}

	return currency, out
}

// median of the amounts, rounding half up between the two middle ones.
func median(amounts []int64) int64 {
	s := slices.Clone(amounts)
	slices.Sort(s)

	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}

	return (s[n/2-1] + s[n/2] + 1) / 2
}
```

`plugin.go`: `Valuations: []valuation.Provider{NewPrices()},` and the comment "barcode lookups in eBay's catalog and the asking prices of used listings".

Translations: `en.json` `providers.ebayPrices.description`: "The median asking price of used listings with the copy's barcode on your first eBay marketplace. Uses the same developer keys as eBay barcode lookups." `es.json`: "El precio mediano que piden los anuncios de segunda mano con el código de barras de la copia en tu primer mercado de eBay. Usa las mismas claves de desarrollador que la búsqueda de códigos de barras de eBay."

- [ ] **Step 4: Run the tests**

Run: `task go -- test ./internal/adapters/outbound/ebay/ ./cmd/...`
Expected: PASS.

- [ ] **Step 5: Probe the real endpoint with bogus keys**

```bash
curl -s -w "\n%{http_code}\n" -X POST https://api.ebay.com/identity/v1/oauth2/token -H "Content-Type: application/x-www-form-urlencoded" -u "bogus:bogus" -d "grant_type=client_credentials&scope=https%3A%2F%2Fapi.ebay.com%2Foauth%2Fapi_scope"
```

Expected: `{"error":"invalid_client",…}` and 401, which the provider reports as "eBay rejected the keys: client authentication failed".

- [ ] **Step 6: Lint and commit**

```bash
git add internal/adapters/outbound/ebay web/src/i18n
git commit -m "eBay prices: the median asking price of used listings, sharing the barcode provider's keys"
```

---

### Task 7: API and wiring

**Files:**
- Create: `proto/gamevault/v1/valuation.proto`, `internal/adapters/inbound/rpc/valuation_handler.go`, `internal/adapters/inbound/rpc/valuation_test.go`
- Modify: `proto/gamevault/v1/game.proto` (+ `task generate`), `internal/adapters/inbound/rpc/mapper.go`, `internal/adapters/inbound/rpc/server.go`, `internal/adapters/inbound/rpc/server_test.go`, `cmd/gamevault/main.go`

**Interfaces:**
- Consumes: `valuation.Service` (Task 4), `plugin.Registry.Valuations()` (Task 3).
- Produces:
  - Proto `message Estimate { string provider = 1; Money sell = 2; Money buy_cash = 3; Money buy_credit = 4; int32 listings = 5; string url = 6; google.protobuf.Timestamp fetched_at = 7; }`; `Copy.estimates = 9`, `Copy.next_valuation = 10`.
  - `service ValuationService { rpc EstimateCopy(EstimateCopyRequest) returns (EstimateCopyResponse); rpc GetCollectionValue(GetCollectionValueRequest) returns (GetCollectionValueResponse); }`; `EstimateCopyResponse { Game game = 1; repeated string warnings = 2; }`; `ProviderTotal { string provider = 1; string name = 2; int64 sell_minor = 3; int64 buy_cash_minor = 4; int64 buy_credit_minor = 5; int32 copies = 6; int32 other_currency = 7; }`; `GetCollectionValueResponse { string currency = 1; repeated ProviderTotal totals = 2; }`.
  - `rpc.NewValuationHandler(*valuation.Service) *ValuationHandler`; `rpc.Handlers.Valuation`.

- [ ] **Step 1: Proto** — in `game.proto`, before `message Copy`:

```proto
// Estimate is a source's latest second-hand price for a physical copy. All its amounts are in one
// currency.
message Estimate {
  // The price provider ("cex-prices", "ebay-prices").
  string provider = 1;
  // What the shop sells it for, or the median asking price of the listings.
  Money sell = 2;
  // What a shop pays for it in cash or store credit; absent when the source does not buy.
  Money buy_cash = 3;
  Money buy_credit = 4;
  // How many listings the price was taken from; 0 for a shop's price.
  int32 listings = 5;
  // The product's page at the source.
  string url = 6;
  google.protobuf.Timestamp fetched_at = 7;
}
```

In `Copy`: `// Latest second-hand price per source (physical copies with a barcode).` `repeated Estimate estimates = 9;` and `// When the prices are next estimated; absent when none is planned.` `google.protobuf.Timestamp next_valuation = 10;`.

`valuation.proto`:

```proto
syntax = "proto3";

package gamevault.v1;

import "gamevault/v1/game.proto";

option go_package = "gamevault/internal/gen/gamevault/v1;gamevaultv1";

message EstimateCopyRequest {
  string game_id = 1;
  string copy_id = 2;
}
message EstimateCopyResponse {
  Game game = 1;
  // Sources that failed this time (their previous estimate is kept).
  repeated string warnings = 2;
}

message GetCollectionValueRequest {}

// ProviderTotal is one source's view of the collection's value, in minor units of the currency.
message ProviderTotal {
  string provider = 1;
  string name = 2;
  int64 sell_minor = 3;
  int64 buy_cash_minor = 4;
  int64 buy_credit_minor = 5;
  // How many copies the totals cover.
  int32 copies = 6;
  // Estimates in another currency, left out.
  int32 other_currency = 7;
}
message GetCollectionValueResponse {
  // The default currency; empty when none is chosen (then nothing is added up).
  string currency = 1;
  repeated ProviderTotal totals = 2;
}

// ValuationService estimates second-hand prices of physical copies (CeX, eBay…).
service ValuationService {
  // Asks every enabled price provider now and plans the next estimate 20–40 days ahead.
  rpc EstimateCopy(EstimateCopyRequest) returns (EstimateCopyResponse);
  // The collection's value per source, in the default currency.
  rpc GetCollectionValue(GetCollectionValueRequest) returns (GetCollectionValueResponse);
}
```

Run: `task generate`. Expected: generated code; the build fails until the handler exists.

- [ ] **Step 2: Write the failing end-to-end test** — in `server_test.go`, `newServer` adds a fake price provider and the valuation service:

```go
// fakePrices is a price provider that knows one barcode.
type fakePrices struct{}

func (fakePrices) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "fake-prices",
		Kind:             provider.KindValuation,
		Name:             "Fake prices",
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (fakePrices) Test(context.Context, schema.Settings) error { return nil }

func (fakePrices) Estimate(_ context.Context, _ schema.Settings, code game.Barcode) (game.Estimate, error) {
	if code != "5030934110075" {
		return game.Estimate{}, valuation.ErrNotListed
	}

	return game.Estimate{
		Sell:    game.Money{Amount: 2000, Currency: "EUR"},
		BuyCash: game.Money{Amount: 600, Currency: "EUR"},
	}, nil
}
```

`media.Providers{…, Valuations: []media.Provider{fakePrices{}}}`; `valuationSvc := valuation.NewService(games, db, mediaSvc, sqlite.NewSettingsRepository(db), time.Now, log, fakePrices{})`; `Handlers.Valuation: rpc.NewValuationHandler(valuationSvc)`; `clients.valuation: gamevaultv1connect.NewValuationServiceClient(http.DefaultClient, srv.URL)`. Then `valuation_test.go`:

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

func TestValuation_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a physical copy with a known barcode and euros as the default currency", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})

		_, err := c.system.UpdatePreferences(ctx, connect.NewRequest(&pb.UpdatePreferencesRequest{Preferences: &pb.Preferences{Currency: "EUR"}}))
		require.NoError(t, err)

		created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title:  "Dead Space 3",
			Copies: []*pb.CopyDetails{{Kind: pb.CopyKind_COPY_KIND_PHYSICAL, Platform: "Xbox 360", Barcode: "5030934110075"}},
		}))
		require.NoError(t, err)

		g := created.Msg.Game

		t.Run("WHEN its price is estimated", func(t *testing.T) {
			res, err := c.valuation.EstimateCopy(ctx, connect.NewRequest(&pb.EstimateCopyRequest{GameId: g.Id, CopyId: g.Copies[0].Id}))
			require.NoError(t, err)

			t.Run("THEN the copy shows the estimate and its next date", func(t *testing.T) {
				cp := res.Msg.Game.Copies[0]
				require.Len(t, cp.Estimates, 1)
				assert.Equal(t, "fake-prices", cp.Estimates[0].Provider)
				assert.Equal(t, int64(2000), cp.Estimates[0].Sell.AmountMinor)
				assert.NotNil(t, cp.NextValuation)
			})

			t.Run("AND the collection is worth it", func(t *testing.T) {
				v, err := c.valuation.GetCollectionValue(ctx, connect.NewRequest(&pb.GetCollectionValueRequest{}))
				require.NoError(t, err)
				assert.Equal(t, "EUR", v.Msg.Currency)
				require.Len(t, v.Msg.Totals, 1)
				assert.Equal(t, int64(2000), v.Msg.Totals[0].SellMinor)
				assert.Equal(t, int64(600), v.Msg.Totals[0].BuyCashMinor)
				assert.Equal(t, int32(1), v.Msg.Totals[0].Copies)
			})
		})

		t.Run("WHEN a copy without a barcode is estimated", func(t *testing.T) {
			added, err := c.games.AddCopy(ctx, connect.NewRequest(&pb.AddCopyRequest{GameId: g.Id, Details: &pb.CopyDetails{Kind: pb.CopyKind_COPY_KIND_PHYSICAL}}))
			require.NoError(t, err)

			_, err = c.valuation.EstimateCopy(ctx, connect.NewRequest(&pb.EstimateCopyRequest{GameId: g.Id, CopyId: added.Msg.Game.Copies[1].Id}))

			t.Run("THEN it is refused, saying to add the barcode", func(t *testing.T) {
				assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
			})
		})
	})
}
```

- [ ] **Step 3: Handler, mapping, wiring** — `valuation_handler.go`:

```go
package rpc

import (
	"context"

	"connectrpc.com/connect"

	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// ValuationHandler implements gamevaultv1connect.ValuationServiceHandler on top of the valuation
// use cases.
type ValuationHandler struct {
	valuation *valuation.Service
}

var _ gamevaultv1connect.ValuationServiceHandler = (*ValuationHandler)(nil)

// NewValuationHandler returns the ValuationService handler.
func NewValuationHandler(v *valuation.Service) *ValuationHandler {
	return &ValuationHandler{valuation: v}
}

// EstimateCopy asks every enabled price provider for a copy's price now.
func (h *ValuationHandler) EstimateCopy(ctx context.Context, req *connect.Request[pb.EstimateCopyRequest]) (*connect.Response[pb.EstimateCopyResponse], error) {
	g, warnings, err := h.valuation.EstimateCopy(ctx, game.ID(req.Msg.GameId), game.ID(req.Msg.CopyId))
	if err != nil {
		return nil, toConnectError(err)
	}

	return connect.NewResponse(&pb.EstimateCopyResponse{
		Game:     gameToPB(g),
		Warnings: warnings,
	}), nil
}

// GetCollectionValue returns the collection's value per source.
func (h *ValuationHandler) GetCollectionValue(ctx context.Context, _ *connect.Request[pb.GetCollectionValueRequest]) (*connect.Response[pb.GetCollectionValueResponse], error) {
	v, err := h.valuation.CollectionValue(ctx)
	if err != nil {
		return nil, toConnectError(err)
	}

	out := &pb.GetCollectionValueResponse{Currency: v.Currency}
	for _, t := range v.Totals {
		out.Totals = append(out.Totals, &pb.ProviderTotal{
			Provider:       string(t.Provider),
			Name:           t.Name,
			SellMinor:      t.Sell,
			BuyCashMinor:   t.BuyCash,
			BuyCreditMinor: t.BuyCredit,
			Copies:         int32(t.Copies),
			OtherCurrency:  int32(t.OtherCurrency),
		})
	}

	return connect.NewResponse(out), nil
}
```

In `mapper.go`: per copy `Estimates: estimatesToPB(c.Estimates),` and `NextValuation: optionalTS(c.NextValuation),` with

```go
func optionalTS(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}

	return ts(t)
}

func moneyOrNil(m game.Money) *pb.Money {
	if m.IsZero() {
		return nil
	}

	return &pb.Money{
		AmountMinor: m.Amount,
		Currency:    m.Currency,
	}
}

func estimatesToPB(estimates []game.Estimate) []*pb.Estimate {
	out := make([]*pb.Estimate, 0, len(estimates))
	for _, e := range estimates {
		out = append(out, &pb.Estimate{
			Provider:  e.Provider,
			Sell:      moneyOrNil(e.Sell),
			BuyCash:   moneyOrNil(e.BuyCash),
			BuyCredit: moneyOrNil(e.BuyCredit),
			Listings:  int32(e.Listings),
			Url:       e.URL,
			FetchedAt: ts(e.FetchedAt),
		})
	}

	return out
}
```

(If the mapper already has a helper for optional timestamps or money, reuse it instead.) `toConnectError`: `valuation.ErrNotValuable`, `valuation.ErrNoProviders` → `CodeFailedPrecondition`; `valuation.ErrChanged` → `CodeAborted`. `server.go`: `Handlers.Valuation *ValuationHandler` (documented), `mux.Handle(gamevaultv1connect.NewValuationServiceHandler(h.Valuation, ic))`. `main.go`:

```go
	valuationSvc := valuation.NewService(games, db, mediaSvc, settingsRepo, now, log, plugins.Valuations()...)
```

after `mediaSvc`; in the unattended block, `go valuationSvc.RunScheduler(ctx)`; `Valuation: rpc.NewValuationHandler(valuationSvc)` in the handlers.

- [ ] **Step 4: Run the tests**

Run: `task go -- test ./...` then `task lint` and `task test`.
Expected: PASS, 0 issues.

- [ ] **Step 5: Commit**

```bash
git add proto internal web/src/gen cmd
git commit -m "Valuation API: estimate a copy now and the collection's value; the scheduler runs with the server"
```

---

### Task 8: UI — prices on copies, the Prices providers and the collection's value

**Files:**
- Create: `web/src/lib/usePriceProviders.ts`, `web/src/features/library/CopyValue.tsx`
- Modify: `web/src/api/client.ts`, `web/src/features/library/GameDetail.tsx`, `web/src/features/providers/ProvidersPage.tsx`, `web/src/features/system/SystemPage.tsx`, `web/src/styles.css`, `web/src/i18n/locales/{en,es}.json`

**Interfaces:**
- Consumes: `ValuationService` (Task 7), provider kind `"valuation"` on the provider service.
- Produces: `valuationClient`; `usePriceProviders(): Map<string, string>` (enabled price providers, id → name); `forgetPriceProviders()`; `<CopyValue game copy onAddBarcode />`.

- [ ] **Step 1: Client and hook** — `client.ts`: import `ValuationService` from `../gen/gamevault/v1/valuation_pb` and `export const valuationClient = createClient(ValuationService, transport);`. `web/src/lib/usePriceProviders.ts`:

```ts
import { useEffect, useState } from 'react';
import { providerClient } from '../api/client';

let cache: Promise<Map<string, string>> | null = null;

/** The enabled price providers (id → name), fetched once per page load. */
export function usePriceProviders(): Map<string, string> {
  const [providers, setProviders] = useState<Map<string, string>>(new Map());
  useEffect(() => {
    cache ??= providerClient.listProviders({ kind: 'valuation' }).then(
      (res) => new Map(res.providers.filter((p) => p.enabled).map((p) => [p.id, p.name])),
      () => new Map(),
    );
    let live = true;
    cache.then((m) => { if (live) setProviders(m); });
    return () => { live = false; };
  }, []);
  return providers;
}

/** Forgets the cached list, after the user changed the providers. */
export function forgetPriceProviders() {
  cache = null;
}
```

- [ ] **Step 2: `CopyValue.tsx`**:

```tsx
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage, valuationClient } from '../../api/client';
import { useFormatters } from '../../components/ui';
import type { Estimate, Money } from '../../gen/gamevault/v1/game_pb';
import { formatAmount } from '../../lib/money';
import { toDate, type Copy, type Game } from '../../lib/model';
import { usePriceProviders } from '../../lib/usePriceProviders';
import { useAppData } from '../../state/AppData';

/** A physical copy's second-hand prices: one line per source, the date, and "Update price". */
export default function CopyValue({ game, copy, onAddBarcode }: { game: Game; copy: Copy; onAddBarcode: () => void }) {
  const { t, i18n } = useTranslation();
  const fmt = useFormatters();
  const { putGame } = useAppData();
  const providers = usePriceProviders();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  if (providers.size === 0) return null;
  if (!copy.details?.barcode) {
    return <p className="small copy-value"><button className="link" onClick={onAddBarcode}>{t('value.addBarcode')}</button></p>;
  }

  const money = (m?: Money) => (m?.amountMinor ? formatAmount(m.amountMinor, m.currency, i18n.language) : '');
  const line = (e: Estimate) => {
    const name = providers.get(e.provider) ?? e.provider;
    if (e.listings > 0) return t('value.listed', { name, price: money(e.sell), count: e.listings });
    const sells = t('value.sells', { name, price: money(e.sell) });
    return e.buyCash?.amountMinor || e.buyCredit?.amountMinor ? sells + t('value.pays', { cash: money(e.buyCash), credit: money(e.buyCredit) }) : sells;
  };
  const update = async () => {
    setBusy(true);
    setError('');
    try {
      const res = await valuationClient.estimateCopy({ gameId: game.id, copyId: copy.id });
      putGame(res.game!);
      if (res.warnings.length) setError(res.warnings.join(' · '));
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  const fetched = copy.estimates.map((e) => toDate(e.fetchedAt)).filter((d): d is Date => !!d);
  const latest = fetched.length ? new Date(Math.max(...fetched.map((d) => d.getTime()))) : null;
  const planned = toDate(copy.nextValuation);

  return (
    <div className="copy-value small">
      {copy.estimates.map((e) => (
        <a key={e.provider} href={e.url || undefined} target="_blank" rel="noreferrer">{line(e)}</a>
      ))}
      <span className="muted">{latest ? fmt.date(latest) : planned ? t('value.planned', { date: fmt.date(planned) }) : t('value.pending')}</span>
      <button className="link" onClick={update} disabled={busy}>{busy ? t('value.updating') : t('value.update')}</button>
      {error && <span className="copy-value-error">{error}</span>}
    </div>
  );
}
```

Check `formatAmount`'s first parameter type (`bigint`): `amountMinor` from the generated `Money` is a `bigint` (int64), so it matches.

- [ ] **Step 3: Use it** — in `GameDetail.tsx`'s `CopiesTab`, for physical copies (`d.kind === CopyKind.PHYSICAL`), after the `extra` line: `<CopyValue game={game} copy={c} onAddBarcode={() => setDialog({ type: 'editCopy', copy: c })} />`. In `ProvidersPage.tsx`, a fourth chain after the metadata one: `<ProviderChain kind="valuation" title={t('providers.valuations')} intro={t('providers.valuationsIntro')} onNotice={setNotice} onChanged={forgetPriceProviders} />` (extend the `kind` union with `'valuation'`; if `onChanged` does not exist on `ProviderChain`, call `forgetPriceProviders()` where a provider is saved or reordered in that component). In `SystemPage.tsx`, load `valuationClient.getCollectionValue({})` with the other data (catch to `null`), and add a card before the backups card when some total has copies or other-currency estimates:

```tsx
      {value && value.totals.length > 0 && (
        <section className="card">
          <h2>{t('system.collectionValue')}</h2>
          {!value.currency && <p className="muted">{t('system.valueNoCurrency')}</p>}
          {value.currency && value.totals.map((v) => {
            const money = (minor: bigint) => formatAmount(minor, value.currency, i18n.language);
            return (
              <p key={v.provider}>
                {v.buyCashMinor > 0n || v.buyCreditMinor > 0n
                  ? t('system.valueShop', { name: v.name, sell: money(v.sellMinor), cash: money(v.buyCashMinor), credit: money(v.buyCreditMinor) })
                  : t('system.valueListed', { name: v.name, sell: money(v.sellMinor) })}
                {' · '}<span className="muted">{t('system.valueCopies', { count: v.copies })}</span>
                {v.otherCurrency > 0 && <span className="muted"> · {t('system.valueOther', { count: v.otherCurrency })}</span>}
              </p>
            );
          })}
        </section>
      )}
```

- [ ] **Step 4: Styles** — append to `styles.css` (use the existing tokens):

```css
/* Second-hand prices on a physical copy (features/library/CopyValue.tsx) */
.copy-value { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 12px; margin: 4px 0 0; }
.copy-value a { color: var(--text); text-decoration: underline; text-decoration-color: var(--border); text-underline-offset: 3px; }
.copy-value-error { flex-basis: 100%; color: var(--danger); }
```

- [ ] **Step 5: Translations** — `en.json`, top-level `value`: `"sells": "{{name}}: sells {{price}}"`, `"pays": ", pays {{cash}} in cash / {{credit}} in credit"`, `"listed_one": "{{name}}: {{price}} ({{count}} listing)"`, `"listed_other": "{{name}}: {{price}} ({{count}} listings)"`, `"update": "Update price"`, `"updating": "Updating…"`, `"pending": "Price pending"`, `"planned": "Price pending (planned for {{date}})"`, `"addBarcode": "Add the barcode to estimate its price"`. In `providers`: `"valuations": "Prices"`, `"valuationsIntro": "Second-hand prices of your physical copies with a barcode. Every enabled source is asked, each copy on its own date about once a month; the order is only how they are listed."`. In `system`: `"collectionValue": "Collection value"`, `"valueShop": "{{name}} sells your collection for {{sell}} and would pay {{cash}} in cash ({{credit}} in credit)"`, `"valueListed": "On {{name}} it is listed at {{sell}}"`, `"valueCopies_one": "{{count}} copy"`, `"valueCopies_other": "{{count}} copies"`, `"valueOther_one": "{{count}} estimate in another currency is not included"`, `"valueOther_other": "{{count}} estimates in other currencies are not included"`, `"valueNoCurrency": "Choose a default currency in Preferences to add up the estimates."`. `es.json`: `value`: `"sells": "{{name}}: la vende a {{price}}"`, `"pays": ", paga {{cash}} en efectivo / {{credit}} en vale"`, `"listed_one": "{{name}}: {{price}} ({{count}} anuncio)"`, `"listed_other": "{{name}}: {{price}} ({{count}} anuncios)"`, `"update": "Actualizar precio"`, `"updating": "Actualizando…"`, `"pending": "Precio pendiente"`, `"planned": "Precio pendiente (previsto el {{date}})"`, `"addBarcode": "Añade el código de barras para estimar su precio"`; `providers`: `"valuations": "Precios"`, `"valuationsIntro": "Precios de segunda mano de tus copias físicas con código de barras. Se consultan todas las fuentes activadas, cada copia en su propia fecha, más o menos una vez al mes; el orden solo indica cómo se muestran."`; `system`: `"collectionValue": "Valor de la colección"`, `"valueShop": "{{name}} vende tu colección por {{sell}} y pagaría {{cash}} en efectivo ({{credit}} en vale)"`, `"valueListed": "En {{name}} está anunciada por {{sell}}"`, `"valueCopies_one": "{{count}} copia"`, `"valueCopies_other": "{{count}} copias"`, `"valueOther_one": "{{count}} estimación en otra moneda no se suma"`, `"valueOther_other": "{{count}} estimaciones en otras monedas no se suman"`, `"valueNoCurrency": "Elige una moneda por defecto en Preferencias para sumar las estimaciones."`.

- [ ] **Step 6: Check**

Run: `task test`. Then `task test-server` (the scheduler is off: `-no-unattended`), and in the browser pane (`?v=<n>`):
- Providers: the "Prices" section lists CeX (enabled) and eBay (disabled; its keys come from the eBay barcode provider if set).
- Library → a game → Copies: a physical copy with the Dead Space 3 barcode `5030934110075` (add one if the copy of the config has none) shows "Price pending", and "Update price" fetches CeX's real price (one request; this is the user-initiated query) and shows "CeX: sells 20 €, pays 6 € in cash / 10 € in credit" with today's date, linking to CeX's page.
- A physical copy without a barcode shows "Add the barcode…" opening its form.
- System → "Collection value" shows CeX's total.
- Mobile preset: the copy line wraps; reset the viewport; `task test-server:stop`.

- [ ] **Step 7: Commit**

```bash
git add web/src
git commit -m "UI: second-hand prices on physical copies, the Prices providers and the collection's value"
```

---

### Task 9: Docs, review and PR

**Files:**
- Modify: `docs/technical.md`, `README.md`, `.claude/memory/cex.md`, `.claude/memory/data-model.md`, `.claude/MEMORY.md`, `docs/superpowers/specs/2026-10-09-valuation-design.md`

- [ ] **Step 1: `docs/technical.md`** — a "Second-hand prices" section: which copies (physical with a barcode), the two sources and what each figure means (CeX sell / cash / credit; eBay median asking price of used listings on the first marketplace, sold prices being reserved to eBay partners), the per-copy random schedule (20–40 days; undated copies within 30 days; a tick every 3–7 minutes; 20–60 s between copies; off with `-no-unattended`), failures (a failing source keeps its previous estimate, a source that no longer lists the product loses it, the date always moves forward), totals per source in the default currency, CeX's Cloudflare check never worked around, and that eBay prices share the barcode provider's keys (settings group). Add `valuation/` to the repository layout and the "Price providers" kind next to covers/barcodes/details.
- [ ] **Step 2: README** — one line in the product description: it estimates the second-hand value of your physical games (CeX, eBay).
- [ ] **Step 3: Memory** — `.claude/memory/cex.md`: dated 2026-10-09, the user's decision allowing CeX prices of the user's own copies (one query per copy, never crawling, no images), the price fields (`sellPrice`, `cashPrice`, `exchangePrice`, decimal units of the country's currency), the product URL `https://{country}.webuy.com/product-detail/?id={EAN}`, and that `/boxes?q=` search answered with a Cloudflare challenge (403) on 2026-10-09 in ES and UK. Update its line in `.claude/MEMORY.md`. `.claude/memory/data-model.md`: copies' `Estimates` / `NextValuation`, document fields `estimates` / `nextValuation`, cleared when the barcode or kind changes.
- [ ] **Step 4: Spec** — note under "eBay prices" that the keys are shared through the settings group `ebay` instead of entered again (decided while planning: the provider framework already shares settings within a group).
- [ ] **Step 5: Full verification** — `task lint`, `task test`, the test-server checks of Task 8 on desktop and mobile.
- [ ] **Step 6: Commit the docs**

```bash
git add docs README.md .claude
git commit -m "Document second-hand price estimates"
```

- [ ] **Step 7: Final review** — a fresh reviewer on the most capable model reviews the whole branch against the spec and the Review Focus list; fix what it finds in separate commits.
- [ ] **Step 8: PR** — push `feature/valuation`, open a PR to `main` titled "Second-hand price estimates for physical copies (CeX, eBay)" with a summary, the verification, what was probed against the real services (one CeX query, eBay with bogus keys) and what only against fakes (eBay listings and prices). Then `mcp__ccd_pr__get_status`. No tags.
