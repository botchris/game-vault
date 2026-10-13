package sync

import (
	"context"
	"time"

	"gamevault/internal/application/unattended"
	"gamevault/internal/domain/source"
)

// KeepAliveDelay returns a keep-alive interval d varied the way RunKeepAlive varies it, for tests.
func (s *Service) KeepAliveDelay(d time.Duration) time.Duration { return s.jittered(d) }

// ScanDelay returns a scan interval d varied the way RunScheduler varies it, for tests.
func (s *Service) ScanDelay(d time.Duration) time.Duration { return s.rhythm.Vary(d, scanJitter) }

// SetRhythm replaces the random draws, for tests.
func (s *Service) SetRhythm(r unattended.Rhythm) { s.rhythm = r }

// KeepAliveNow runs one keep-alive of the source at once, for tests.
func (s *Service) KeepAliveNow(ctx context.Context, id source.ID) {
	src, err := s.sources.Get(ctx, id)
	if err != nil {
		return
	}

	if k, ok := s.providers[src.Type()].(KeepAliver); ok {
		s.keepAlive(ctx, id, k)
	}
}
