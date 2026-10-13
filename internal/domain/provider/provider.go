// Package provider holds the configuration of metadata providers, Plex-agent style: for each kind
// of data (covers, barcode lookups...) the user orders the available providers, enables the ones
// they want and configures their credentials. The first enabled provider that has an answer wins;
// the next ones act as fallbacks.
package provider

import (
	"context"
	"maps"
	"slices"
	"time"

	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/schema"
)

// ID identifies a provider implementation, e.g. "thegamesdb". It is stable and doubles as the
// configuration's identity: there is one configuration per provider.
type ID string

// Kind is the type of data a provider supplies.
type Kind string

// Values of Kind.
const (
	KindCover     Kind = "cover"
	KindBarcode   Kind = "barcode"
	KindMetadata  Kind = "metadata"  // game details: summary, genres, companies, trailers...
	KindValuation Kind = "valuation" // second-hand price estimates of physical copies
)

// Kinds returns every kind of provider, in the order the Providers page lists them. A new kind is
// added here and to the plugins that bring providers of it; the configuration registry ranges over
// this list.
func Kinds() []Kind { return []Kind{KindCover, KindBarcode, KindMetadata, KindValuation} }

// ErrNotFound means no configuration exists for the provider.
var ErrNotFound = errkind.New(errkind.NotFound, "provider not found")

// ErrRateLimited means the service asked to slow down (HTTP 429). It is transient: the answer says
// nothing about the game, so callers must not remember it as "nothing found" and ask again later.
var ErrRateLimited = errkind.New(errkind.Unavailable, "the service is receiving too many requests: try again in a few minutes")

// ErrQuotaExhausted means the service's request allowance (a monthly quota) is used up until it
// renews. It is a kind of ErrRateLimited, so errors.Is matches both: like a rate limit, the answer
// says nothing about the game and nothing is remembered from it; it only lasts longer. Providers
// wrap it in an error that says when the allowance renews.
var ErrQuotaExhausted error = quotaExhausted{}

// quotaExhausted is ErrQuotaExhausted's type: its own message, matching ErrRateLimited too.
type quotaExhausted struct{}

func (quotaExhausted) Error() string {
	return "the service has used its request allowance: it answers again when the allowance renews"
}

// Is makes errors.Is(ErrQuotaExhausted, ErrRateLimited) true without ErrRateLimited's message.
func (quotaExhausted) Is(target error) bool { return target == ErrRateLimited }

// Kind returns ErrRateLimited's kind.
func (quotaExhausted) Kind() errkind.Kind { return errkind.Unavailable }

// ErrUnavailable means the service is not answering (a 5xx, no connection). It is a kind of
// ErrRateLimited, so errors.Is matches both: like a rate limit, the answer says nothing about the
// game and nothing is remembered from it.
var ErrUnavailable error = unavailable{}

// unavailable is ErrUnavailable's type: its own message, matching ErrRateLimited too.
type unavailable struct{}

func (unavailable) Error() string { return "the service is not answering: try again later" }

// Is makes errors.Is(ErrUnavailable, ErrRateLimited) true without ErrRateLimited's message.
func (unavailable) Is(target error) bool { return target == ErrRateLimited }

// Kind returns ErrRateLimited's kind.
func (unavailable) Kind() errkind.Kind { return errkind.Unavailable }

// ErrBadSettings means the service rejects the provider's settings (an unknown key, wrong
// credentials): a problem of the provider, not of a game. Providers return it through BadSettings,
// with a message that says what to fix.
var ErrBadSettings = errkind.New(errkind.Precondition, "the service rejects the provider's settings: check them in the provider settings")

// BadSettings returns an error with msg that matches ErrBadSettings.
func BadSettings(msg string) error { return badSettings(msg) }

// badSettings is BadSettings' type.
type badSettings string

func (e badSettings) Error() string { return string(e) }

// Is makes errors.Is(e, ErrBadSettings) true.
func (badSettings) Is(target error) bool { return target == ErrBadSettings }

// Kind returns ErrBadSettings' kind.
func (badSettings) Kind() errkind.Kind { return errkind.Precondition }

// Resumer is implemented by errors that know when the service answers again (a monthly allowance
// that renews at a known time).
type Resumer interface {
	// ResumesAt returns when the service answers again.
	ResumesAt() time.Time
}

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

	// DefaultOrder is the provider's place in its chain the first time it is seen (lower runs
	// first; equal values keep the plugins' order). Afterwards the user's order is used.
	DefaultOrder int
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
	return &Provider{
		id:        d.ID,
		kind:      d.Kind,
		enabled:   d.EnabledByDefault,
		priority:  priority,
		settings:  schema.Settings{},
		updatedAt: now,
	}
}

// Rehydrate rebuilds a configuration from storage. Only repositories should call it.
func Rehydrate(id ID, kind Kind, enabled bool, priority int, settings schema.Settings, updatedAt time.Time) *Provider {
	if settings == nil {
		settings = schema.Settings{}
	}

	return &Provider{
		id:        id,
		kind:      kind,
		enabled:   enabled,
		priority:  priority,
		settings:  settings,
		updatedAt: updatedAt,
	}
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
	merged, changed := d.Fields.MergeState(p.settings, settings)
	if changed {
		p.settings, p.updatedAt = merged, now
	}

	return changed
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
