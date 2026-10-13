package thegamesdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// allowanceServer is a fake TheGamesDB whose key may have used up its monthly allowance. Used up,
// it answers every lookup with HTTP 429 and its generic body, as the real one did on 2026-10-10,
// while API/Limit (free) still answers 200 with the remaining allowance and the seconds until it
// renews. It counts the requests each path received.
type allowanceServer struct {
	*httptest.Server

	mu sync.Mutex

	// usedUp makes lookups answer HTTP 429.
	usedUp bool

	// remaining and refresh are what API/Limit reports; a negative refresh leaves the timer out.
	remaining int
	refresh   int

	// limitFails makes API/Limit answer HTTP 500 that many times.
	limitFails int

	// limitGate, when set, holds API/Limit's answer until it is closed.
	limitGate chan struct{}

	// calls counts the requests per path.
	calls map[string]int
}

func newAllowanceServer(t *testing.T) *allowanceServer {
	t.Helper()

	s := &allowanceServer{calls: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		gate := s.limitGate
		s.mu.Unlock()

		if gate != nil && r.URL.Path == "/v1/API/Limit" {
			<-gate
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		s.calls[r.URL.Path]++

		switch {
		case r.URL.Path == "/v1/API/Limit" && s.limitFails > 0:
			s.limitFails--

			w.WriteHeader(http.StatusInternalServerError)
		case r.URL.Path == "/v1/API/Limit" && s.refresh < 0:
			fmt.Fprintf(w, `{"remaining_monthly_allowance":%d,"extra_allowance":0}`, s.remaining)
		case r.URL.Path == "/v1/API/Limit":
			fmt.Fprintf(w, `{"remaining_monthly_allowance":%d,"extra_allowance":0,"allowance_refresh_timer":%d}`, s.remaining, s.refresh)
		case s.usedUp:
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(tooManyRequests))
		case r.URL.Path == "/v1.1/Games/ByGameName":
			w.Write([]byte(`{"data":{"games":[{"id":1,"game_title":"Halo 3","release_date":"2007-09-25","platform":12}]},
			 "include":{"boxart":{"base_url":{"large":"https://cdn.test/large/"},"data":{"1":[{"side":"front","filename":"front/1.jpg"}]}}}}`))
		default:
			w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *allowanceServer) set(usedUp bool, remaining, refresh int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.usedUp, s.remaining, s.refresh = usedUp, remaining, refresh
}

// lookups returns how many requests other than API/Limit the server received.
func (s *allowanceServer) lookups() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for path, c := range s.calls {
		if path != "/v1/API/Limit" {
			n += c
		}
	}

	return n
}

func (s *allowanceServer) limitChecks() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.calls["/v1/API/Limit"]
}

// allowanceProvider returns a provider talking to srv on a fake clock, throttle included.
func allowanceProvider(srv *allowanceServer, clock *fakeClock) *Provider {
	p := New()
	p.API.BaseURL, p.API.HTTP = srv.URL, srv.Client()
	p.now = clock.Now
	p.throttle = newThrottle(minGap, clock.Now, clock.Sleep)

	return p
}

