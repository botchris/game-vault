// Package provider holds the configuration of metadata providers, Plex-agent style: for each kind
// of data (covers, barcode lookups...) the user orders the available providers, enables the ones
// they want and configures their credentials. The first enabled provider that has an answer wins;
// the next ones act as fallbacks.
package provider

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"gamevault/internal/domain/schema"
)

// ID identifies a provider implementation, e.g. "thegamesdb". It is stable and doubles as the
// configuration's identity: there is one configuration per provider.
type ID string

// Kind is the type of data a provider supplies.
type Kind string

// Values of Kind.
const (
	KindCover    Kind = "cover"
	KindBarcode  Kind = "barcode"
	KindMetadata Kind = "metadata" // game details: summary, genres, companies, trailers...
)

// ErrNotFound means no configuration exists for the provider.
var ErrNotFound = errors.New("provider not found")

// Descriptor describes a provider implementation and the settings it needs.
type Descriptor struct {
	ID             ID
	Kind           Kind
	Name           string
	DescriptionKey string // translation key: what it does and which games it applies to
	Fields         schema.Fields

	// EnabledByDefault is used the first time the provider is seen. Providers that need
	// credentials should start disabled.
	EnabledByDefault bool

	// SettingsGroup links providers of different kinds backed by the same service (e.g.
	// TheGamesDB covers and TheGamesDB details): they share their settings, so a key is entered once.
	SettingsGroup string
}

// Provider is the user's configuration of one provider.
type Provider struct {
	id        ID
	kind      Kind
	enabled   bool
	priority  int // lower runs first
	settings  schema.Settings
	updatedAt time.Time
}

// New creates the default configuration for a provider seen for the first time.
func New(d Descriptor, priority int, now time.Time) *Provider {
	return &Provider{id: d.ID, kind: d.Kind, enabled: d.EnabledByDefault, priority: priority, settings: schema.Settings{}, updatedAt: now}
}

// Rehydrate rebuilds a configuration from storage. Only repositories should call it.
func Rehydrate(id ID, kind Kind, enabled bool, priority int, settings schema.Settings, updatedAt time.Time) *Provider {
	if settings == nil {
		settings = schema.Settings{}
	}

	return &Provider{id: id, kind: kind, enabled: enabled, priority: priority, settings: settings, updatedAt: updatedAt}
}

// ID returns the provider implementation this configuration belongs to.
func (p *Provider) ID() ID { return p.id }

// Kind returns the type of data the provider supplies.
func (p *Provider) Kind() Kind { return p.kind }

// Enabled reports whether the provider takes part in its chain.
func (p *Provider) Enabled() bool { return p.enabled }

// Priority returns the position in the chain; lower runs first.
func (p *Provider) Priority() int { return p.priority }

// UpdatedAt returns when the configuration was last changed.
func (p *Provider) UpdatedAt() time.Time { return p.updatedAt }

// Settings returns a copy of the raw settings, secrets included.
func (p *Provider) Settings() schema.Settings {
	out := schema.Settings{}
	maps.Copy(out, p.settings)

	return out
}

// Configure enables or disables the provider and updates its settings. A provider can only be
// enabled when its required settings are present. Secrets sent as the placeholder are kept.
func (p *Provider) Configure(d Descriptor, enabled bool, incoming schema.Settings, now time.Time) error {
	settings := d.Fields.Merge(p.settings, incoming)
	if enabled {
		if err := d.Fields.Validate(settings, d.Name); err != nil {
			return err
		}
	}

	p.enabled, p.settings, p.updatedAt = enabled, settings, now

	return nil
}

// UpdateState keeps the state fields a provider wrote into settings during a lookup (e.g. what it
// learned about where to look first) and reports whether they changed, so the caller saves them.
// Settings the user configured are left as they are.
func (p *Provider) UpdateState(d Descriptor, settings schema.Settings, now time.Time) bool {
	next, prev := d.Fields.State(settings), d.Fields.State(p.settings)
	if maps.Equal(next, prev) {
		return false
	}

	merged := schema.Settings{}

	for k, v := range p.settings {
		if _, isState := prev[k]; !isState {
			merged[k] = v
		}
	}

	maps.Copy(merged, next)
	p.settings, p.updatedAt = merged, now

	return true
}

// ShareSettings copies settings configured on a sibling provider (same SettingsGroup).
func (p *Provider) ShareSettings(settings schema.Settings, now time.Time) {
	p.settings = schema.Settings{}
	maps.Copy(p.settings, settings)
	p.updatedAt = now
}

// Reorder assigns priorities following ids (first = highest priority). Providers not listed keep
// their relative order after the listed ones.
func Reorder(providers []*Provider, ids []ID, now time.Time) {
	rank := func(p *Provider) int {
		if i := slices.Index(ids, p.id); i >= 0 {
			return i
		}

		return len(ids) + p.priority
	}

	slices.SortStableFunc(providers, func(a, b *Provider) int { return rank(a) - rank(b) })

	for i, p := range providers {
		if p.priority != i {
			p.priority, p.updatedAt = i, now
		}
	}
}

// Sort orders providers by priority.
func Sort(providers []*Provider) {
	slices.SortStableFunc(providers, func(a, b *Provider) int { return a.priority - b.priority })
}

// Repository is the persistence port for provider configurations.
type Repository interface {
	// List returns the saved configurations of a kind of provider, in priority order.
	List(ctx context.Context, kind Kind) ([]*Provider, error)

	// Save creates or updates a provider configuration.
	Save(ctx context.Context, p *Provider) error
}
