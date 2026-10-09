package game

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func eur(minor int64) Money {
	return Money{
		Amount:   minor,
		Currency: "EUR",
	}
}

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
				{
					Provider:  "ebay-prices",
					Sell:      eur(1400),
					Listings:  9,
					FetchedAt: t0,
				},
				{
					Provider:  "cex-prices",
					Sell:      eur(2000),
					BuyCash:   eur(600),
					BuyCredit: eur(1000),
					FetchedAt: t0,
				},
			}, next, t0)
			require.NoError(t, err)

			t.Run("THEN they are kept sorted by source, with the next date", func(t *testing.T) {
				require.Len(t, c.Estimates, 2)
				assert.Equal(t, "cex-prices", c.Estimates[0].Provider)
				assert.Equal(t, next, c.NextValuation)
			})

			t.Run("AND changing other details keeps them", func(t *testing.T) {
				_, err := g.UpdateCopy(id, CopyDetails{
					Kind:     KindPhysical,
					Platform: "Xbox 360",
					Barcode:  "5030934110075",
					Notes:    "x",
				}, t0)
				require.NoError(t, err)
				assert.Len(t, g.Copies()[0].Estimates, 2)
			})

			t.Run("AND changing the barcode drops them and the date", func(t *testing.T) {
				_, err := g.UpdateCopy(id, CopyDetails{
					Kind:     KindPhysical,
					Platform: "Xbox 360",
					Barcode:  "5030930112256",
				}, t0)
				require.NoError(t, err)
				assert.Empty(t, g.Copies()[0].Estimates)
				assert.True(t, g.Copies()[0].NextValuation.IsZero())
			})
		})

		t.Run("WHEN an estimate mixes currencies, has no price, or a source appears twice", func(t *testing.T) {
			cases := [][]Estimate{
				{{
					Provider: "cex-prices",
					Sell:     eur(2000),
					BuyCash: Money{
						Amount:   500,
						Currency: "GBP",
					},
				}},
				{{Provider: "cex-prices"}},
				{{
					Provider: "cex-prices",
					Sell:     eur(1),
				}, {
					Provider: "cex-prices",
					Sell:     eur(2),
				}},
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
				_, err := g.SetEstimates(id, []Estimate{{
					Provider: "cex-prices",
					Sell:     eur(1),
				}}, next, t0)

				var ve *ValidationError
				assert.ErrorAs(t, err, &ve)
				assert.ErrorAs(t, g.PlanValuation(id, next), &ve)
			}
		})
	})

	t.Run("GIVEN a priced copy", func(t *testing.T) {
		g, id := valuableGame(t)
		_, err := g.SetEstimates(id, []Estimate{{
			Provider: "cex-prices",
			Sell:     eur(2000),
		}}, next, t0)
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
		_, err := g.SetEstimates(id, []Estimate{{
			Provider: "cex-prices",
			Sell:     eur(2000),
		}}, next, t0)
		require.NoError(t, err)

		c := &g.copies[0]

		t.Run("WHEN a re-import keeps the barcode", func(t *testing.T) {
			c.applyImport(CopyDetails{
				Kind:  KindPhysical,
				Notes: "shelf",
			})

			t.Run("THEN the estimates stay", func(t *testing.T) {
				assert.Len(t, c.Estimates, 1)
			})
		})

		t.Run("WHEN a re-import brings another barcode", func(t *testing.T) {
			c.applyImport(CopyDetails{
				Kind:    KindPhysical,
				Barcode: "5030930112256",
			})

			t.Run("THEN the estimates and the date go", func(t *testing.T) {
				assert.Empty(t, c.Estimates)
				assert.True(t, c.NextValuation.IsZero())
			})
		})
	})

	t.Run("GIVEN a priced copy", func(t *testing.T) {
		g, id := valuableGame(t)
		_, err := g.SetEstimates(id, []Estimate{{
			Provider: "cex-prices",
			Sell:     eur(2000),
		}}, next, t0)
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
