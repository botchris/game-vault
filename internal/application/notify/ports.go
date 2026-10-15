// Package notify tells the outside world what happened in Game Vault: it keeps the configured
// connections (Telegram, ntfy, a webhook…), queues one delivery per subscribed connection when a use
// case publishes an event, and renders each event into a message in the connection's language.
package notify

import (
	"context"
	"errors"
	"time"

	"gamevault/internal/application/health"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

// Connector is the plugin piece that sends messages to one kind of service.
type Connector interface {
	// Descriptor describes the connector type and its settings.
	Descriptor() connection.TypeDescriptor

	// Send delivers m with settings. An error wrapping ErrPermanent means retrying cannot help
	// (a rejected token, an unknown chat); any other error is retried.
	Send(ctx context.Context, settings schema.Settings, m Message) error

	// Test sends a short sample with settings; nil means it arrived.
	Test(ctx context.Context, settings schema.Settings, m Message) error
}

// ChatFinder is implemented by connectors that can find where to send (Telegram: the last chat
// that wrote to the bot).
type ChatFinder interface {
	// FindChat returns the settings key to fill and its value.
	FindChat(ctx context.Context, settings schema.Settings) (key, value string, err error)
}

// Validator is implemented by connectors that can check their settings beyond the descriptor's
// rules (an address that must be an http or https URL with a host), so Save and Test refuse them
// at once instead of the first delivery failing.
type Validator interface {
	// Validate returns an Invalid error that says what to fix, or nil.
	Validate(settings schema.Settings) error
}

// Pacer is implemented by connectors that need more than a second between messages.
type Pacer interface {
	// Gap returns the time to leave between two messages of one connection.
	Gap() time.Duration
}

// Connections stores the configured connections.
type Connections interface {
	// List returns every connection by name.
	List(ctx context.Context) ([]*connection.Connection, error)

	// Get returns one, or connection.ErrNotFound.
	Get(ctx context.Context, id connection.ID) (*connection.Connection, error)

	// Save stores it.
	Save(ctx context.Context, c *connection.Connection) error

	// Delete removes it, or answers connection.ErrNotFound (its queued deliveries are deleted by
	// the service).
	Delete(ctx context.Context, id connection.ID) error
}

// Delivery is one event to send through one connection.
type Delivery struct {
	// ID identifies the delivery.
	ID string

	// Connection is where it goes.
	Connection connection.ID

	// Event is what it tells.
	Event event.Event

	// State is queued, sent, failed or dropped.
	State State

	// Attempts counts failed sends.
	Attempts int

	// NextAt is when it is due.
	NextAt time.Time

	// LastError is the last failure's reason.
	LastError string

	// CreatedAt is when it was queued.
	CreatedAt time.Time

	// UpdatedAt is the last change.
	UpdatedAt time.Time
}

// State of a delivery.
type State string

// Values of State.
const (
	StateQueued  State = "queued"
	StateSent    State = "sent"
	StateFailed  State = "failed"
	StateDropped State = "dropped"
)

// Outbox is the durable queue.
type Outbox interface {
	// Enqueue stores new deliveries.
	Enqueue(ctx context.Context, ds []Delivery) error

	// Due returns queued deliveries due at now, oldest first.
	Due(ctx context.Context, now time.Time) ([]Delivery, error)

	// MarkSent records a delivery that arrived.
	MarkSent(ctx context.Context, id string, at time.Time) error

	// MarkRetry records a failed attempt and when to try again.
	MarkRetry(ctx context.Context, id string, attempts int, next time.Time, reason string, at time.Time) error

	// MarkFailed gives a delivery up.
	MarkFailed(ctx context.Context, id string, reason string, at time.Time) error

	// MarkDropped records a delivery too old to send.
	MarkDropped(ctx context.Context, id string, at time.Time) error

	// Recent returns a connection's latest deliveries, newest first.
	Recent(ctx context.Context, id connection.ID, limit int) ([]Delivery, error)

	// DeleteFor removes a connection's deliveries.
	DeleteFor(ctx context.Context, id connection.ID) error

	// Prune removes finished deliveries older than before.
	Prune(ctx context.Context, before time.Time) error
}

// IssueMemory is the watcher's memory of notified Health issues.
type IssueMemory interface {
	// Notified returns the issues told as open, by key.
	Notified(ctx context.Context) (map[string]event.Issue, error)

	// SaveNotified replaces them.
	SaveNotified(ctx context.Context, issues map[string]event.Issue) error

	// Seeded reports whether the watcher ever ran.
	Seeded(ctx context.Context) (bool, error)

	// MarkSeeded records that it ran.
	MarkSeeded(ctx context.Context) error
}

// KeyMemory remembers the deadline thresholds told per key copy.
type KeyMemory interface {
	// KeyThresholds returns the thresholds told for each copy.
	KeyThresholds(ctx context.Context) (map[game.ID][]int, error)

	// RememberKey records a threshold told.
	RememberKey(ctx context.Context, copyID game.ID, threshold int, at time.Time) error

	// ForgetKeys drops the copies not in keep (redeemed, deleted).
	ForgetKeys(ctx context.Context, keep []game.ID) error
}

// SettingsStore keeps the notification settings.
type SettingsStore interface {
	// Address returns the Game Vault address messages link to, "" when unset.
	Address(ctx context.Context) (string, error)

	// SaveAddress stores it.
	SaveAddress(ctx context.Context, address string) error
}

// CoverFiles reads a game's cover from the local game data, never from the network.
type CoverFiles interface {
	// GetCover returns the cover of the game's edition on system; false when none is stored.
	GetCover(id game.ID, system string) (media.Image, bool, error)
}

// Catalog reads the games (for covers, keys and names).
type Catalog interface {
	// List returns every game.
	List(ctx context.Context) ([]*game.Game, error)

	// Get returns one game.
	Get(ctx context.Context, id game.ID) (*game.Game, error)
}

// Issues is Health, as the watcher reads it.
type Issues interface {
	// Issues returns the current problems.
	Issues(ctx context.Context) []health.Issue
}

// ErrPermanent marks a send failure retrying cannot fix.
var ErrPermanent = errors.New("permanent delivery failure")

// Message is what a connector sends.
type Message struct {
	// Title is a short headline.
	Title string

	// Text is the body, plain text with line breaks.
	Text string

	// Important is true for attention and security events (ntfy high priority).
	Important bool

	// Link opens the thing in Game Vault (or a deal's or a release's public page), "" without one.
	Link string

	// LinkLabel is the link's text in the connection's language ("Open in Game Vault").
	LinkLabel string

	// Images are at most two.
	Images []Image

	// Event is the event, for connectors that send data (the webhook).
	Event event.Event
}

// Image is either bytes (a local cover) or a public URL (a deal's or a release's art).
type Image struct {
	// Name is a file name.
	Name string

	// Data holds a local image.
	Data []byte

	// ContentType is Data's type.
	ContentType string

	// URL is a public image instead.
	URL string
}
