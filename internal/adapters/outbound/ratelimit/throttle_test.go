package ratelimit_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/ratelimit"
	"gamevault/internal/domain/provider"
)

// fakeClock is a clock whose sleeps only move it forward, recording how long each one was.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)

	return nil
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func (c *fakeClock) taken() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := c.sleeps
	c.sleeps = nil

	return out
}

// on returns cfg running on the clock.
func on(clock *fakeClock, cfg ratelimit.Config) *ratelimit.Throttle {
	cfg.Now, cfg.Sleep = clock.Now, clock.Sleep

	return ratelimit.New(cfg)
}

// retryAfter is a rate-limit answer that says how long to wait, as apiclient.StatusError does.
type retryAfter time.Duration

func (r retryAfter) Error() string             { return fmt.Sprintf("HTTP 429, retry after %v", time.Duration(r)) }
func (r retryAfter) Unwrap() error             { return provider.ErrRateLimited }
func (r retryAfter) RetryAfter() time.Duration { return time.Duration(r) }

func TestThrottle_pacing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a throttle whose gap counts from the end of the previous request", func(t *testing.T) {
		clock := newClock()
		th := on(clock, ratelimit.Config{
			Gap:      time.Second,
			AfterEnd: true,
		})
		request := func() error {
			clock.advance(200 * time.Millisecond) // the request itself takes 200 ms
			return nil
		}

		t.Run("WHEN three requests are sent in a row", func(t *testing.T) {
			for range 3 {
				require.NoError(t, th.Do(ctx, request))
			}

			t.Run("THEN the first goes at once and each next one waits the gap after the previous ended", func(t *testing.T) {
				assert.Equal(t, []time.Duration{time.Second, time.Second}, clock.taken())
			})
		})

		t.Run("WHEN the next request comes after the gap has passed", func(t *testing.T) {
			clock.advance(2 * time.Second)
			require.NoError(t, th.Do(ctx, request))

			t.Run("THEN it does not wait", func(t *testing.T) {
				assert.Empty(t, clock.taken())
			})
		})
	})

	t.Run("GIVEN a throttle whose gap counts from the start of the previous request", func(t *testing.T) {
		clock := newClock()
		th := on(clock, ratelimit.Config{Gap: time.Second})

		t.Run("WHEN three requests that take 200 ms are sent in a row", func(t *testing.T) {
			for range 3 {
				require.NoError(t, th.Do(ctx, func() error { clock.advance(200 * time.Millisecond); return nil }))
			}

			t.Run("THEN each starts a gap after the previous one started", func(t *testing.T) {
				assert.Equal(t, []time.Duration{800 * time.Millisecond, 800 * time.Millisecond}, clock.taken())
			})
		})
	})

	t.Run("GIVEN a throttle that allows a burst of three after a quiet spell", func(t *testing.T) {
		clock := newClock()
		th := on(clock, ratelimit.Config{
			Gap:   1500 * time.Millisecond,
			Burst: 3,
		})
		instant := func() error { return nil }

		t.Run("WHEN five requests come at once", func(t *testing.T) {
			for range 5 {
				require.NoError(t, th.Do(ctx, instant))
			}

			t.Run("THEN three go at once and the others one gap apart", func(t *testing.T) {
				assert.Equal(t, []time.Duration{1500 * time.Millisecond, 1500 * time.Millisecond}, clock.taken())
			})
		})

		t.Run("WHEN the service has been quiet for a while", func(t *testing.T) {
			clock.advance(time.Minute)

			for range 3 {
				require.NoError(t, th.Do(ctx, instant))
			}

			t.Run("THEN a new burst goes at once", func(t *testing.T) {
				assert.Empty(t, clock.taken())
			})
		})
	})
}

