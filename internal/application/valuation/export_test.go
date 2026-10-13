package valuation

import (
	"context"
	"time"

	"gamevault/internal/application/unattended"
)

// SetChance replaces the random source and the sleeper, for tests.
func (s *Service) SetChance(random func() float64, sleep func(context.Context, time.Duration) error) {
	s.rhythm = unattended.Rhythm{
		Random: random,
		Sleep:  func(ctx context.Context, d time.Duration) bool { return sleep(ctx, d) == nil },
	}
}
