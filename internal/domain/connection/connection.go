// Package connection is a configured notification connector: which service, its settings, the
// events it sends and in which language.
package connection

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
)

// ID identifies a connection.
type ID string

// NewID returns a new id.
func NewID() ID { return ID(uuid.Must(uuid.NewV7()).String()) }

// Type names a connector type ("telegram", "ntfy", "webhook").
type Type string

// TypeDescriptor describes a connector type: its name and settings.
type TypeDescriptor struct {
	// Type is the type's id.
	Type Type

	// Name is shown in the UI.
	Name string

	// DescriptionKey is the i18n key of a one-line description.
	DescriptionKey string

	// Fields are the settings it needs.
	Fields schema.Fields
}

// Validate checks settings against the fields.
func (d TypeDescriptor) Validate(s schema.Settings) error { return d.Fields.Validate(s, d.Name) }

// MergeSettings merges incoming settings over stored ones (secrets sent back masked keep theirs).
func (d TypeDescriptor) MergeSettings(stored, incoming schema.Settings) schema.Settings {
	return d.Fields.Merge(stored, incoming)
}

// Masked returns the settings with secrets masked, for clients.
func (d TypeDescriptor) Masked(s schema.Settings) schema.Settings { return d.Fields.Masked(s) }

// Errors of a connection.
var (
	// ErrNotFound means no connection has the id.
	ErrNotFound = errkind.New(errkind.NotFound, "connection not found")

	// ErrUnknownType means no connector has the type.
	ErrUnknownType = errkind.New(errkind.NotFound, "unknown connector type")

	// ErrNoEvents means the connection would send nothing.
	ErrNoEvents = errkind.New(errkind.Invalid, "choose at least one event to send")

	// ErrUnknownEvent means an event kind is not in the catalog.
	ErrUnknownEvent = errkind.New(errkind.Invalid, "unknown event")

	// ErrBadLanguage means the language is not one messages are written in.
	ErrBadLanguage = errkind.New(errkind.Invalid, "messages can be written in English (en) or Spanish (es)")
)

// Languages returns the languages messages are written in.
func Languages() []string { return []string{"en", "es"} }

// Config is what the user sets.
type Config struct {
	// Name labels the connection.
	Name string

	// Enabled turns sending on.
	Enabled bool

	// Events are the kinds it sends.
	Events []event.Kind

	// Language is the messages' language ("en" or "es").
	Language string

	// Settings are the connector's settings.
	Settings schema.Settings
}

// Snapshot is a connection's whole state, for storage.
type Snapshot struct {
	// ID identifies the connection.
	ID ID

	// Type is its connector type.
	Type Type

	// Name labels it.
	Name string

	// Enabled says whether sending is on.
	Enabled bool

	// Events are the kinds it sends.
	Events []event.Kind

	// Language is the messages' language.
	Language string

	// Settings are the connector's settings, secrets included.
	Settings schema.Settings

	// PausedWhy says why the dispatcher paused it, "" when running.
	PausedWhy string

	// PausedAt is when it was paused.
	PausedAt time.Time

	// CreatedAt is when it was created.
	CreatedAt time.Time

	// UpdatedAt is when it was last configured.
	UpdatedAt time.Time
}

// Connection is a configured connector.
type Connection struct {
	id        ID
	typ       Type
	name      string
	enabled   bool
	events    []event.Kind
	language  string
	settings  schema.Settings
	pausedWhy string
	pausedAt  time.Time
	createdAt time.Time
	updatedAt time.Time
}

// New returns a connection of type d.
func New(d TypeDescriptor, cfg Config, now time.Time) (*Connection, error) {
	c := &Connection{
		id:        NewID(),
		typ:       d.Type,
		createdAt: now,
	}

	if err := c.Reconfigure(d, cfg, now); err != nil {
		return nil, err
	}

	return c, nil
}

