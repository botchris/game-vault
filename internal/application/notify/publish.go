package notify

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/event"
)

// Publish implements events.Publisher: it queues one delivery per enabled, unpaused connection
// subscribed to e's kind. Games added by hand wait in a batch (see FlushBatches). A failure is
// logged: an event that cannot be queued is not worth failing the use case that reported it.
func (s *Service) Publish(ctx context.Context, e event.Event) {
	if !s.sending {
		return
	}

	if added, ok := e.Data.(event.CollectionAdded); ok && added.Origin == event.OriginManual {
		s.batch(added)

		return
	}

	if err := s.enqueue(ctx, e); err != nil {
		s.log.Warn("queueing a notification", "event", e.Kind, "error", err)
	}
}

// FlushBatches queues the games added by hand once the batch window has passed without another.
// The dispatcher calls it on every tick.
func (s *Service) FlushBatches(ctx context.Context) {
	s.mu.Lock()

	if s.pending == nil || s.now().Sub(s.lastAt) < s.window {
		s.mu.Unlock()

		return
	}

	added, at := *s.pending, s.lastAt
	s.pending = nil
	s.mu.Unlock()

	err := s.enqueue(ctx, event.New(event.KindCollectionAdded, at, added))

	s.mu.Lock()
	first := err != nil && !s.flushFailing
	s.flushFailing = err != nil
	s.mu.Unlock()

	if err != nil {
		// The batch is tried again on every tick: one line per failure streak, not one a tick.
		if first {
			s.log.Warn("queueing the games added by hand", "error", err)
		}

		s.unbatch(added)
	}
}

// batch adds games added by hand to the pending batch and restarts its window.
func (s *Service) batch(added event.CollectionAdded) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending == nil {
		s.pending = &event.CollectionAdded{
			Origin: event.OriginManual,
			Games:  []event.GameRef{},
		}
	}

	s.pending.Games = append(s.pending.Games, added.Games...)
	s.pending.CopiesAdded += added.CopiesAdded
	s.lastAt = s.now()
}

// unbatch puts back a batch that could not be queued, ahead of what arrived since, for the next
// flush to try again.
func (s *Service) unbatch(added event.CollectionAdded) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pending == nil {
		s.pending = &added

		return
	}

	s.pending.Games = append(slices.Clone(added.Games), s.pending.Games...)
	s.pending.CopiesAdded += added.CopiesAdded
}

func (s *Service) enqueue(ctx context.Context, e event.Event) error {
	conns, err := s.conns.List(ctx)
	if err != nil {
		return err
	}

	now := s.now()
	ds := make([]Delivery, 0, len(conns))

	for _, c := range conns {
		if !c.Subscribed(e.Kind) || c.Paused() || ownIssue(e, c.ID()) {
			continue
		}

		ds = append(ds, Delivery{
			ID:         uuid.Must(uuid.NewV7()).String(),
			Connection: c.ID(),
			Event:      e,
			State:      StateQueued,
			NextAt:     now,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	if len(ds) == 0 {
		return nil
	}

	if err := s.outbox.Enqueue(ctx, ds); err != nil {
		return err
	}

	s.wake()

	return nil
}

// wake tells the dispatcher there is work, without waiting: one pending signal is enough, since
// the dispatcher reads every due delivery when it wakes.
func (s *Service) wake() {
	select {
	case s.wakeup <- struct{}{}:
	default:
	}
}

// ownIssue reports whether e is the problem of connection id itself: a connection that cannot
// send would only queue its own alarm forever.
func ownIssue(e event.Event, id connection.ID) bool {
	is, ok := e.Data.(event.Issue)

	return ok && is.Key == ConnectionIssueKey(id)
}

// Wants implements Interest: it reports whether sending is on and at least one enabled, unpaused
// connection subscribes to kind. A failure to read the connections counts as no: the caller then
// keeps nothing as told, and asks again next time.
func (s *Service) Wants(ctx context.Context, kind event.Kind) bool {
	if !s.sending {
		return false
	}

	conns, err := s.conns.List(ctx)
	if err != nil {
		s.log.Warn("reading the connections", "error", err)

		return false
	}

	return slices.ContainsFunc(conns, func(c *connection.Connection) bool { return c.Subscribed(kind) && !c.Paused() })
}
