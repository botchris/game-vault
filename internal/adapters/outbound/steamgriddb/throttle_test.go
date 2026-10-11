package steamgriddb

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
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

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

	t.Run("GIVEN the production throttle on a fake clock", func(t *testing.T) {
		clock := &fakeClock{now: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
		th := newThrottle(minGap, clock.Now, clock.Sleep)

		request := func() error {
			clock.now = clock.now.Add(200 * time.Millisecond) // the request itself takes 200 ms
			return nil
		}

		t.Run("WHEN three requests are sent in a row", func(t *testing.T) {
			for range 3 {
				require.NoError(t, th.do(ctx, request))
			}

			t.Run("THEN the first goes at once and each next one waits a second after the previous ended", func(t *testing.T) {
				assert.Equal(t, []time.Duration{time.Second, time.Second}, clock.sleeps)
			})
		})

		t.Run("WHEN a request is answered HTTP 429", func(t *testing.T) {
			clock.sleeps = nil
			clock.now = clock.now.Add(time.Hour)

			err := th.do(ctx, func() error { return errTooManyRequests })
			require.ErrorIs(t, err, provider.ErrRateLimited)

			sent := false
			err = th.do(ctx, func() error { sent = true; return nil })

			t.Run("THEN the next lookup fails fast as rate-limited instead of asking again", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.False(t, sent)
				assert.Empty(t, clock.sleeps)
			})

			t.Run("AND after the back-off it is sent again", func(t *testing.T) {
				clock.now = clock.now.Add(backoff)

				require.NoError(t, th.do(ctx, request))
			})
		})
	})

	t.Run("GIVEN the provider with a throttle, and lookups asked all at once", func(t *testing.T) {
		var inFlight, most, total atomic.Int32

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			total.Add(1)

			n := inFlight.Add(1)
			defer inFlight.Add(-1)

			for {
				m := most.Load()
				if n <= m || most.CompareAndSwap(m, n) {
					break
				}
			}

			time.Sleep(5 * time.Millisecond)
			w.Write([]byte(`{"success":true,"data":[]}`))
		}))
		t.Cleanup(srv.Close)

		p := NewCovers()
		p.API.BaseURL = srv.URL
		p.API.HTTP = srv.Client()
		p.throttle = newThrottle(10*time.Millisecond, time.Now, sleep)

		t.Run("WHEN eight lookups run concurrently", func(t *testing.T) {
			var (
				wg      sync.WaitGroup
				limited atomic.Int32
			)

			for range 8 {
				wg.Go(func() {
					_, err := p.Covers(ctx, media.CoverQuery{
						Links:    game.Links{game.LinkSteam: "620"},
						Fallback: true,
					}, goodSettings)
					if err != nil {
						assert.ErrorIs(t, err, provider.ErrRateLimited)
						limited.Add(1)
					}
				})
			}

			wg.Wait()

			t.Run("THEN SteamGridDB never sees two requests at the same time", func(t *testing.T) {
				assert.Equal(t, int32(1), most.Load())
			})

			t.Run("AND every lookup was either sent or refused at once as rate-limited (queue full)", func(t *testing.T) {
				assert.Equal(t, int32(8), total.Load()+limited.Load())
			})
		})
	})
}
