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
