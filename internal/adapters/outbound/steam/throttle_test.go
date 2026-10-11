package steam

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
)

// fakeClock is a clock whose sleeps advance it at once.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time

	// slept records every wait.
	slept []time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)

	return nil
}

func TestThrottle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN Steam's pace: a burst of 3, then one request every 1.5 s, waiting at most 4 s", func(t *testing.T) {
		clock := &fakeClock{now: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
		th := newThrottle(1500*time.Millisecond, 3, 4*time.Second, clock.Now, clock.Sleep)

		var sent int

		send := func() error { return th.do(ctx, func() error { sent++; return nil }) }

		t.Run("WHEN three requests come at once", func(t *testing.T) {
			for range 3 {
				require.NoError(t, send())
			}

			t.Run("THEN none waits", func(t *testing.T) {
				assert.Empty(t, clock.slept)
			})
		})

		t.Run("WHEN two more follow", func(t *testing.T) {
			require.NoError(t, send())
			require.NoError(t, send())

			t.Run("THEN each waits for the pace", func(t *testing.T) {
				assert.Equal(t, []time.Duration{1500 * time.Millisecond, 1500 * time.Millisecond}, clock.slept)
			})
		})
	})

	t.Run("GIVEN a throttle whose queue already reaches the longest wait", func(t *testing.T) {
		clock := &fakeClock{now: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
		th := newThrottle(1500*time.Millisecond, 1, 2*time.Second, clock.Now, func(context.Context, time.Duration) error { return nil })

		// Two requests take the turns at 0 s and 1.5 s without the clock moving: the next is at 3 s.
		for range 2 {
			require.NoError(t, th.do(ctx, func() error { return nil }))
		}

		t.Run("WHEN another one comes", func(t *testing.T) {
			called := false
			err := th.do(ctx, func() error { called = true; return nil })

			t.Run("THEN it fails fast as a rate limit, without being sent", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.False(t, called)
			})
		})
	})

	t.Run("GIVEN a request holding the turn for longer than the longest wait", func(t *testing.T) {
		th := newThrottle(0, 10, 50*time.Millisecond, time.Now, func(context.Context, time.Duration) error { return nil })
		release := make(chan struct{})
		started := make(chan struct{})

		go func() {
			_ = th.do(ctx, func() error {
				close(started)
				<-release

				return nil
			})
		}()

		<-started
		t.Cleanup(func() { close(release) })

		t.Run("WHEN another one comes", func(t *testing.T) {
			begin := time.Now()
			err := th.do(ctx, func() error { return nil })

			t.Run("THEN it fails fast as a rate limit instead of blocking until its context ends", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Less(t, time.Since(begin), time.Second)
			})
		})
	})

	t.Run("GIVEN slow requests sent concurrently", func(t *testing.T) {
		th := newThrottle(0, 10, time.Second, time.Now, func(context.Context, time.Duration) error { return nil })

		var open, most atomic.Int32

		var wg sync.WaitGroup

		for range 5 {
			wg.Go(func() {
				_ = th.do(ctx, func() error {
					if n := open.Add(1); n > most.Load() {
						most.Store(n)
					}

					time.Sleep(5 * time.Millisecond)
					open.Add(-1)

					return nil
				})
			})
		}

		wg.Wait()

		t.Run("THEN only one is ever open", func(t *testing.T) {
			assert.Equal(t, int32(1), most.Load())
		})
	})
}
