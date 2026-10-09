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
				assert.Equal(t, game.Money{
					Amount:   2000,
					Currency: "EUR",
				}, e.Sell)
				assert.Equal(t, game.Money{
					Amount:   600,
					Currency: "EUR",
				}, e.BuyCash)
				assert.Equal(t, game.Money{
					Amount:   1000,
					Currency: "EUR",
				}, e.BuyCredit)
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
				assert.Equal(t, game.Money{
					Amount:   450,
					Currency: "GBP",
				}, e.Sell)
				assert.Equal(t, game.Money{
					Amount:   175,
					Currency: "GBP",
				}, e.BuyCredit)
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

func TestPrices_countries(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN both CeX providers", func(t *testing.T) {
		t.Run("THEN they share one settings group, so the countries are set once", func(t *testing.T) {
			assert.Equal(t, "cex", NewPrices().Descriptor().SettingsGroup)
			assert.Equal(t, "cex", New().Descriptor().SettingsGroup)
		})
	})

	t.Run("GIVEN a countries setting with no valid country", func(t *testing.T) {
		p, asked := pricesServer(t, nil)
		_, err := p.Estimate(ctx, schema.Settings{"countries": "xx, zz"}, "5030934110075")

		t.Run("THEN only Spain is asked, not every CeX store", func(t *testing.T) {
			assert.ErrorIs(t, err, valuation.ErrNotListed)
			assert.Equal(t, []string{"es"}, *asked)
		})
	})
}
