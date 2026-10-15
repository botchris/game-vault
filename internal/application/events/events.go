// Package events is the port use cases publish what happened through. It is a leaf (it imports only
// the event domain) so that any use case can take a Publisher without importing the notification
// service, which depends on media and, through it, on the catalog.
package events

import (
	"context"

	"gamevault/internal/domain/event"
)

// Publisher is what use cases publish events through.
type Publisher interface {
	// Publish queues e for the connections subscribed to its kind. It never fails the caller: an
	// event that cannot be queued is logged by the publisher, since the change it reports is already
	// saved.
	Publish(ctx context.Context, e event.Event)
}

// Nop is a Publisher that drops every event: the default of every use case's option.
type Nop struct{}

// Publish implements Publisher.
func (Nop) Publish(context.Context, event.Event) {}

var _ Publisher = Nop{}
