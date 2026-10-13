package oauthsession_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/oauthsession"
	"gamevault/internal/domain/source"
)

// session is what the fake store keeps between scans.
type session struct {
	RefreshToken string `json:"refresh_token"`
	Account      string `json:"account"`
}

var (
	errNoSession = errors.New("paste a code")
	errBadCode   = errors.New("the store rejected the code")
	errExpired   = source.SignedOut("the session expired: paste a new code")
	errDown      = errors.New("the store is not answering")
)

// store is a fake OAuth server whose refresh tokens rotate on every use, like Epic's and GOG's: a
// used code or refresh token is refused.
type store struct {
	mu        sync.Mutex
	exchanges int
	refreshes int
	issued    int
	live      map[string]bool // refresh tokens that still work
	codes     map[string]bool // codes not used yet
	down      bool
	rotate    bool
	gate      chan struct{} // when set, a refresh waits for it
	entered   chan struct{} // when set, a refresh says it started
}

func newStore() *store {
	return &store{
		live:   map[string]bool{},
		codes:  map[string]bool{"good-code": true},
		rotate: true,
	}
}

func (s *store) grant() oauthsession.Grant[session] {
	s.issued++

	return oauthsession.Grant[session]{
		Session: session{
			RefreshToken: fmt.Sprintf("refresh-%d", s.issued),
			Account:      "acc1",
		},
		AccessToken: fmt.Sprintf("access-%d", s.issued),
		Lifetime:    8 * time.Hour,
	}
}

func (s *store) exchange(_ context.Context, code string) (oauthsession.Grant[session], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.down {
		return oauthsession.Grant[session]{}, errDown
	}

	if !s.codes[code] {
		return oauthsession.Grant[session]{}, errBadCode
	}

	delete(s.codes, code)
	s.exchanges++
	g := s.grant()
	s.live[g.Session.RefreshToken] = true

	return g, nil
}