func TestAllowance_usedUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	settings := schema.Settings{settingAPIKey: "good"}
	q := media.GameQuery{
		Title:             "Halo 3",
		PhysicalPlatforms: []string{"PC"},
	}

	t.Run("GIVEN a key whose allowance is used up and renews in 12 days", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 0, int((12 * 24 * time.Hour).Seconds()))

		clock := &fakeClock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
		p := allowanceProvider(srv, clock)

		t.Run("WHEN covers are searched and TheGamesDB answers HTTP 429", func(t *testing.T) {
			_, err := p.Covers(ctx, q, settings)

			t.Run("THEN the error is the used-up allowance, saying when it renews", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrQuotaExhausted)
				assert.Equal(t, "the API key has used this month's allowance: covers and sheets resume when it renews in 12 days", err.Error())

				t.Run("AND callers still treat it as a rate limit", func(t *testing.T) {
					assert.ErrorIs(t, err, provider.ErrRateLimited)
				})

				t.Run("AND the allowance was checked once", func(t *testing.T) {
					assert.Equal(t, 1, srv.limitChecks())
				})
			})
		})

		t.Run("WHEN more covers and sheets are asked a day later", func(t *testing.T) {
			before := srv.lookups()
			clock.now = clock.now.Add(24 * time.Hour)

			errs := make([]error, 0, 2*maxQueue)

			for range maxQueue {
				_, err := p.Covers(ctx, q, settings)
				errs = append(errs, err)

				_, err = NewDetails(p).Details(ctx, q, "en", settings)
				errs = append(errs, err)
			}

			t.Run("THEN TheGamesDB receives no request at all", func(t *testing.T) {
				assert.Equal(t, before, srv.lookups())
				assert.Equal(t, 1, srv.limitChecks())
			})

			t.Run("AND every lookup gets the allowance error, never the busy one", func(t *testing.T) {
				for _, err := range errs {
					require.ErrorIs(t, err, provider.ErrQuotaExhausted)
					assert.Contains(t, err.Error(), "renews in 11 days")
					assert.NotContains(t, err.Error(), "busy")
				}
			})
		})

		t.Run("WHEN the allowance has renewed and covers are searched again", func(t *testing.T) {
			srv.set(false, 1000, 30*24*3600)

			clock.now = clock.now.Add(11*24*time.Hour + time.Minute)
			before := srv.lookups()

			candidates, err := p.Covers(ctx, q, settings)

			t.Run("THEN requests resume and the cover is found", func(t *testing.T) {
				require.NoError(t, err)
				assert.Greater(t, srv.lookups(), before)
				require.NotEmpty(t, candidates)
				assert.Equal(t, "https://cdn.test/large/front/1.jpg", candidates[0].URL)
			})
		})
	})

	t.Run("GIVEN a key whose allowance is used up and TheGamesDB not saying when it renews", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 0, -1)

		clock := &fakeClock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
		p := allowanceProvider(srv, clock)

		t.Run("WHEN covers are searched", func(t *testing.T) {
			_, err := p.Covers(ctx, q, settings)

			t.Run("THEN the allowance is taken as used up for the fallback time", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrQuotaExhausted)
				assert.Contains(t, err.Error(), "renews in 6 hours")
			})
		})

		t.Run("WHEN the fallback time has passed", func(t *testing.T) {
			clock.now = clock.now.Add(allowanceFallback + time.Minute)
			before := srv.lookups()

			_, err := p.Covers(ctx, q, settings)

			t.Run("THEN TheGamesDB is asked again, and the allowance checked again", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrQuotaExhausted)
				assert.Greater(t, srv.lookups(), before)
				assert.Equal(t, 2, srv.limitChecks())
			})
		})
	})
}

