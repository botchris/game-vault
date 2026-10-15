package notify

import (
	"context"
	"errors"
	"maps"
	"time"

	"gamevault/internal/domain/connection"
)

// retryAfter is the wait after each failed attempt; after the last, the delivery is given up.
var retryAfter = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

const (
	// tick is how often the dispatcher looks at the queue without being woken.
	tick = 15 * time.Second

	// maxAge is how old a delivery may be when its turn comes: news a day late is no news.
	maxAge = 24 * time.Hour

	// keepFinished is how long sent, failed and dropped deliveries are listed.
	keepFinished = 7 * 24 * time.Hour

	// defaultGap is the time between two messages of a connection whose connector has no Pacer.
	defaultGap = time.Second

	// sendTimeout bounds one send.
	sendTimeout = 30 * time.Second
)

// retryAfterer is implemented by errors that say how long the service asked to wait (a 429's
// Retry-After, see apiclient.StatusError).
type retryAfterer interface {
	// RetryAfter returns how long the service asked to wait; zero when it did not say.
	RetryAfter() time.Duration
}

// Run sends due deliveries until ctx ends: at once when woken by a new delivery, and every tick.
// Each look also queues the games added by hand whose batch window has passed.
func (s *Service) Run(ctx context.Context) {
	for {
		s.FlushBatches(ctx)
		s.round(ctx)

		if !s.sleep(ctx) {
			return
		}
	}
}

