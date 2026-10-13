package oauthapp_test

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

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/oauthapp"
)

// issuer is a fake token endpoint: each fetch issues a new token, counted.
type issuer struct {
	issued   atomic.Int32
	lifetime time.Duration
	delay    time.Duration
	err      error
}

func (i *issuer) fetch(ctx context.Context) (oauthapp.Token, error) {
	time.Sleep(i.delay)

	if i.err != nil {
		return oauthapp.Token{}, i.err
	}

	n := i.issued.Add(1)

	return oauthapp.Token{
		Value:    fmt.Sprintf("token-%d", n),
		Lifetime: i.lifetime,
	}, nil
}

func TestTokens_get(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	key := oauthapp.Key("client-id", "client-secret")

	t.Run("GIVEN an application whose tokens live two hours", func(t *testing.T) {
		iss := &issuer{lifetime: 2 * time.Hour}
		tokens := &oauthapp.Tokens{Now: func() time.Time { return now }}

		t.Run("WHEN a token is asked for twice", func(t *testing.T) {
			first, err := tokens.Get(ctx, key, iss.fetch)
			require.NoError(t, err)

			second, err := tokens.Get(ctx, key, iss.fetch)
			require.NoError(t, err)

			t.Run("THEN one token serves both", func(t *testing.T) {
				assert.Equal(t, "token-1", first)
				assert.Equal(t, first, second)
				assert.Equal(t, int32(1), iss.issued.Load())
			})
		})

		t.Run("WHEN it is within five minutes of its end", func(t *testing.T) {
			now = now.Add(2*time.Hour - 4*time.Minute)

			tok, err := tokens.Get(ctx, key, iss.fetch)
			require.NoError(t, err)

			t.Run("THEN a new one is asked for before it expires", func(t *testing.T) {
				assert.Equal(t, "token-2", tok)
			})
		})

		t.Run("WHEN other credentials ask", func(t *testing.T) {
			tok, err := tokens.Get(ctx, oauthapp.Key("client-id", "other-secret"), iss.fetch)
			require.NoError(t, err)

			t.Run("THEN they get a token of their own", func(t *testing.T) {
				assert.Equal(t, "token-3", tok)
			})
		})
	})

	t.Run("GIVEN tokens that live two minutes", func(t *testing.T) {
		iss := &issuer{lifetime: 2 * time.Minute}
		tokens := &oauthapp.Tokens{Now: func() time.Time { return now }}

		_, err := tokens.Get(ctx, key, iss.fetch)
		require.NoError(t, err)

		t.Run("THEN they are renewed halfway rather than five minutes early", func(t *testing.T) {
			now = now.Add(59 * time.Second)
			_, _ = tokens.Get(ctx, key, iss.fetch)
			assert.Equal(t, int32(1), iss.issued.Load())

			now = now.Add(time.Second)
			_, _ = tokens.Get(ctx, key, iss.fetch)
			assert.Equal(t, int32(2), iss.issued.Load())
		})
	})

	t.Run("GIVEN an endpoint that announces no lifetime", func(t *testing.T) {
		iss := &issuer{}
		tokens := &oauthapp.Tokens{Now: func() time.Time { return now }}

		_, err := tokens.Get(ctx, key, iss.fetch)
		require.NoError(t, err)

		t.Run("THEN the token is kept most of an hour rather than asked for every time", func(t *testing.T) {
			now = now.Add(50 * time.Minute)
			_, _ = tokens.Get(ctx, key, iss.fetch)
			assert.Equal(t, int32(1), iss.issued.Load())
		})
	})

	t.Run("GIVEN an endpoint that gives an absolute expiry", func(t *testing.T) {
		expires := now.Add(time.Hour)
		calls := 0
		fetch := func(context.Context) (oauthapp.Token, error) {
			calls++

			return oauthapp.Token{
				Value:   "abs",
				Expires: expires,
			}, nil
		}
		tokens := &oauthapp.Tokens{Now: func() time.Time { return now }}

		t.Run("THEN it is renewed five minutes before it", func(t *testing.T) {
			_, _ = tokens.Get(ctx, key, fetch)
			now = expires.Add(-6 * time.Minute)
			_, _ = tokens.Get(ctx, key, fetch)

			assert.Equal(t, 1, calls)

			now = expires.Add(-5 * time.Minute)
			_, _ = tokens.Get(ctx, key, fetch)

			assert.Equal(t, 2, calls)
		})
	})

	t.Run("GIVEN an endpoint that fails", func(t *testing.T) {
		errDown := errors.New("down")
		iss := &issuer{err: errDown}
		tokens := &oauthapp.Tokens{}

		t.Run("WHEN a token is asked for", func(t *testing.T) {
			_, err := tokens.Get(ctx, key, iss.fetch)

			t.Run("THEN the error is the endpoint's, and nothing is cached", func(t *testing.T) {
				require.ErrorIs(t, err, errDown)

				iss.err = nil
				tok, err := tokens.Get(ctx, key, iss.fetch)
				require.NoError(t, err)
				assert.Equal(t, "token-1", tok)
			})
		})
	})
}

