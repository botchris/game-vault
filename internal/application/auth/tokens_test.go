package auth_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	appauth "gamevault/internal/application/auth"
	"gamevault/internal/domain/auth"
)

// plainHasher stands in for bcrypt: these tests are about tokens, and bcrypt is slow on purpose.
type plainHasher struct{}

func (plainHasher) Hash(p string) (string, error) { return "plain:" + p, nil }

func (plainHasher) Verify(h, p string) bool { return h == "plain:"+p }

type noCerts struct{}

func (noCerts) SetCertificateValidation(auth.CertificateValidation) {}

// clock is a settable port.Clock.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func newService(t *testing.T, ctx context.Context, c *clock) *appauth.Service {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return appauth.NewService(sqlite.NewAuthRepository(db), sqlite.NewSettingsRepository(db), plainHasher{}, noCerts{}, c.Now,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func lastUsed(t *testing.T, ctx context.Context, svc *appauth.Service) time.Time {
	t.Helper()

	list, err := svc.AccessTokens(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, list)

	return list[0].LastUsedAt
}

func TestAccessTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := &clock{now: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
	svc := newService(t, ctx, c)

	t.Run("GIVEN a token created for the extension", func(t *testing.T) {
		tok, secret, err := svc.CreateAccessToken(ctx, "Extension, laptop")
		require.NoError(t, err)

		t.Run("THEN the secret is a gvt_ token, and the list shows the token by its hash only", func(t *testing.T) {
			assert.True(t, auth.IsAccessToken(secret))

			list, err := svc.AccessTokens(ctx)
			require.NoError(t, err)
			require.Len(t, list, 1)
			assert.Equal(t, tok.ID, list[0].ID)
			assert.Equal(t, auth.HashToken(secret), list[0].Hash)
			assert.True(t, list[0].LastUsedAt.IsZero())
		})

		t.Run("WHEN it authenticates a request", func(t *testing.T) {
			p, err := svc.AuthenticateToken(ctx, secret)
			require.NoError(t, err)

			t.Run("THEN the caller is the token", func(t *testing.T) {
				assert.Equal(t, auth.MethodToken, p.Method)
				assert.Equal(t, "Extension, laptop", p.Name)
				assert.Equal(t, tok.ID, p.TokenID)
				assert.Empty(t, p.UserID)
			})

			t.Run("AND its use is recorded", func(t *testing.T) {
				assert.Equal(t, c.now, lastUsed(t, ctx, svc))
			})
		})

		t.Run("WHEN it is used again within the minute", func(t *testing.T) {
			first := c.now
			c.now = c.now.Add(30 * time.Second)
			_, err := svc.AuthenticateToken(ctx, secret)
			require.NoError(t, err)

			t.Run("THEN the last use is not written again", func(t *testing.T) {
				assert.Equal(t, first, lastUsed(t, ctx, svc))
			})
		})

		t.Run("WHEN it is used in a later minute", func(t *testing.T) {
			c.now = c.now.Add(31 * time.Second)
			_, err := svc.AuthenticateToken(ctx, secret)
			require.NoError(t, err)

			t.Run("THEN the last use moves, kept to the minute", func(t *testing.T) {
				assert.Equal(t, time.Date(2026, 10, 12, 9, 1, 0, 0, time.UTC), lastUsed(t, ctx, svc))
			})
		})

		t.Run("WHEN it is revoked", func(t *testing.T) {
			require.NoError(t, svc.RevokeAccessToken(ctx, tok.ID))
			_, err := svc.AuthenticateToken(ctx, secret)

			t.Run("THEN it stops working at once", func(t *testing.T) {
				assert.ErrorIs(t, err, auth.ErrUnauthenticated)
			})

			t.Run("AND revoking it again says it is not there", func(t *testing.T) {
				assert.ErrorIs(t, svc.RevokeAccessToken(ctx, tok.ID), auth.ErrNotFound)
			})
		})
	})

	t.Run("GIVEN a name that breaks the rules", func(t *testing.T) {
		_, _, err := svc.CreateAccessToken(ctx, "  ")

		t.Run("THEN no token is created", func(t *testing.T) {
			var ve *auth.ValidationError
			assert.ErrorAs(t, err, &ve)
		})
	})
}

func TestAccessTokens_throttling(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := &clock{now: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
	svc := newService(t, ctx, c)
	_, secret, err := svc.CreateAccessToken(ctx, "Extension, laptop")
	require.NoError(t, err)

	t.Run("GIVEN ten wrong tokens", func(t *testing.T) {
		for i := range 10 {
			_, err := svc.AuthenticateToken(ctx, fmt.Sprintf("gvt_guess%d", i))
			require.ErrorIs(t, err, auth.ErrUnauthenticated)
		}

		t.Run("THEN even the right token waits", func(t *testing.T) {
			_, err := svc.AuthenticateToken(ctx, secret)
			assert.ErrorIs(t, err, auth.ErrTooManyAttempts)
		})

		t.Run("AND so does signing in with a password, since both count together", func(t *testing.T) {
			_, _, err := svc.Login(ctx, auth.Request{}, "admin", "correct horse")
			assert.ErrorIs(t, err, auth.ErrTooManyAttempts)
		})

		t.Run("WHEN the window has passed", func(t *testing.T) {
			c.now = c.now.Add(16 * time.Minute)
			_, err := svc.AuthenticateToken(ctx, secret)

			t.Run("THEN the right token works again", func(t *testing.T) {
				assert.NoError(t, err)
			})
		})
	})
}
