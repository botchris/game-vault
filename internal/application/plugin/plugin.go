// Package plugin groups what each integration adds to Game Vault. A plugin is one external
// system (a store, a game database, a barcode service) and brings all of its pieces at once: the
// source that imports its library, and the cover, details and barcode providers it offers. The
// pieces stay independent at run time (a store's cover provider works without an account of that
// store, through the game's link to it); the plugin only says they come together.
//
// Plugins are compiled in: cmd/gamevault lists them, and the registry hands each piece to the
// service that runs it.
package plugin

import (
	"errors"
	"fmt"

	"gamevault/internal/application/media"
	"gamevault/internal/application/sync"
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
}

// Registry holds the plugins compiled into Game Vault.
type Registry struct {
	plugins []Plugin
}

// ErrDuplicate means two plugins, two source types or two providers share an id.
var ErrDuplicate = errors.New("duplicate plugin piece")

// NewRegistry checks that every plugin, source type and provider id is unique: two pieces under one
// id would silently replace each other in the services.
func NewRegistry(plugins ...Plugin) (*Registry, error) {
	var (
		ids       = map[string]bool{}
		types     = map[source.Type]string{}
		providers = map[provider.ID]string{}
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
	}

	return &Registry{plugins: plugins}, nil
}

func (p Plugin) mediaDescriptors() []provider.Descriptor {
	out := make([]provider.Descriptor, 0, len(p.Covers)+len(p.Metadata)+len(p.Barcodes))
	for _, c := range p.Covers {
		out = append(out, c.Descriptor())
	}

	for _, m := range p.Metadata {
		out = append(out, m.Descriptor())
	}

	for _, b := range p.Barcodes {
		out = append(out, b.Descriptor())
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

// Media returns every plugin's media providers, for the media service.
func (r *Registry) Media() media.Providers {
	var out media.Providers
	for _, p := range r.plugins {
		out.Covers = append(out.Covers, p.Covers...)
		out.Metadata = append(out.Metadata, p.Metadata...)
		out.Barcodes = append(out.Barcodes, p.Barcodes...)
	}

	return out
}
