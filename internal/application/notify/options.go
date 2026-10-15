package notify

import (
	"log/slog"
	"time"

	"gamevault/internal/application/port"
)

// Option configures a Service.
type Option func(*Service)

// WithConnectors sets the connector types compiled in (none by default), in the order the UI lists
// them.
func WithConnectors(cs ...Connector) Option {
	return func(s *Service) {
		for _, c := range cs {
			typ := c.Descriptor().Type
			if _, seen := s.connectors[typ]; !seen {
				s.order = append(s.order, typ)
			}

			s.connectors[typ] = c
		}
	}
}

// WithClock sets the clock (time.Now by default).
func WithClock(now port.Clock) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger (discarded by default).
func WithLogger(log *slog.Logger) Option { return func(s *Service) { s.log = log } }

// WithSending turns queueing on. It is off by default, so nothing leaves a server unless main turns
// it on: a test server on a copy of the config holds the user's real tokens and must stay silent.
// Test sends whatever this says, since the user pressed it.
func WithSending(on bool) Option { return func(s *Service) { s.sending = on } }

// WithBatchWindow sets how long games added by hand wait for another before they are told in one
// message (10 minutes by default).
func WithBatchWindow(d time.Duration) Option { return func(s *Service) { s.window = d } }