// sleep waits for a tick or a wake-up and reports false when ctx ended first.
func (s *Service) sleep(ctx context.Context) bool {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	ticked := make(chan bool, 1)

	go func() { ticked <- s.rhythm.Wait(waitCtx, tick) }()

	select {
	case ok := <-ticked:
		return ok
	case <-s.wakeup:
		// Stop the tick's wait before looking again, so no waiting goroutine outlives this call.
		cancel()
		<-ticked

		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}

// round sends every due delivery, connection by connection in the order they were queued, then
// prunes the finished ones nobody will list any more.
func (s *Service) round(ctx context.Context) {
	due, err := s.outbox.Due(ctx, s.now())
	if err != nil {
		s.log.Warn("reading the notification queue", "error", err)

		return
	}

	var order []connection.ID

	byConn := map[connection.ID][]Delivery{}

	for _, d := range due {
		if _, seen := byConn[d.Connection]; !seen {
			order = append(order, d.Connection)
		}

		byConn[d.Connection] = append(byConn[d.Connection], d)
	}

	address := ""
	if len(due) > 0 {
		if address, err = s.settings.Address(ctx); err != nil {
			s.log.Warn("reading the notification address", "error", err)
		}
	}

	for _, id := range order {
		if ctx.Err() != nil {
			return
		}

		s.sendAll(ctx, id, byConn[id], address)
	}

	if err := s.outbox.Prune(ctx, s.now().Add(-keepFinished)); err != nil {
		s.log.Warn("pruning the notification queue", "error", err)
	}
}

// sendAll sends one connection's due deliveries, in order and at its pace. A permanent failure
// pauses the connection and leaves the rest queued for when the user fixes it.
func (s *Service) sendAll(ctx context.Context, id connection.ID, ds []Delivery, address string) {
	c, err := s.conns.Get(ctx, id)
	if errors.Is(err, connection.ErrNotFound) {
		// Deleted after they were queued: nobody is left to send them to.
		s.dropAll(ctx, ds)

		return
	}

	if err != nil {
		s.log.Warn("reading a connection", "error", err)

		return
	}

	if c.Paused() {
		return // they wait for the user to fix it; old ones are dropped when it sends again
	}

	if !c.Enabled() {
		// Turned off after they were queued: the user no longer wants them.
		s.dropAll(ctx, ds)

		return
	}

	conn, err := s.connector(c.Type())
	if err != nil {
		for _, d := range ds {
			s.mark(s.outbox.MarkFailed(ctx, d.ID, err.Error(), s.now()))
		}

		return
	}

	gap := defaultGap
	if p, ok := conn.(Pacer); ok {
		gap = p.Gap()
	}

	for _, d := range ds {
		if s.now().Sub(d.CreatedAt) > maxAge {
			s.mark(s.outbox.MarkDropped(ctx, d.ID, s.now()))

			continue
		}

		if !s.pace(ctx, id, gap) {
			return
		}

		if stop := s.send(ctx, c, conn, d, address); stop {
			return
		}
	}
}

// pace waits until gap has passed since connection id last sent; false when ctx ended first.
func (s *Service) pace(ctx context.Context, id connection.ID, gap time.Duration) bool {
	last, ok := s.lastSend[id]
	if !ok {
		return true
	}

	wait := gap - s.now().Sub(last)
	if wait <= 0 {
		return true
	}

	return s.rhythm.Wait(ctx, wait)
}

// send tries one delivery and records the outcome. It reports whether the connection's round must
// stop: its failure was permanent (the connection is paused), or the server is stopping.
func (s *Service) send(ctx context.Context, c *connection.Connection, conn Connector, d Delivery, address string) (stop bool) {
	m := Render(ctx, d.Event, c.Language(), address, s.catalog, s.covers)

	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	err := conn.Send(sendCtx, c.Settings(), m)

	cancel()

	now := s.now()
	s.lastSend[c.ID()] = now

	// The outcome is recorded even when the server is stopping: a message that arrived must not be
	// sent again after the restart.
	record := context.WithoutCancel(ctx)

	if err == nil {
		s.mark(s.outbox.MarkSent(record, d.ID, now))

		return false
	}

	if ctx.Err() != nil {
		// Stopping is not the service's failure: the delivery stays as it was, tried again after
		// the restart.
		return true
	}

	// The connector's error never holds its settings (connectors keep tokens out of them), and the
	// log line names the connection, never its settings.
	s.log.Warn("sending a notification", "connection", c.Name(), "event", d.Event.Kind, "error", err)

	if errors.Is(err, ErrPermanent) {
		s.mark(s.outbox.MarkFailed(record, d.ID, err.Error(), now))
		s.pause(record, c, err.Error(), now)

		return true
	}

	attempt := d.Attempts + 1
	if attempt > len(retryAfter) {
		s.mark(s.outbox.MarkFailed(record, d.ID, err.Error(), now))

		return false
	}

	s.mark(s.outbox.MarkRetry(record, d.ID, attempt, now.Add(retryWait(err, attempt)), err.Error(), now))

	return false
}

// pause stops connection sent from sending, sent being the copy whose settings failed. The
// connection is read again first: the user may have fixed or deleted it while the message was on its
// way, and saving the old copy would undo that.
func (s *Service) pause(ctx context.Context, sent *connection.Connection, why string, at time.Time) {
	c, err := s.conns.Get(ctx, sent.ID())
	if errors.Is(err, connection.ErrNotFound) {
		return
	}

	if err != nil {
		s.log.Warn("pausing a connection", "connection", sent.Name(), "error", err)

		return
	}

	if !maps.Equal(c.Settings(), sent.Settings()) {
		return // new settings, not yet tried: they get their own chance
	}

	c.Pause(why, at)

	if err := s.conns.Save(ctx, c); err != nil {
		s.log.Warn("pausing a connection", "connection", c.Name(), "error", err)
	}
}

// retryWait is how long to wait before attempt+1: the schedule's wait, or longer when the service
// asked for it, up to the schedule's longest.
func retryWait(err error, attempt int) time.Duration {
	wait := retryAfter[attempt-1]

	var ra retryAfterer
	if errors.As(err, &ra) && ra.RetryAfter() > wait {
		wait = min(ra.RetryAfter(), retryAfter[len(retryAfter)-1])
	}

	return wait
}

// dropAll marks deliveries nobody will receive.
func (s *Service) dropAll(ctx context.Context, ds []Delivery) {
	for _, d := range ds {
		s.mark(s.outbox.MarkDropped(ctx, d.ID, s.now()))
	}
}

// mark logs a failure to record a delivery's outcome: the next round sees it again.
func (s *Service) mark(err error) {
	if err != nil {
		s.log.Warn("recording a notification's outcome", "error", err)
	}
}