func (s *store) refresh(ctx context.Context, old session) (oauthsession.Grant[session], error) {
	if s.entered != nil {
		s.entered <- struct{}{}
	}

	if s.gate != nil {
		<-s.gate
	}

	if ctx.Err() != nil {
		return oauthsession.Grant[session]{}, ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.down {
		return oauthsession.Grant[session]{}, errDown
	}

	if !s.live[old.RefreshToken] {
		return oauthsession.Grant[session]{}, errExpired
	}

	s.refreshes++
	g := s.grant()

	if !s.rotate {
		g.Session.RefreshToken = old.RefreshToken
		return g, nil
	}

	delete(s.live, old.RefreshToken)
	s.live[g.Session.RefreshToken] = true

	return g, nil
}

func (s *store) counts() (exchanges, refreshes int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.exchanges, s.refreshes
}

// clock is a settable clock.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

func newSessions(st *store, c *clock) *oauthsession.Sessions[session] {
	return &oauthsession.Sessions[session]{
		CodeField:    "code",
		StateField:   "session",
		Clean:        func(v string) (string, error) { return strings.Trim(v, `"' `), nil },
		Exchange:     st.exchange,
		Refresh:      st.refresh,
		RefreshToken: func(s session) string { return s.RefreshToken },
		ErrNoSession: errNoSession,
		ErrSignedOut: errExpired,
		Now:          c.Now,
	}
}

func stored(t *testing.T, settings source.Settings) session {
	t.Helper()

	var s session
	require.NoError(t, json.Unmarshal([]byte(settings["session"]), &s))

	return s
}

func TestSessions_prepare(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a store and a one-time code", func(t *testing.T) {
		st := newStore()
		c := &clock{now: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
		sessions := newSessions(st, c)

		t.Run("WHEN the code is prepared twice (Test, then Save)", func(t *testing.T) {
			first, err := sessions.Prepare(ctx, source.Settings{"code": ` "good-code" `, "other": "kept"})
			require.NoError(t, err)

			second, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})
			require.NoError(t, err)

			t.Run("THEN it is exchanged once and both store the same session instead of the code", func(t *testing.T) {
				exchanges, _ := st.counts()
				assert.Equal(t, 1, exchanges)
				assert.Equal(t, "refresh-1", stored(t, first).RefreshToken)
				assert.Equal(t, first["session"], second["session"])
				assert.NotContains(t, first, "code")
				assert.Equal(t, "kept", first["other"])
			})

			t.Run("AND its access token serves the first scan without a refresh", func(t *testing.T) {
				tok, err := sessions.Token(ctx, first)
				require.NoError(t, err)
				assert.Equal(t, "access-1", tok)

				_, refreshes := st.counts()
				assert.Zero(t, refreshes)
			})
		})

		t.Run("WHEN the code is forgotten and prepared again within the cache's time", func(t *testing.T) {
			sessions.ForgetCode(` "good-code" `)

			_, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})

			t.Run("THEN it is exchanged again, and the store refuses a used code", func(t *testing.T) {
				require.ErrorIs(t, err, errBadCode)
			})
		})

		t.Run("WHEN the same code is prepared after the cache's time", func(t *testing.T) {
			c.advance(16 * time.Minute)

			_, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})

			t.Run("THEN it is exchanged again, and the store refuses a used code", func(t *testing.T) {
				require.ErrorIs(t, err, errBadCode)
			})
		})
	})

	t.Run("GIVEN no new code", func(t *testing.T) {
		sessions := newSessions(newStore(), &clock{now: time.Now()})

		t.Run("THEN a stored session is kept as it is", func(t *testing.T) {
			out, err := sessions.Prepare(ctx, source.Settings{"session": `{"refresh_token":"x"}`, "code": source.SecretPlaceholder})
			require.NoError(t, err)
			assert.JSONEq(t, `{"refresh_token":"x"}`, out["session"])
			assert.NotContains(t, out, "code")
		})

		t.Run("OR without one it asks for a code", func(t *testing.T) {
			_, err := sessions.Prepare(ctx, source.Settings{})
			require.ErrorIs(t, err, errNoSession)
		})
	})

	t.Run("GIVEN a code the cleaner refuses", func(t *testing.T) {
		errEmailed := errors.New("that is the emailed code")
		sessions := newSessions(newStore(), &clock{now: time.Now()})
		sessions.Clean = func(string) (string, error) { return "", errEmailed }

		t.Run("THEN its error is returned", func(t *testing.T) {
			_, err := sessions.Prepare(ctx, source.Settings{"code": "123456"})
			require.ErrorIs(t, err, errEmailed)
		})
	})

	t.Run("GIVEN a store that is down during the exchange", func(t *testing.T) {
		st := newStore()
		st.down = true
		sessions := newSessions(st, &clock{now: time.Now()})

		_, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})
		require.ErrorIs(t, err, errDown)

		t.Run("WHEN it is back", func(t *testing.T) {
			st.down = false
			out, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})

			t.Run("THEN the code is exchanged then, nothing having been cached", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "refresh-1", stored(t, out).RefreshToken)
			})
		})
	})
}

func TestSessions_prepareGivesUpWhileARefreshRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a refresh under way that does not end", func(t *testing.T) {
		st := newStore()
		st.live["refresh-0"] = true
		st.gate, st.entered = make(chan struct{}), make(chan struct{}, 1)
		sessions := newSessions(st, &clock{now: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)})

		go func() { _, _ = sessions.Token(ctx, source.Settings{"session": `{"refresh_token":"refresh-0"}`}) }()

		<-st.entered

		// The refresh ends after 2 s anyway, so a caller that does wait for it fails the test rather
		// than hanging it.
		var once sync.Once

		end := func() { once.Do(func() { close(st.gate) }) }
		time.AfterFunc(2*time.Second, end)

		t.Run("WHEN a code is prepared by a caller that gives up", func(t *testing.T) {
			gone, stop := context.WithTimeout(ctx, 20*time.Millisecond)
			defer stop()

			begin := time.Now()
			_, err := sessions.Prepare(gone, source.Settings{"code": "good-code"})
			waited := time.Since(begin)

			end()

			t.Run("THEN it returns its context's error at once", func(t *testing.T) {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Less(t, waited, time.Second)
			})

			t.Run("AND the exchange still runs, so the next Prepare of the code gets its session", func(t *testing.T) {
				require.Eventually(t, func() bool {
					exchanges, _ := st.counts()
					return exchanges == 1
				}, 5*time.Second, time.Millisecond)

				out, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})
				require.NoError(t, err)
				assert.NotEmpty(t, stored(t, out).RefreshToken)
			})
		})
	})
}

