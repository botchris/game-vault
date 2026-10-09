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
