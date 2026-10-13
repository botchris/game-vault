package igdb

import (
	"context"
	"fmt"
	"sync"
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

		t.Run("WHEN another query follows at once", func(t *testing.T) {
			sent := len(f.sent("games"))

			var out []map[string]any

			err := c.query(ctx, goodSettings, "games", "fields id; limit 1;", &out)

			t.Run("THEN it is rate-limited without reaching IGDB, which is left alone for a while", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Len(t, f.sent("games"), sent)
			})
		})
	})
}

func TestClient_credentialsStayOutOfErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a token endpoint that hangs up", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.set(func(f *fakeIGDB) { f.hangUp = true })
		c := f.client(time.Now)

		t.Run("WHEN the connection is tested and a query is sent", func(t *testing.T) {
			testErr := c.Test(ctx, goodSettings)

			var out []map[string]any

			queryErr := c.query(ctx, goodSettings, "games", "fields id;", &out)

			t.Run("THEN neither error, as warnings and logs show it, carries the secret", func(t *testing.T) {
				for _, err := range []error{testErr, queryErr} {
					require.Error(t, err)
					assert.NotContains(t, err.Error(), "good-secret")
					assert.NotContains(t, fmt.Sprintf("%+v", err), "good-secret")
					assert.Contains(t, err.Error(), "try again")
				}
			})

			t.Run("AND the credentials were sent in the body, never in the URL", func(t *testing.T) {
				assert.Empty(t, f.credentialLeaks())
			})
		})
	})
}

func TestClient_oneTokenAtATime(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	parallel := func(c *Client) []error {
		errs := make([]error, 8)

		var wg sync.WaitGroup

		for i := range errs {
			wg.Go(func() {
				var out []map[string]any

				errs[i] = c.query(ctx, goodSettings, "games", "fields id; limit 1;", &out)
			})
		}

		wg.Wait()

		return errs
	}

	t.Run("GIVEN a slow token endpoint and no token yet", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.set(func(f *fakeIGDB) { f.tokenDelay = 100 * time.Millisecond })
		c := f.client(time.Now)

		t.Run("WHEN 8 queries are sent at once", func(t *testing.T) {
			errs := parallel(c)

			t.Run("THEN they share one token request", func(t *testing.T) {
				for _, err := range errs {
					require.NoError(t, err)
				}

				assert.Equal(t, 1, f.tokens())
			})
		})

		t.Run("WHEN IGDB refuses the token they all hold", func(t *testing.T) {
			f.revokeTokens()

			errs := parallel(c)

			t.Run("THEN it is renewed once, not once per query", func(t *testing.T) {
				for _, err := range errs {
					require.NoError(t, err)
				}

				assert.Equal(t, 2, f.tokens())
			})
		})
	})

	t.Run("GIVEN Twitch announces no lifetime for its tokens", func(t *testing.T) {
		f := newFakeIGDB(t)
		f.set(func(f *fakeIGDB) { f.expiresIn = "" })

		now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		c := f.client(func() time.Time { return now })

		t.Run("WHEN queries follow each other for most of an hour", func(t *testing.T) {
			var out []map[string]any
			require.NoError(t, c.query(ctx, goodSettings, "games", "fields id;", &out))

			now = now.Add(50 * time.Minute)

			require.NoError(t, c.query(ctx, goodSettings, "games", "fields id;", &out))

			t.Run("THEN the token is kept for an hour rather than asked for every time", func(t *testing.T) {
				assert.Equal(t, 1, f.tokens())
			})
		})
	})
}

func TestClient_queryErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN IGDB refuses a malformed query", func(t *testing.T) {
		f := newFakeIGDB(t)
		c := f.client(time.Now)

		t.Run("WHEN it is sent", func(t *testing.T) {
			var out []map[string]any

			err := c.query(ctx, goodSettings, "games", `search "x"; fields id; where bogus;`, &out)

			t.Run("THEN the error keeps IGDB's explanation and says what to do", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "HTTP 400")
				assert.Contains(t, err.Error(), "Syntax Error")
				assert.Contains(t, err.Error(), "try again later; if it persists, report this message")
				assert.NotErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})
}
