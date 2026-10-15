package home

import (
	"log/slog"

	"gamevault/internal/application/port"
	"gamevault/internal/application/unattended"
)

// Option configures a Service.
type Option func(*Service)

// WithDeals sets the deals providers compiled in (none by default).
func WithDeals(ps ...DealsProvider) Option {
	return func(s *Service) {
		for _, p := range ps {
			s.deals[p.Descriptor().ID] = p
		}
	}
}

// WithReleases sets the releases providers compiled in (none by default).
func WithReleases(ps ...ReleasesProvider) Option {
	return func(s *Service) {
		for _, p := range ps {
			s.releases[p.Descriptor().ID] = p
		}
	}
}

// WithClock sets the clock (time.Now by default).
func WithClock(now port.Clock) Option { return func(s *Service) { s.now = now } }

// WithLogger sets the logger (discarded by default).
func WithLogger(log *slog.Logger) Option { return func(s *Service) { s.log = log } }

// WithRhythm sets how the job draws and waits its pauses (random and a real timer by default);
// tests make it deterministic.
func WithRhythm(r unattended.Rhythm) Option { return func(s *Service) { s.rhythm = r } }