func TestThrottle_failsFast(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	sent := 0
	request := func() error {
		sent++
		return nil
	}

	t.Run("GIVEN a throttle whose next turn is further away than the longest wait", func(t *testing.T) {
		clock := newClock()
		th := on(clock, ratelimit.Config{
			Gap:     10 * time.Second,
			MaxWait: 5 * time.Second,
		})
		require.NoError(t, th.Do(ctx, request))

		t.Run("WHEN a request comes", func(t *testing.T) {
			sent = 0
			err := th.Do(ctx, request)

			t.Run("THEN it is refused at once as busy, without waiting or sending", func(t *testing.T) {
				require.ErrorIs(t, err, ratelimit.ErrBusy)
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Zero(t, sent)
				assert.Empty(t, clock.taken())
			})
		})
	})

	t.Run("GIVEN a throttle whose queue is full", func(t *testing.T) {
		th := ratelimit.New(ratelimit.Config{
			Gap:      time.Millisecond,
			MaxQueue: 2,
		})

		release := make(chan struct{})

		var wg sync.WaitGroup

		for range 2 {
			wg.Go(func() { _ = th.Do(ctx, func() error { <-release; return nil }) })
		}

		require.Eventually(t, func() bool { return th.Queued() == 2 }, 5*time.Second, time.Millisecond)

		t.Run("WHEN one more request comes", func(t *testing.T) {
			sent = 0
			err := th.Do(ctx, request)

			close(release)
			wg.Wait()

			t.Run("THEN it is refused at once as busy", func(t *testing.T) {
				require.ErrorIs(t, err, ratelimit.ErrBusy)
				assert.Zero(t, sent)
			})

			t.Run("AND the queue empties once the others are done", func(t *testing.T) {
				assert.Zero(t, th.Queued())
			})
		})
	})

	t.Run("GIVEN a request under way that takes longer than a caller may wait", func(t *testing.T) {
		th := ratelimit.New(ratelimit.Config{
			Gap:     time.Millisecond,
			MaxWait: 20 * time.Millisecond,
		})

		release := make(chan struct{})
		started := make(chan struct{})

		go th.Do(ctx, func() error {
			close(started)
			<-release

			return nil
		})

		<-started

		t.Run("WHEN another caller asks for its turn", func(t *testing.T) {
			sent = 0
			begin := time.Now()
			err := th.Do(ctx, request)

			close(release)

			t.Run("THEN it gives up quickly as busy instead of hanging behind it", func(t *testing.T) {
				require.ErrorIs(t, err, ratelimit.ErrBusy)
				assert.Zero(t, sent)
				assert.Less(t, time.Since(begin), time.Second)
			})
		})
	})

	t.Run("GIVEN a caller queued behind a slow request, with MaxWait bounding its whole wait", func(t *testing.T) {
		clock := newClock()
		th := on(clock, ratelimit.Config{
			Gap:      time.Second,
			AfterEnd: true,
			MaxWait:  3 * time.Second,
		})

		release := make(chan struct{})
		started := make(chan struct{})
		first := make(chan error, 1)

		go func() {
			first <- th.Do(ctx, func() error {
				close(started)
				<-release

				return nil
			})
		}()

		<-started

		second := make(chan error, 1)

		go func() { second <- th.Do(ctx, request) }()

		require.Eventually(t, func() bool { return th.Queued() == 2 }, 5*time.Second, time.Millisecond)

		t.Run("WHEN the slow request ends after 2.5 s and the gap after it is 1 s more", func(t *testing.T) {
			sent = 0

			clock.advance(2500 * time.Millisecond)
			close(release)
			require.NoError(t, <-first)

			err := <-second

			t.Run("THEN the caller is refused as busy: 3.5 s in all is over MaxWait", func(t *testing.T) {
				require.ErrorIs(t, err, ratelimit.ErrBusy)
				assert.Zero(t, sent)
				assert.Empty(t, clock.taken())
			})
		})
	})

	t.Run("GIVEN a caller that gives up while it waits", func(t *testing.T) {
		th := ratelimit.New(ratelimit.Config{Gap: time.Hour})
		require.NoError(t, th.Do(ctx, request))

		done, stop := context.WithTimeout(ctx, 20*time.Millisecond)
		defer stop()

		t.Run("WHEN its context ends", func(t *testing.T) {
			sent = 0
			err := th.Do(done, request)

			t.Run("THEN it returns the context's error without sending", func(t *testing.T) {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Zero(t, sent)
			})
		})
	})
}