func TestAllowance_perKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.GameQuery{Title: "Halo 3"}

	t.Run("GIVEN a key found to have used up its allowance", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 0, int((12 * 24 * time.Hour).Seconds()))

		clock := &fakeClock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
		p := allowanceProvider(srv, clock)
		p.platforms = map[string]int64{}

		_, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "good"})
		require.ErrorIs(t, err, provider.ErrQuotaExhausted)

		t.Run("WHEN another key is pasted and a cover is searched", func(t *testing.T) {
			srv.set(false, 1000, 86400)

			before := srv.lookups()
			_, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "other"})

			t.Run("THEN the new key is not blocked", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, before+1, srv.lookups())
			})
		})

		t.Run("WHEN the first key gets extra allowance and its settings are tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingAPIKey: "good"})
			require.NoError(t, err)

			before := srv.lookups()
			_, err = p.Covers(ctx, q, schema.Settings{settingAPIKey: "good"})

			t.Run("THEN its lookups are sent again", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, before+1, srv.lookups())
			})
		})

		t.Run("WHEN a test reports the allowance used up again", func(t *testing.T) {
			srv.set(true, 0, 3600)

			err := p.Test(ctx, schema.Settings{settingAPIKey: "good"})
			require.ErrorIs(t, err, ErrNoAllowance)

			before := srv.lookups()
			_, err = p.Covers(ctx, q, schema.Settings{settingAPIKey: "good"})

			t.Run("THEN lookups stop until it renews", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrQuotaExhausted)
				assert.Contains(t, err.Error(), "renews in 60 minutes")
				assert.Equal(t, before, srv.lookups())
			})
		})
	})

	t.Run("GIVEN an unknown key, which API/Limit answers with no allowance and no refresh timer", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 0, 0)

		clock := &fakeClock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
		p := allowanceProvider(srv, clock)
		p.platforms = map[string]int64{}

		t.Run("WHEN a cover is searched and TheGamesDB answers HTTP 429", func(t *testing.T) {
			_, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "unknown"})

			t.Run("THEN the error says the key is unknown, not that it renews", func(t *testing.T) {
				require.ErrorIs(t, err, ErrUnknownKey)
				assert.NotErrorIs(t, err, provider.ErrQuotaExhausted)
				assert.Equal(t, "the API key was not recognized: check it in the provider settings", err.Error())

				t.Run("AND it is still a rate limit, so nothing is remembered from the 429", func(t *testing.T) {
					assert.ErrorIs(t, err, provider.ErrRateLimited)
				})
			})
		})

		t.Run("WHEN TheGamesDB answers 429 again after the back-off, within 5 minutes", func(t *testing.T) {
			clock.now = clock.now.Add(backoff + time.Second)

			_, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "unknown"})

			t.Run("THEN the stored verdict is reused without another check", func(t *testing.T) {
				assert.Equal(t, 1, srv.limitChecks())
				require.ErrorIs(t, err, ErrUnknownKey)
				assert.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})
}

func TestAllowance_failedCheck(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.GameQuery{Title: "Halo 3"}
	settings := schema.Settings{settingAPIKey: "good"}

	t.Run("GIVEN a used-up key whose first allowance check fails", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 0, 86400)
		srv.limitFails = 1

		start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
		clock := &fakeClock{now: start}
		p := allowanceProvider(srv, clock)
		p.platforms = map[string]int64{}

		_, err := p.Covers(ctx, q, settings)
		require.ErrorIs(t, err, provider.ErrRateLimited)
		require.NotErrorIs(t, err, provider.ErrQuotaExhausted)

		t.Run("WHEN the next 429 comes after the back-off, within 5 minutes", func(t *testing.T) {
			clock.now = start.Add(backoff + time.Second)

			_, err := p.Covers(ctx, q, settings)

			t.Run("THEN the allowance is checked again and found used up", func(t *testing.T) {
				assert.Equal(t, 2, srv.limitChecks())
				require.ErrorIs(t, err, provider.ErrQuotaExhausted)
			})
		})
	})
}

