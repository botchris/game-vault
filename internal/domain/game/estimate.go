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

// Validate reports whether the estimate can be stored: a provider, a price, one currency.
func (e Estimate) Validate() error {
	_, err := e.normalize()

	return err
}

// Valuable reports whether the copy can have price estimates: a physical copy with a barcode,
// which is how the sources find the product.
func (c Copy) Valuable() bool { return c.Kind == KindPhysical && c.Barcode != "" }

// clearValuation drops the estimates and the next date (they were for another product).
func (c *Copy) clearValuation() {
	c.Estimates, c.NextValuation, c.ValuedAt = nil, time.Time{}, time.Time{}
}

// SetEstimates replaces a copy's price estimates (one per provider), records when they were asked
// and plans the next valuation.
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

	g.copies[i].Estimates, g.copies[i].NextValuation, g.copies[i].ValuedAt = out, next.UTC(), now
	// Only the copy changes: the game's own update time drives its cached cover and details,
	// which an estimate does not affect.
	g.copies[i].UpdatedAt = now

	return g.copies[i].clone(), nil
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
