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

	// remaining and refresh are what API/Limit reports.
	remaining int
	refresh   int

	// calls counts the requests per path.
	calls map[string]int
}

func newAllowanceServer(t *testing.T) *allowanceServer {
	t.Helper()

	s := &allowanceServer{calls: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.calls[r.URL.Path]++

		switch {
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
	q := media.CoverQuery{
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
				assert.Equal(t, "TheGamesDB has used this month's request allowance; it renews in 12 days", err.Error())

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
		srv.set(true, 0, 0)

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

func TestAllowance_left(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	settings := schema.Settings{settingAPIKey: "good"}
	q := media.CoverQuery{
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