func TestAllowance_concurrent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.GameQuery{Title: "Halo 3"}
	settings := schema.Settings{settingAPIKey: "good"}

	t.Run("GIVEN a used-up key and a provider without a throttle", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 0, 86400)

		p := testProvider(srv.Server)
		p.platforms = map[string]int64{}

		t.Run("WHEN eight lookups hit HTTP 429 at once", func(t *testing.T) {
			errs := make([]error, 8)

			var wg sync.WaitGroup

			for i := range errs {
				wg.Go(func() { _, errs[i] = p.Covers(ctx, q, settings) })
			}

			wg.Wait()

			t.Run("THEN the allowance is checked once and all get the allowance error", func(t *testing.T) {
				assert.Equal(t, 1, srv.limitChecks())

				for _, err := range errs {
					assert.ErrorIs(t, err, provider.ErrQuotaExhausted)
				}
			})
		})
	})

	t.Run("GIVEN a used-up key, the throttle, and a lookup queued while the allowance is checked", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 0, 86400)

		gate := make(chan struct{})
		release := sync.OnceFunc(func() { close(gate) })
		t.Cleanup(release) // never leave the server's handler blocked, even when an assertion fails

		srv.mu.Lock()
		srv.limitGate = gate
		srv.mu.Unlock()

		p := testProvider(srv.Server)
		p.platforms = map[string]int64{}
		p.throttle = newThrottle(minGap, nil, nil)

		t.Run("WHEN the check answers", func(t *testing.T) {
			var (
				wg     sync.WaitGroup
				first  error
				queued error
			)

			wg.Go(func() { _, first = p.Covers(ctx, q, settings) })

			require.Eventually(t, func() bool { return srv.lookups() == 1 }, 5*time.Second, time.Millisecond)

			wg.Go(func() { _, queued = p.Covers(ctx, q, settings) })

			require.Eventually(t, func() bool { return p.throttle.Queued() == 2 }, 5*time.Second, time.Millisecond)

			release()
			wg.Wait()

			t.Run("THEN both get the allowance error, never the busy one", func(t *testing.T) {
				require.ErrorIs(t, first, provider.ErrQuotaExhausted)
				require.ErrorIs(t, queued, provider.ErrQuotaExhausted)
				assert.NotContains(t, queued.Error(), "busy")
				assert.Equal(t, 1, srv.lookups())
			})
		})
	})
}

func TestAllowance_left(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	settings := schema.Settings{settingAPIKey: "good"}
	q := media.GameQuery{
		Title:             "Halo 3",
		PhysicalPlatforms: []string{"PC"},
	}

	t.Run("GIVEN a key with allowance left and TheGamesDB answering a burst with HTTP 429", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 500, 86400)

		start := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
		clock := &fakeClock{now: start}
		p := allowanceProvider(srv, clock)

		t.Run("WHEN covers are searched", func(t *testing.T) {
			_, err := p.Covers(ctx, q, settings)

			t.Run("THEN the error is the plain rate limit, not the allowance", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.NotErrorIs(t, err, provider.ErrQuotaExhausted)
				assert.Equal(t, 1, srv.limitChecks())
			})
		})

		t.Run("WHEN the next lookup comes during the 30 s back-off", func(t *testing.T) {
			before := srv.lookups()
			clock.now = clock.now.Add(backoff / 2)

			_, err := p.Covers(ctx, q, settings)

			t.Run("THEN it fails fast as rate-limited without reaching TheGamesDB", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.NotErrorIs(t, err, provider.ErrQuotaExhausted)
				assert.Equal(t, before, srv.lookups())
			})
		})

		t.Run("WHEN TheGamesDB answers 429 again after the back-off, within a few minutes", func(t *testing.T) {
			clock.now = start.Add(backoff + time.Second)

			_, err := p.Covers(ctx, q, settings)

			t.Run("THEN the allowance is not checked again so soon", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Equal(t, 1, srv.limitChecks())
			})
		})

		t.Run("WHEN the back-off has passed and TheGamesDB answers again", func(t *testing.T) {
			srv.set(false, 500, 86400)

			clock.now = clock.now.Add(backoff + time.Second)

			candidates, err := p.Covers(ctx, q, settings)

			t.Run("THEN the cover is found", func(t *testing.T) {
				require.NoError(t, err)
				assert.NotEmpty(t, candidates)
			})
		})
	})
}

func TestRenewsIn(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{12 * 24 * time.Hour, "12 days"},
		{36 * time.Hour, "2 days"},
		{6 * time.Hour, "6 hours"},
		{90 * time.Minute, "2 hours"},
		{45 * time.Minute, "45 minutes"},
		{30 * time.Second, "a minute"},
	} {
		assert.Equal(t, tc.want, renewsIn(tc.in), tc.in.String())
	}
}
