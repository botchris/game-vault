package igdb

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

func TestClient_tokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a Twitch application's credentials", func(t *testing.T) {
		f := newFakeIGDB(t)
		now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		c := f.client(func() time.Time { return now })

		t.Run("WHEN two queries are sent", func(t *testing.T) {
			var out []map[string]any
			require.NoError(t, c.query(ctx, goodSettings, "games", "fields id; limit 1;", &out))
			require.NoError(t, c.query(ctx, goodSettings, "games", "fields id; limit 1;", &out))

			t.Run("THEN one access token serves both", func(t *testing.T) {
				assert.Equal(t, 1, f.tokens())
			})
		})

		t.Run("WHEN the token is about to expire", func(t *testing.T) {
			now = now.Add(5011271*time.Second - time.Minute)

			var out []map[string]any
			require.NoError(t, c.query(ctx, goodSettings, "games", "fields id; limit 1;", &out))

			t.Run("THEN a new one is asked for before it does", func(t *testing.T) {
				assert.Equal(t, 2, f.tokens())
			})
		})

		t.Run("WHEN IGDB refuses the cached token", func(t *testing.T) {
			f.revokeTokens()

			var out []map[string]any

			err := c.query(ctx, goodSettings, "games", "fields id; limit 1;", &out)

			t.Run("THEN a new token is asked for once and the query retried", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, 3, f.tokens())
			})
		})
	})

	t.Run("GIVEN credentials Twitch rejects", func(t *testing.T) {
		f := newFakeIGDB(t)
		c := f.client(time.Now)

		for name, s := range map[string]schema.Settings{
			"an unknown Client ID": {
				settingClientID:     "bogus",
				settingClientSecret: "good-secret",
			},
			"a wrong secret": {
				settingClientID:     "good-id",
				settingClientSecret: "bogus",
			},
		} {
			t.Run("WHEN the connection is tested with "+name, func(t *testing.T) {
				err := c.Test(ctx, s)

				t.Run("THEN the error says to check them", func(t *testing.T) {
					require.ErrorIs(t, err, ErrBadCredentials)
					assert.Contains(t, err.Error(), "check them in the provider settings")
				})
			})
		}

		t.Run("WHEN the connection is tested without them", func(t *testing.T) {
			err := c.Test(ctx, schema.Settings{})

			t.Run("THEN it asks for them without calling Twitch", func(t *testing.T) {
				require.ErrorIs(t, err, ErrNotConfigured)
				assert.Zero(t, f.tokens())
			})
		})
	})

	t.Run("GIVEN good credentials", func(t *testing.T) {
		f := newFakeIGDB(t)
		c := f.client(time.Now)

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := c.Test(ctx, goodSettings)

			t.Run("THEN it works with one minimal query", func(t *testing.T) {
				require.NoError(t, err)
				assert.Len(t, f.sent("games"), 1)
			})
		})

		t.Run("WHEN IGDB answers HTTP 429", func(t *testing.T) {
			f.setBusy(true)

			var out []map[string]any

			err := c.query(ctx, goodSettings, "games", "fields id; limit 1;", &out)

			t.Run("THEN it is a rate limit, which records nothing", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})
}

func TestLimiter(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN IGDB's limits: 4 requests per second, 8 open at once", func(t *testing.T) {
		var (
			mu     sync.Mutex
			starts []time.Time
		)

		l := newLimiter(250*time.Millisecond, 8, time.Now)

		t.Run("WHEN five requests are sent at once", func(t *testing.T) {
			var wg sync.WaitGroup

			for range 5 {
				wg.Go(func() {
					_ = l.do(ctx, func() error {
						mu.Lock()

						starts = append(starts, time.Now())
						mu.Unlock()

						return nil
					})
				})
			}

			wg.Wait()

			t.Run("THEN they start a quarter of a second apart", func(t *testing.T) {
				require.Len(t, starts, 5)

				first, last := starts[0], starts[0]
				for _, s := range starts {
					first, last = minTime(first, s), maxTime(last, s)
				}

				assert.GreaterOrEqual(t, last.Sub(first), 900*time.Millisecond)
			})
		})
	})

	t.Run("GIVEN a limiter allowing two open requests", func(t *testing.T) {
		l := newLimiter(0, 2, time.Now)

		t.Run("WHEN slow requests pile up", func(t *testing.T) {
			var open, most atomic.Int32

			var wg sync.WaitGroup

			for range 6 {
				wg.Go(func() {
					_ = l.do(ctx, func() error {
						n := open.Add(1)

						for {
							m := most.Load()
							if n <= m || most.CompareAndSwap(m, n) {
								break
							}
						}

						time.Sleep(20 * time.Millisecond)
						open.Add(-1)

						return nil
					})
				})
			}

			wg.Wait()

			t.Run("THEN no more than two are ever open", func(t *testing.T) {
				assert.Equal(t, int32(2), most.Load())
			})
		})
	})
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}

	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}
