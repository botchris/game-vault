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
