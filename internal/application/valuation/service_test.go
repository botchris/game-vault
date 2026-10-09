package valuation_test

import (
	"context"
	"errors"
	"fmt"
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
	out := make([]media.ProviderView, 0, len(c.impls))

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
		cex: &prices{
			id: "cex-prices",
			sell: map[game.Barcode]game.Money{deadSpace: {
				Amount:   2000,
				Currency: "EUR",
			}, halo: {
				Amount:   900,
				Currency: "GBP",
			}},
		},
		ebay: &prices{
			id: "ebay-prices",
			sell: map[game.Barcode]game.Money{deadSpace: {
				Amount:   1400,
				Currency: "EUR",
			}},
		},
	}
	w.cfg = &configs{
		impls: []valuation.Provider{w.cex, w.ebay},
		on:    map[provider.ID]bool{"cex-prices": true, "ebay-prices": true},
	}
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

	c, err := g.AddCopy(game.CopyDetails{
		Kind:    game.KindPhysical,
		Barcode: code,
	}, now)
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
				assert.Equal(t, valuation.ProviderTotal{
					Provider:      "cex-prices",
					Name:          "cex-prices",
					Sell:          2000,
					Copies:        1,
					OtherCurrency: 1,
				}, v.Totals[0])
				assert.Equal(t, valuation.ProviderTotal{
					Provider: "ebay-prices",
					Name:     "ebay-prices",
					Sell:     1400,
					Copies:   1,
				}, v.Totals[1])
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

func TestEstimateCopy_invalidAnswer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a source that answers a price of zero (listings that round to nothing)", func(t *testing.T) {
		w := newWorld(ctx, t)
		w.cex.sell[deadSpace] = game.Money{}
		gid, cid := w.physical(ctx, t, "Dead Space 3", deadSpace)

		t.Run("WHEN the copy is estimated", func(t *testing.T) {
			_, warnings, err := w.svc.EstimateCopy(ctx, gid, cid)
			require.NoError(t, err)

			t.Run("THEN that answer counts as a failure, the other source is kept and the date moves", func(t *testing.T) {
				c := w.copy(ctx, t, gid)
				require.Len(t, c.Estimates, 1)
				assert.Equal(t, "ebay-prices", c.Estimates[0].Provider)
				assert.Len(t, warnings, 1)
				assert.Equal(t, now.Add(30*day), c.NextValuation)
			})
		})
	})
}

func TestScheduler_failingSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN three due copies and CeX failing (a bot check)", func(t *testing.T) {
		w := newWorld(ctx, t)
		w.cex.fail = errors.New("CeX is asking for a browser check")

		ids := make([]game.ID, 0, 3)

		for _, title := range []string{"A", "B", "C"} {
			gid, _ := w.physical(ctx, t, title, deadSpace)
			g, err := w.games.Get(ctx, gid)
			require.NoError(t, err)
			require.NoError(t, g.PlanValuation(g.Copies()[0].ID, now.Add(-day)))
			require.NoError(t, w.games.Save(ctx, g))

			ids = append(ids, gid)
		}

		t.Run("WHEN the scheduler ticks", func(t *testing.T) {
			require.NoError(t, w.svc.Tick(ctx))

			t.Run("THEN CeX is asked once and skipped for the rest of the round, eBay still answers, and every date moves", func(t *testing.T) {
				assert.Len(t, w.cex.calls, 1)
				assert.Len(t, w.ebay.calls, 3)

				for _, id := range ids {
					c := w.copy(ctx, t, id)
					assert.Equal(t, now.Add(30*day), c.NextValuation)
					require.Len(t, c.Estimates, 1)
				}
			})
		})
	})
}

// dueCopies adds n copies due yesterday and returns their game ids.
func (w *world) dueCopies(ctx context.Context, t *testing.T, n int) []game.ID {
	t.Helper()

	ids := make([]game.ID, 0, n)

	for i := range n {
		gid, _ := w.physical(ctx, t, fmt.Sprintf("Due %d", i), deadSpace)
		g, err := w.games.Get(ctx, gid)
		require.NoError(t, err)
		require.NoError(t, g.PlanValuation(g.Copies()[0].ID, now.Add(-day)))
		require.NoError(t, w.games.Save(ctx, g))

		ids = append(ids, gid)
	}

	return ids
}

func TestScheduler_changesDuringARound(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN two due copies, and the second is priced by hand while the round waits", func(t *testing.T) {
		w := newWorld(ctx, t)
		ids := w.dueCopies(ctx, t, 2)
		w.svc.SetChance(func() float64 { return 0.5 }, func(context.Context, time.Duration) error {
			g, err := w.games.Get(ctx, ids[1])
			require.NoError(t, err)
			require.NoError(t, g.PlanValuation(g.Copies()[0].ID, now.Add(20*day)))

			return w.games.Save(ctx, g)
		})

		t.Run("WHEN the scheduler ticks", func(t *testing.T) {
			require.NoError(t, w.svc.Tick(ctx))

			t.Run("THEN the second copy is no longer due and is not asked again", func(t *testing.T) {
				assert.Len(t, w.cex.calls, 1)
			})
		})
	})

	t.Run("GIVEN three due copies, and every price provider is disabled while the round waits", func(t *testing.T) {
		w := newWorld(ctx, t)
		w.dueCopies(ctx, t, 3)

		pauses := 0

		w.svc.SetChance(func() float64 { return 0.5 }, func(context.Context, time.Duration) error {
			pauses++
			w.cfg.on = map[provider.ID]bool{}

			return nil
		})

		t.Run("WHEN the scheduler ticks", func(t *testing.T) {
			require.NoError(t, w.svc.Tick(ctx))

			t.Run("THEN the round stops instead of pausing for every remaining copy", func(t *testing.T) {
				assert.Equal(t, 1, pauses)
				assert.Len(t, w.cex.calls, 1)
			})
		})
	})
}