func TestThrottle_backOff(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	sent := 0
	request := func() error {
		sent++
		return nil
	}
	config := ratelimit.Config{
		Gap:      time.Second,
		AfterEnd: true,
		MaxWait:  3 * time.Second,
		Backoff:  time.Minute,
	}

	t.Run("GIVEN a request answered HTTP 429", func(t *testing.T) {
		clock := newClock()
		th := on(clock, config)
		start := clock.Now()

		err := th.Do(ctx, func() error { return fmt.Errorf("HTTP 429: %w", provider.ErrRateLimited) })
		require.ErrorIs(t, err, provider.ErrRateLimited)

		t.Run("WHEN the next request comes during the back-off", func(t *testing.T) {
			sent = 0

			clock.advance(30 * time.Second)

			err := th.Do(ctx, request)

			t.Run("THEN it fails fast as resting, without reaching the service or waiting", func(t *testing.T) {
				require.ErrorIs(t, err, ratelimit.ErrResting)
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Zero(t, sent)
				assert.Empty(t, clock.taken())
			})
		})

		t.Run("WHEN the next request comes after the back-off", func(t *testing.T) {
			sent = 0
			clock.now = start.Add(time.Minute)
			err := th.Do(ctx, request)

			t.Run("THEN it is sent", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, 1, sent)
			})
		})
	})

	t.Run("GIVEN an outage (no answer, a 5xx)", func(t *testing.T) {
		clock := newClock()
		th := on(clock, config)
		_ = th.Do(ctx, func() error { return fmt.Errorf("HTTP 503: %w", provider.ErrUnavailable) })

		t.Run("THEN it backs off too", func(t *testing.T) {
			sent = 0

			require.ErrorIs(t, th.Do(ctx, request), ratelimit.ErrResting)
			assert.Zero(t, sent)
		})
	})

	t.Run("GIVEN a request that fails because the key's allowance is used up", func(t *testing.T) {
		clock := newClock()
		th := on(clock, config)
		_ = th.Do(ctx, func() error { return fmt.Errorf("used up: %w", provider.ErrQuotaExhausted) })

		t.Run("WHEN the next request comes", func(t *testing.T) {
			sent = 0
			err := th.Do(ctx, request)

			t.Run("THEN it only waits the usual gap, not the back-off", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, 1, sent)
				assert.Equal(t, []time.Duration{time.Second}, clock.taken())
			})
		})
	})

	t.Run("GIVEN a request that fails because its caller gave up (its connection dropped by us)", func(t *testing.T) {
		clock := newClock()
		th := on(clock, config)

		gone, stop := context.WithCancel(ctx)
		err := th.Do(gone, func() error {
			stop()

			return fmt.Errorf("no answer: %w", provider.ErrUnavailable)
		})
		require.ErrorIs(t, err, provider.ErrUnavailable)

		t.Run("WHEN the next request comes", func(t *testing.T) {
			sent = 0
			err := th.Do(ctx, request)

			t.Run("THEN no back-off started: it only waits the gap after the abandoned request", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, 1, sent)
				assert.Equal(t, []time.Duration{time.Second}, clock.taken())
			})
		})
	})

	t.Run("GIVEN a failure that is not a rate limit", func(t *testing.T) {
		clock := newClock()
		th := on(clock, config)
		_ = th.Do(ctx, func() error { return errors.New("HTTP 404") })

		t.Run("THEN the next request only waits the gap", func(t *testing.T) {
			sent = 0

			require.NoError(t, th.Do(ctx, request))
			assert.Equal(t, []time.Duration{time.Second}, clock.taken())
		})
	})

	t.Run("GIVEN a rate limit that says how long to wait", func(t *testing.T) {
		for name, tc := range map[string]struct {
			hint time.Duration
			rest time.Duration
		}{
			"longer than the back-off":  {5 * time.Minute, 5 * time.Minute},
			"shorter than the back-off": {10 * time.Second, time.Minute},
			"absurdly long":             {48 * time.Hour, time.Hour},
		} {
			t.Run("WHEN it is "+name, func(t *testing.T) {
				clock := newClock()
				th := on(clock, config)
				start := clock.Now()
				_ = th.Do(ctx, func() error { return retryAfter(tc.hint) })

				t.Run("THEN the throttle rests for the longer of the two, within an hour", func(t *testing.T) {
					clock.now = start.Add(tc.rest - 10*time.Second) // further than MaxWait from the end

					require.ErrorIs(t, th.Do(ctx, request), ratelimit.ErrResting)

					clock.now = start.Add(tc.rest)

					require.NoError(t, th.Do(ctx, request))
				})
			})
		}
	})

	t.Run("GIVEN a throttle with its own rule for what backs off", func(t *testing.T) {
		errSlowDown := errors.New("slow down")
		clock := newClock()
		cfg := config
		cfg.BackoffOn = func(err error) bool { return errors.Is(err, errSlowDown) }
		th := on(clock, cfg)

		_ = th.Do(ctx, func() error { return errSlowDown })

		t.Run("THEN that error starts the back-off", func(t *testing.T) {
			require.ErrorIs(t, th.Do(ctx, request), ratelimit.ErrResting)
		})
	})
}

func TestThrottle_concurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, open := range []int{1, 3} {
		t.Run(fmt.Sprintf("GIVEN a throttle that keeps %d requests open at most", open), func(t *testing.T) {
			th := ratelimit.New(ratelimit.Config{
				Gap:  time.Millisecond,
				Open: open,
			})

			var inFlight, most atomic.Int32

			request := func() error {
				n := inFlight.Add(1)
				defer inFlight.Add(-1)

				for {
					m := most.Load()
					if n <= m || most.CompareAndSwap(m, n) {
						break
					}
				}

				time.Sleep(5 * time.Millisecond)

				return nil
			}

			t.Run("WHEN twelve callers ask at once", func(t *testing.T) {
				var wg sync.WaitGroup

				for range 12 {
					wg.Go(func() { assert.NoError(t, th.Do(ctx, request)) })
				}

				wg.Wait()

				t.Run("THEN no more than that are ever under way together", func(t *testing.T) {
					assert.LessOrEqual(t, most.Load(), int32(open))
				})
			})
		})
	}

	t.Run("GIVEN no throttle", func(t *testing.T) {
		var th *ratelimit.Throttle

		t.Run("THEN the request runs at once", func(t *testing.T) {
			ran := false

			require.NoError(t, th.Do(ctx, func() error { ran = true; return nil }))
			assert.True(t, ran)
		})
	})
}
