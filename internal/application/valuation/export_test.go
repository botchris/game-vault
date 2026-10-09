package valuation

import (
	"context"
	"time"
)

// SetChance replaces the random source and the sleeper, for tests.
func (s *Service) SetChance(random func() float64, sleep func(context.Context, time.Duration) error) {
	s.random, s.sleep = random, sleep
}
