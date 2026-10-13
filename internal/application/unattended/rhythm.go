// Package unattended holds the timing rule of requests nobody asked for at that moment
// (keep-alives, scheduled scans, background scans): they never follow a fixed rhythm a site could
// tell apart from a person.
package unattended

import (
	"context"
	"math/rand/v2"
	"time"
)

// Rhythm draws and waits the pauses of an unattended loop. Its zero value uses math/rand and a real
// timer; tests replace either function to make the loop deterministic.
type Rhythm struct {
	// Random returns a number in [0, 1).
	Random func() float64

	// Sleep waits for d and reports false when ctx ended first.
	Sleep func(ctx context.Context, d time.Duration) bool
}

// Vary returns d varied at random by up to ±fraction of it.
func (r Rhythm) Vary(d time.Duration, fraction float64) time.Duration {
	return time.Duration(float64(d) * (1 + fraction*(2*r.random()-1)))
}

// Between returns a random duration in [lo, hi).
func (r Rhythm) Between(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(r.random()*float64(hi-lo))
}

// Wait waits for d and reports false when ctx ended first.
func (r Rhythm) Wait(ctx context.Context, d time.Duration) bool {
	if r.Sleep != nil {
		return r.Sleep(ctx, d)
	}

	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (r Rhythm) random() float64 {
	if r.Random != nil {
		return r.Random()
	}

	return rand.Float64() // timing jitter, not a secret: math/rand is enough
}
