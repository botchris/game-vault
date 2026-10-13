// Package providers keeps the configuration of every provider, whatever it provides (covers,
// barcode lookups, game sheets, prices): which ones are enabled, in what order, with which settings,
// and the provider-level problems they currently have. The services that run the providers (media,
// valuation) read their chains here.
//
// The configuration is read from storage once, on first use, and kept in memory; it changes only
// through this registry (the Providers page), which writes each change through to storage.
package providers

import (
	"context"
	"errors"
	"log/slog"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// ErrUnknownProvider means no implementation is registered under that id.
var ErrUnknownProvider = errkind.New(errkind.NotFound, "unknown provider")

// Provider is what every provider implements, whatever it provides (covers, barcodes, details,
// prices).
type Provider interface {
	// Descriptor describes the provider: its id, kind, name and settings.
	Descriptor() provider.Descriptor

	// Test checks, with one cheap request to the real service, that the provider works with these
	// settings. A nil error means it does; an error says why not.
	Test(ctx context.Context, settings schema.Settings) error
}

// View is a provider's configuration together with its descriptor. Each View holds its own copy
// of the configuration: changing it changes nothing until it goes through the Registry.
type View struct {
	*provider.Provider
	Descriptor provider.Descriptor
}

// ProviderLevel reports whether err is a problem of the provider rather than of a game: a rate limit
// (busy, quota, outage) or rejected settings. Those are reported in the provider's status, not on
// game sheets, in the cover picker or as a copy's price warning.
func ProviderLevel(err error) bool {
	return errors.Is(err, provider.ErrRateLimited) || errors.Is(err, provider.ErrBadSettings)
}

// Option configures a Registry.
type Option func(*Registry)

// WithClock sets the clock that dates configurations and statuses (time.Now by default).
func WithClock(now port.Clock) Option { return func(r *Registry) { r.now = now } }

// WithLogger sets the logger (discarded by default).
func WithLogger(log *slog.Logger) Option { return func(r *Registry) { r.log = log } }
