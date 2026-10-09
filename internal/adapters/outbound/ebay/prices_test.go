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
				assert.Equal(t, game.Money{
					Amount:   1350,
					Currency: "EUR",
				}, e.Sell) // (12.00 + 14.99) / 2, rounded
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