func TestSessions_token(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	start := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN a stored session and a process with no access token yet", func(t *testing.T) {
		st := newStore()
		st.live["refresh-0"] = true
		c := &clock{now: start}
		sessions := newSessions(st, c)
		settings := source.Settings{"session": `{"refresh_token":"refresh-0","account":"acc1"}`}

		t.Run("WHEN a token is asked for", func(t *testing.T) {
			tok, err := sessions.Token(ctx, settings)
			require.NoError(t, err)

			t.Run("THEN the session is refreshed and the rotated one handed back to be saved", func(t *testing.T) {
				assert.Equal(t, "access-1", tok)
				assert.Equal(t, "refresh-1", stored(t, settings).RefreshToken)
			})
		})

		t.Run("WHEN another is asked for with the saved session", func(t *testing.T) {
			tok, err := sessions.Token(ctx, settings)
			require.NoError(t, err)

			t.Run("THEN the cached access token serves it", func(t *testing.T) {
				assert.Equal(t, "access-1", tok)

				_, refreshes := st.counts()
				assert.Equal(t, 1, refreshes)
			})
		})

		t.Run("WHEN a caller still holds the session from before the rotation", func(t *testing.T) {
			old := source.Settings{"session": `{"refresh_token":"refresh-0","account":"acc1"}`}
			tok, err := sessions.Token(ctx, old)

			t.Run("THEN it gets the newest session handed back, never reusing the old refresh token", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "access-1", tok)
				assert.Equal(t, "refresh-1", stored(t, old).RefreshToken)

				_, refreshes := st.counts()
				assert.Equal(t, 1, refreshes)
			})
		})

		t.Run("WHEN the access token is about to expire", func(t *testing.T) {
			c.advance(8*time.Hour - 30*time.Second)

			old := source.Settings{"session": `{"refresh_token":"refresh-0","account":"acc1"}`}
			tok, err := sessions.Token(ctx, old)

			t.Run("THEN the newest session is refreshed, even for a caller holding an old one", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "access-2", tok)
				assert.Equal(t, "refresh-2", stored(t, old).RefreshToken)
			})
		})
	})

	t.Run("GIVEN eight scans that need a token at once", func(t *testing.T) {
		st := newStore()
		st.live["refresh-0"] = true
		sessions := newSessions(st, &clock{now: start})

		var wg sync.WaitGroup

		results := make([]source.Settings, 8)
		for i := range results {
			results[i] = source.Settings{"session": `{"refresh_token":"refresh-0"}`}

			wg.Go(func() {
				_, err := sessions.Token(ctx, results[i])
				assert.NoError(t, err)
			})
		}

		wg.Wait()

		t.Run("THEN the session is refreshed once and every caller gets the rotated one", func(t *testing.T) {
			_, refreshes := st.counts()
			assert.Equal(t, 1, refreshes)

			for _, s := range results {
				assert.Equal(t, "refresh-1", stored(t, s).RefreshToken)
			}
		})
	})

	t.Run("GIVEN a caller that gives up while the session is refreshed", func(t *testing.T) {
		st := newStore()
		st.live["refresh-0"] = true
		st.gate, st.entered = make(chan struct{}), make(chan struct{}, 1)
		sessions := newSessions(st, &clock{now: start})

		done, stop := context.WithCancel(ctx)
		settings := source.Settings{"session": `{"refresh_token":"refresh-0"}`}

		errs := make(chan error, 1)

		go func() {
			_, err := sessions.Token(done, settings)
			errs <- err
		}()

		<-st.entered
		stop()
		require.ErrorIs(t, <-errs, context.Canceled)
		close(st.gate)

		t.Run("WHEN the next caller comes with the session it held", func(t *testing.T) {
			next := source.Settings{"session": `{"refresh_token":"refresh-0"}`}

			require.Eventually(t, func() bool {
				_, refreshes := st.counts()
				return refreshes == 1
			}, 5*time.Second, time.Millisecond)

			tok, err := sessions.Token(ctx, next)

			t.Run("THEN the refresh it started was not lost: the rotated session is handed back", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "access-1", tok)
				assert.Equal(t, "refresh-1", stored(t, next).RefreshToken)
			})
		})
	})

	t.Run("GIVEN a store whose refresh tokens do not rotate", func(t *testing.T) {
		st := newStore()
		st.rotate = false
		st.live["refresh-0"] = true
		c := &clock{now: start}
		sessions := newSessions(st, c)
		settings := source.Settings{"session": `{"refresh_token":"refresh-0"}`}

		t.Run("THEN tokens are renewed with the same session", func(t *testing.T) {
			_, err := sessions.Token(ctx, settings)
			require.NoError(t, err)

			c.advance(9 * time.Hour)

			tok, err := sessions.Token(ctx, settings)
			require.NoError(t, err)
			assert.Equal(t, "access-2", tok)
			assert.Equal(t, "refresh-0", stored(t, settings).RefreshToken)
		})
	})

	t.Run("GIVEN a refresh the store refuses", func(t *testing.T) {
		sessions := newSessions(newStore(), &clock{now: start})
		settings := source.Settings{"session": `{"refresh_token":"stale"}`}

		t.Run("THEN its error is returned and the stored session left alone", func(t *testing.T) {
			_, err := sessions.Token(ctx, settings)
			require.ErrorIs(t, err, source.ErrSignedOut)
			assert.JSONEq(t, `{"refresh_token":"stale"}`, settings["session"])
		})
	})

	t.Run("GIVEN no usable stored session", func(t *testing.T) {
		sessions := newSessions(newStore(), &clock{now: start})

		for _, raw := range []string{"", "not json", `{"refresh_token":""}`} {
			_, err := sessions.Token(ctx, source.Settings{"session": raw})
			require.ErrorIs(t, err, errNoSession, raw)
		}
	})
}

