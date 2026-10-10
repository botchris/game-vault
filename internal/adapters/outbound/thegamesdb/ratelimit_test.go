package thegamesdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// tooManyRequests is TheGamesDB's answer to a burst of requests, as a test copy of the library
// received it on 2026-10-10: HTTP 429 with its generic error body.
const tooManyRequests = `{"code":429,"status":"Unknown Error Code","remaining_monthly_allowance":0}`

func TestRateLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a TheGamesDB that answers every search with HTTP 429", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/Platforms" {
				w.Write([]byte(`{"data":{"platforms":{"12":{"id":12,"name":"Sony Playstation 3","alias":"sony-playstation-3"}}}}`))
				return
			}

			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(tooManyRequests))
		}))
		t.Cleanup(srv.Close)

		p := testProvider(srv)
		q := media.CoverQuery{
			Title:             "Halo 3",
			System:            "PS3",
			PhysicalPlatforms: []string{"PS3"},
		}

		t.Run("WHEN covers are searched", func(t *testing.T) {
			_, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "good"})

			t.Run("THEN the error is the rate limit, not a generic failure", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Contains(t, err.Error(), "429")
			})
		})

		t.Run("WHEN details are asked", func(t *testing.T) {
			_, err := NewDetails(p).Details(ctx, q, "en", schema.Settings{settingAPIKey: "good"})

			t.Run("THEN the error is the rate limit too", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})
}

// fakeClock is a clock whose sleeps only move it forward, recording how long each one was.
type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)

	return nil
}

func TestThrottle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a throttle with a 500 ms gap on a fake clock", func(t *testing.T) {
		clock := &fakeClock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
		th := newThrottle(500*time.Millisecond, clock.Now, clock.Sleep)

		request := func() error {
			clock.now = clock.now.Add(100 * time.Millisecond) // the request itself takes 100 ms
			return nil
		}

		t.Run("WHEN three requests are sent in a row", func(t *testing.T) {
			for range 3 {
				require.NoError(t, th.do(ctx, request))
			}

			t.Run("THEN the first goes at once and each next one waits the gap after the previous ended", func(t *testing.T) {
				assert.Equal(t, []time.Duration{500 * time.Millisecond, 500 * time.Millisecond}, clock.sleeps)
			})
		})

		t.Run("WHEN the next request comes after the gap has passed", func(t *testing.T) {
			clock.sleeps = nil
			clock.now = clock.now.Add(2 * time.Second)

			require.NoError(t, th.do(ctx, request))

			t.Run("THEN it does not wait", func(t *testing.T) {
				assert.Empty(t, clock.sleeps)
			})
		})
	})

	t.Run("GIVEN the provider with a throttle, and lookups asked all at once", func(t *testing.T) {
		var inFlight, most atomic.Int32

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := inFlight.Add(1)
			defer inFlight.Add(-1)

			for {
				m := most.Load()
				if n <= m || most.CompareAndSwap(m, n) {
					break
				}
			}

			time.Sleep(5 * time.Millisecond)
			w.Write([]byte(`{"data":{"games":[]}}`))
		}))
		t.Cleanup(srv.Close)

		p := testProvider(srv)
		p.throttle = newThrottle(time.Millisecond, time.Now, sleep)

		t.Run("WHEN eight lookups run concurrently", func(t *testing.T) {
			var wg sync.WaitGroup

			for range 8 {
				wg.Go(func() {
					_, err := p.Covers(ctx, media.CoverQuery{Title: "Halo 3"}, schema.Settings{settingAPIKey: "good"})
					assert.NoError(t, err)
				})
			}

			wg.Wait()

			t.Run("THEN TheGamesDB never sees two requests at the same time", func(t *testing.T) {
				assert.Equal(t, int32(1), most.Load())
			})
		})
	})
}

func TestThrottle_failsFast(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	sent := 0
	request := func() error {
		sent++
		return nil
	}

	t.Run("GIVEN a throttle whose queue is full", func(t *testing.T) {
		clock := &fakeClock{now: start}
		th := newThrottle(minGap, clock.Now, clock.Sleep)
		th.queued = maxQueue

		t.Run("WHEN one more request comes", func(t *testing.T) {
			sent = 0
			err := th.do(ctx, request)

			t.Run("THEN it is refused at once as rate-limited, without waiting or sending", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Zero(t, sent)
				assert.Empty(t, clock.sleeps)
			})
		})
	})

	t.Run("GIVEN a throttle whose next turn is further away than the longest wait", func(t *testing.T) {
		clock := &fakeClock{now: start}
		th := newThrottle(minGap, clock.Now, clock.Sleep)
		th.next = start.Add(maxWait + time.Second)

		t.Run("WHEN a request comes", func(t *testing.T) {
			sent = 0
			err := th.do(ctx, request)

			t.Run("THEN it is refused at once as rate-limited", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Zero(t, sent)
				assert.Empty(t, clock.sleeps)
			})
		})
	})

	t.Run("GIVEN a request that TheGamesDB answers with HTTP 429", func(t *testing.T) {
		clock := &fakeClock{now: start}
		th := newThrottle(minGap, clock.Now, clock.Sleep)
		err := th.do(ctx, func() error { return provider.ErrRateLimited })
		require.ErrorIs(t, err, provider.ErrRateLimited)

		t.Run("WHEN the next request comes during the back-off", func(t *testing.T) {
			sent = 0
			clock.now = clock.now.Add(backoff / 2)
			err := th.do(ctx, request)

			t.Run("THEN it fails fast without reaching TheGamesDB", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Zero(t, sent)
			})
		})

		t.Run("WHEN the next request comes after the back-off", func(t *testing.T) {
			sent = 0
			clock.now = start.Add(backoff + time.Second)
			err := th.do(ctx, request)

			t.Run("THEN it is sent", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, 1, sent)
			})
		})
	})
}

func TestTest_skipsTheThrottle(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN TheGamesDB backing off after a 429", func(t *testing.T) {
		srv, _ := fakeServer(t)
		p := testProvider(srv)
		p.throttle = newThrottle(minGap, time.Now, sleep)
		p.throttle.next = time.Now().Add(backoff)

		t.Run("WHEN the settings are tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingAPIKey: "good"})

			t.Run("THEN the check is sent anyway and passes", func(t *testing.T) {
				require.NoError(t, err)
			})
		})
	})
}
