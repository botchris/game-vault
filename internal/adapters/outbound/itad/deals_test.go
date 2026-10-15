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
			got, err := d.Deals(ctx, goodSettings, home.DealQuery{
				List:    home.DealsFeatured,
				Country: "DE",
				Limit:   40,
			})
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
					Price: game.Money{
						Amount:   499,
						Currency: "EUR",
					},
					Regular: game.Money{
						Amount:   1999,
						Currency: "EUR",
					},
					Cut:           75,
					HistoricalLow: true,
					Expires:       time.Date(2026, 10, 20, 17, 0, 0, 0, time.UTC),
					URL:           "https://itad.link/018d9386-0000-7000-8000-00000000aaaa/",
					ImageURL:      "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000001/boxart.jpg?t=1",
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
			got, err := d.Deals(ctx, goodSettings, home.DealQuery{
				List:  home.DealsHistoricalLows,
				Limit: 40,
			})
			require.NoError(t, err)

			t.Run("THEN the deepest cuts are asked for, a full page of them", func(t *testing.T) {
				q := f.sentQueries()
				last := q[len(q)-1]
				assert.Equal(t, "-cut", last.Get("sort"))
				assert.Equal(t, "200", last.Get("limit"))
			})

			t.Run("AND only the deals at their lowest price are kept", func(t *testing.T) {
				titles := make([]string, 0, len(got))
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
			_, err := d.Deals(ctx, s, home.DealQuery{
				List:    home.DealsFeatured,
				Country: "de",
				Limit:   10,
			})
			require.NoError(t, err)
			_, err = d.Deals(ctx, s, home.DealQuery{
				List:  home.DealsFeatured,
				Limit: 10,
			})
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
				_, err := d.Deals(ctx, tc.s, home.DealQuery{
					List:  home.DealsFeatured,
					Limit: 10,
				})

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
			_, err := d.Deals(ctx, schema.Settings{settingAPIKey: "bogus"}, home.DealQuery{
				List:  home.DealsFeatured,
				Limit: 10,
			})

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
			_, err := d.Deals(ctx, goodSettings, home.DealQuery{
				List:  home.DealsFeatured,
				Limit: 10,
			})

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

			t.Run("THEN it works, with the featured list's request as a refresh sends it", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, f.sentQueries(), 1)

				q := f.sentQueries()[0]
				assert.Equal(t, "ES", q.Get("country"))
				assert.Equal(t, "40", q.Get("limit"))
				assert.Equal(t, "false", q.Get("mature"))
				assert.False(t, q.Has("sort"))
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