func TestSessions_withRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	rejected := fmt.Errorf("HTTP 401: %w", apiclient.ErrTokenRejected)

	t.Run("GIVEN a cached access token the store revoked early", func(t *testing.T) {
		st := newStore()
		sessions := newSessions(st, &clock{now: time.Now()})
		settings, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})
		require.NoError(t, err)

		t.Run("WHEN a call is made with it", func(t *testing.T) {
			var used []string

			err := sessions.WithRetry(ctx, settings, func(tok string) error {
				used = append(used, tok)
				if tok == "access-1" {
					return rejected
				}

				return nil
			})

			t.Run("THEN the session is refreshed once, the call made again and the rotated session handed back", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, []string{"access-1", "access-2"}, used)
				assert.Equal(t, "refresh-2", stored(t, settings).RefreshToken)
			})
		})
	})

	t.Run("GIVEN a store that rejects even a renewed access token", func(t *testing.T) {
		st := newStore()
		sessions := newSessions(st, &clock{now: time.Now()})
		settings, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})
		require.NoError(t, err)

		t.Run("WHEN a call is made", func(t *testing.T) {
			calls := 0
			err := sessions.WithRetry(ctx, settings, func(string) error { calls++; return rejected })

			t.Run("THEN it is signed out after one renewal", func(t *testing.T) {
				require.ErrorIs(t, err, source.ErrSignedOut)
				assert.Equal(t, 2, calls)

				_, refreshes := st.counts()
				assert.Equal(t, 1, refreshes)
			})
		})
	})

	t.Run("GIVEN a call that fails for another reason", func(t *testing.T) {
		sessions := newSessions(newStore(), &clock{now: time.Now()})
		settings, err := sessions.Prepare(ctx, source.Settings{"code": "good-code"})
		require.NoError(t, err)

		t.Run("THEN it is not retried", func(t *testing.T) {
			calls := 0
			err := sessions.WithRetry(ctx, settings, func(string) error { calls++; return errDown })
			require.ErrorIs(t, err, errDown)
			assert.Equal(t, 1, calls)
		})
	})
}
