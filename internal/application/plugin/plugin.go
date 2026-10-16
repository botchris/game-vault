// Package plugin groups what each integration adds to Game Vault. A plugin is one external
// system (a store, a game database, a barcode service, a messaging service) and brings all of its
// pieces at once: the source that imports its library, the cover, details, barcode and price
// providers it offers, and the notification connectors that send to it. The
// pieces stay independent at run time (a store's cover provider works without an account of that
// store, through the game's link to it); the plugin only says they come together.
//
// Plugins are compiled in: cmd/gamevault lists them, and the registry hands each piece to the
// service that runs it.
package plugin

import (
	"errors"
	"fmt"

	"gamevault/internal/application/home"
	"gamevault/internal/application/media"
	"gamevault/internal/application/notify"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/source"
)

// Plugin is everything one integration adds. A dependency on another plugin's piece (Battle.net's
// covers use the Steam store) is a parameter of the plugin's constructor, so it is explicit and
// checked by the compiler.
type Plugin struct {
	// ID names the plugin, usually its package ("steam").
	ID string

	// Name is the integration's display name.
	Name string

	// Sources import the libraries of accounts (one source type each).
	Sources []sync.Provider

	// Covers, Metadata and Barcodes are the media providers. Each declares its default place in
	// its chain (provider.Descriptor.DefaultOrder).
	Covers   []media.CoverProvider
	Metadata []media.MetadataProvider
	Barcodes []media.BarcodeProvider

	// Valuations estimate second-hand prices of physical copies.
	Valuations []valuation.Provider

	// Deals and Releases feed the home page: current deals and upcoming games.
	Deals    []home.DealsProvider
	Releases []home.ReleasesProvider

	// Connectors send notifications to a service (one connector type each). They are configured
	// as connections, not as providers.
	Connectors []notify.Connector
}

// Registry holds the plugins compiled into Game Vault.
type Registry struct {
	plugins []Plugin
}

// ErrDuplicate means two plugins, two source types, two providers or two connector types share an
// id.
var ErrDuplicate = errors.New("duplicate plugin piece")

// NewRegistry checks that every plugin, source type, provider id and connector type is unique: two
// pieces under one id would silently replace each other in the services.
func NewRegistry(plugins ...Plugin) (*Registry, error) {
	var (
		ids        = map[string]bool{}
		types      = map[source.Type]string{}
		providers  = map[provider.ID]string{}
		connectors = map[connection.Type]string{}
	)

	for _, p := range plugins {
		if p.ID == "" || ids[p.ID] {
			return nil, fmt.Errorf("%w: plugin %q", ErrDuplicate, p.ID)
		}

		ids[p.ID] = true

		for _, s := range p.Sources {
			t := s.Descriptor().Type
			if other, ok := types[t]; ok {
				return nil, fmt.Errorf("%w: source type %q in plugins %q and %q", ErrDuplicate, t, other, p.ID)
			}

			types[t] = p.ID
		}

		for _, d := range p.mediaDescriptors() {
			if other, ok := providers[d.ID]; ok {
				return nil, fmt.Errorf("%w: provider %q in plugins %q and %q", ErrDuplicate, d.ID, other, p.ID)
			}

			providers[d.ID] = p.ID
		}

		for _, c := range p.Connectors {
			t := c.Descriptor().Type
			if other, ok := connectors[t]; ok {
				return nil, fmt.Errorf("%w: connector type %q in plugins %q and %q", ErrDuplicate, t, other, p.ID)
			}

			connectors[t] = p.ID
		}
	}

	return &Registry{plugins: plugins}, nil
}

func (p Plugin) mediaDescriptors() []provider.Descriptor {
	var out []provider.Descriptor

	for _, kind := range provider.Kinds() {
		for _, pr := range p.providersOf(kind) {
			out = append(out, pr.Descriptor())
		}
	}

	return out
}

// providersOf returns the plugin's providers of a kind. A new kind of provider is a new field of
// Plugin and a case here.
func (p Plugin) providersOf(kind provider.Kind) []providers.Impl {
	switch kind {
	case provider.KindCover:
		return asProviders(p.Covers)
	case provider.KindBarcode:
		return asProviders(p.Barcodes)
	case provider.KindMetadata:
		return asProviders(p.Metadata)
	case provider.KindValuation:
		return asProviders(p.Valuations)
	case provider.KindDeals:
		return asProviders(p.Deals)
	case provider.KindReleases:
		return asProviders(p.Releases)
	default:
		return nil
	}
}

func asProviders[T providers.Impl](in []T) []providers.Impl {
	out := make([]providers.Impl, len(in))
	for i, p := range in {
		out[i] = p
	}

	return out
}

// Plugins returns the registered plugins, in registration order.
func (r *Registry) Plugins() []Plugin { return append([]Plugin(nil), r.plugins...) }

// Sources returns every plugin's source providers, for the sync service.
func (r *Registry) Sources() []sync.Provider {
	var out []sync.Provider
	for _, p := range r.plugins {
		out = append(out, p.Sources...)
	}

	return out
}

// Providers returns every plugin's providers (covers, barcodes, metadata, prices, deals, releases), kind by kind in
// provider.Kinds' order and, within a kind, in registration order: the order that breaks ties
// between equal DefaultOrders in the providers registry.
func (r *Registry) Providers() []providers.Impl {
	n := 0
	for _, p := range r.plugins {
		n += len(p.mediaDescriptors())
	}

	out := make([]providers.Impl, 0, n)

	for _, kind := range provider.Kinds() {
		for _, p := range r.plugins {
			out = append(out, p.providersOf(kind)...)
		}
	}

	return out
}

// Valuations returns every plugin's price providers, for the valuation service.
func (r *Registry) Valuations() []valuation.Provider {
	var out []valuation.Provider
	for _, p := range r.plugins {
		out = append(out, p.Valuations...)
	}

	return out
}

// Deals returns every plugin's deals providers, for the home service.
func (r *Registry) Deals() []home.DealsProvider {
	var out []home.DealsProvider
	for _, p := range r.plugins {
		out = append(out, p.Deals...)
	}

	return out
}

// Releases returns every plugin's releases providers, for the home service.
func (r *Registry) Releases() []home.ReleasesProvider {
	var out []home.ReleasesProvider
	for _, p := range r.plugins {
		out = append(out, p.Releases...)
	}

	return out
}

// Connectors returns every plugin's notification connectors, for the notification service, in
// registration order (the order the UI lists them in).
func (r *Registry) Connectors() []notify.Connector {
	var out []notify.Connector
	for _, p := range r.plugins {
		out = append(out, p.Connectors...)
	}

	return out
}