// Rehydrate rebuilds a connection from its stored state, without validating it.
func Rehydrate(s Snapshot) *Connection {
	return &Connection{
		id:        s.ID,
		typ:       s.Type,
		name:      s.Name,
		enabled:   s.Enabled,
		events:    slices.Clone(s.Events),
		language:  s.Language,
		settings:  maps.Clone(s.Settings),
		pausedWhy: s.PausedWhy,
		pausedAt:  s.PausedAt,
		createdAt: s.CreatedAt,
		updatedAt: s.UpdatedAt,
	}
}

// Snapshot returns the connection's whole state, for storage.
func (c *Connection) Snapshot() Snapshot {
	return Snapshot{
		ID:        c.id,
		Type:      c.typ,
		Name:      c.name,
		Enabled:   c.enabled,
		Events:    slices.Clone(c.events),
		Language:  c.language,
		Settings:  maps.Clone(c.settings),
		PausedWhy: c.pausedWhy,
		PausedAt:  c.pausedAt,
		CreatedAt: c.createdAt,
		UpdatedAt: c.updatedAt,
	}
}

// Reconfigure replaces the configuration (settings merged, secrets kept) and unpauses it: the user
// fixed what paused it, or will be told again.
func (c *Connection) Reconfigure(d TypeDescriptor, cfg Config, now time.Time) error {
	if len(cfg.Events) == 0 {
		return ErrNoEvents
	}

	for _, k := range cfg.Events {
		if !event.Known(k) {
			return errkind.Wrap(errkind.Invalid, fmt.Errorf("%w: %q", ErrUnknownEvent, k))
		}
	}

	if !slices.Contains(Languages(), cfg.Language) {
		return ErrBadLanguage
	}

	settings := d.MergeSettings(c.settings, cfg.Settings)
	if err := d.Validate(settings); err != nil {
		return err
	}

	c.name = strings.TrimSpace(cmp.Or(strings.TrimSpace(cfg.Name), d.Name))
	c.enabled = cfg.Enabled
	c.events = slices.Clone(cfg.Events)
	c.language = cfg.Language
	c.settings = settings
	c.Unpause()
	c.updatedAt = now

	return nil
}

// Pause stops the connection from sending until it is reconfigured, and records why.
func (c *Connection) Pause(why string, at time.Time) {
	c.pausedWhy = why
	c.pausedAt = at
}

// Unpause lets the connection send again.
func (c *Connection) Unpause() {
	c.pausedWhy = ""
	c.pausedAt = time.Time{}
}

// Subscribed reports whether the connection is enabled and chose kind k. It ignores the pause: the
// service checks that separately, so a paused connection can still be told apart from a muted one.
func (c *Connection) Subscribed(k event.Kind) bool {
	return c.enabled && slices.Contains(c.events, k)
}

// ID returns the connection's id.
func (c *Connection) ID() ID { return c.id }

// Type returns the connector type.
func (c *Connection) Type() Type { return c.typ }

// Name returns the connection's label.
func (c *Connection) Name() string { return c.name }

// Enabled reports whether sending is turned on.
func (c *Connection) Enabled() bool { return c.enabled }

// Events returns a copy of the kinds the connection sends.
func (c *Connection) Events() []event.Kind { return slices.Clone(c.events) }

// Language returns the messages' language.
func (c *Connection) Language() string { return c.language }

// Settings returns a copy of the connector's settings, secrets included.
func (c *Connection) Settings() schema.Settings { return maps.Clone(c.settings) }

// Paused reports whether the connection was paused and not yet reconfigured.
func (c *Connection) Paused() bool { return c.pausedWhy != "" }

// PausedWhy returns why the connection was paused, "" when it is not.
func (c *Connection) PausedWhy() string { return c.pausedWhy }

// PausedAt returns when the connection was paused.
func (c *Connection) PausedAt() time.Time { return c.pausedAt }

// CreatedAt returns when the connection was created.
func (c *Connection) CreatedAt() time.Time { return c.createdAt }

// UpdatedAt returns when the connection was last configured.
func (c *Connection) UpdatedAt() time.Time { return c.updatedAt }