func TestTokens_oneRequestAtATime(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	key := oauthapp.Key("client-id", "client-secret")

	t.Run("GIVEN a slow token endpoint and no token yet", func(t *testing.T) {
		iss := &issuer{
			lifetime: time.Hour,
			delay:    50 * time.Millisecond,
		}
		tokens := &oauthapp.Tokens{}

		t.Run("WHEN eight callers ask at once", func(t *testing.T) {
			var wg sync.WaitGroup

			for range 8 {
				wg.Go(func() {
					tok, err := tokens.Get(ctx, key, iss.fetch)
					assert.NoError(t, err)
					assert.Equal(t, "token-1", tok)
				})
			}

			wg.Wait()

			t.Run("THEN they share one request", func(t *testing.T) {
				assert.Equal(t, int32(1), iss.issued.Load())
			})
		})
	})

	t.Run("GIVEN a token every caller holds when the service refuses it", func(t *testing.T) {
		iss := &issuer{
			lifetime: time.Hour,
			delay:    10 * time.Millisecond,
		}
		tokens := &oauthapp.Tokens{}
		refused, err := tokens.Get(ctx, key, iss.fetch)
		require.NoError(t, err)

		t.Run("WHEN eight callers drop it and ask again", func(t *testing.T) {
			var wg sync.WaitGroup

			for range 8 {
				wg.Go(func() {
					tokens.Drop(key, refused)

					tok, err := tokens.Get(ctx, key, iss.fetch)
					assert.NoError(t, err)
					assert.NotEqual(t, refused, tok)
				})
			}

			wg.Wait()

			t.Run("THEN it is renewed once, not once per caller", func(t *testing.T) {
				assert.Equal(t, int32(2), iss.issued.Load())
			})
		})
	})
}

func TestTokens_withRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	key := oauthapp.Key("client-id", "client-secret")
	refusedBy := func(bad map[string]bool) func(string) error {
		return func(tok string) error {
			if bad[tok] {
				return fmt.Errorf("HTTP 401: %w", apiclient.ErrTokenRejected)
			}

			return nil
		}
	}

	t.Run("GIVEN a cached token the service no longer accepts", func(t *testing.T) {
		iss := &issuer{lifetime: time.Hour}
		tokens := &oauthapp.Tokens{}
		_, _ = tokens.Get(ctx, key, iss.fetch)

		t.Run("WHEN a call is made with it", func(t *testing.T) {
			var used []string

			err := tokens.WithRetry(ctx, key, iss.fetch, func(tok string) error {
				used = append(used, tok)
				return refusedBy(map[string]bool{"token-1": true})(tok)
			})

			t.Run("THEN a new token is asked for once and the call made again", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, []string{"token-1", "token-2"}, used)
			})
		})
	})

	t.Run("GIVEN a service that refuses even a new token", func(t *testing.T) {
		iss := &issuer{lifetime: time.Hour}
		tokens := &oauthapp.Tokens{}

		t.Run("WHEN a call is made", func(t *testing.T) {
			calls := 0
			err := tokens.WithRetry(ctx, key, iss.fetch, func(tok string) error {
				calls++
				return refusedBy(map[string]bool{"token-1": true, "token-2": true})(tok)
			})

			t.Run("THEN it gives up after one renewal, still a rejected token", func(t *testing.T) {
				require.ErrorIs(t, err, apiclient.ErrTokenRejected)
				assert.Equal(t, 2, calls)
			})
		})
	})

	t.Run("GIVEN a call that fails for another reason", func(t *testing.T) {
		errOther := errors.New("HTTP 404")
		iss := &issuer{lifetime: time.Hour}
		tokens := &oauthapp.Tokens{}

		t.Run("THEN it is not retried", func(t *testing.T) {
			calls := 0
			err := tokens.WithRetry(ctx, key, iss.fetch, func(string) error { calls++; return errOther })
			require.ErrorIs(t, err, errOther)
			assert.Equal(t, 1, calls)
		})
	})
}

func TestKey(t *testing.T) {
	t.Run("THEN it does not hold the secret and tells pairs apart", func(t *testing.T) {
		k := oauthapp.Key("id", "s3cret")
		assert.NotContains(t, k, "s3cret")
		assert.Equal(t, k, oauthapp.Key("id", "s3cret"))
		assert.NotEqual(t, k, oauthapp.Key("id", "other"))
		assert.NotEqual(t, oauthapp.Key("ab", "c"), oauthapp.Key("a", "bc"))
	})
}
