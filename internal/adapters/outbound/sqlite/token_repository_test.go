package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/auth"
)

func TestAccessTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	r := NewAuthRepository(openTest(t))
	t0 := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	older := auth.AccessToken{
		ID:        "t1",
		Name:      "Extension, laptop",
		Hash:      auth.HashToken("gvt_one"),
		CreatedAt: t0,
	}
	newer := auth.AccessToken{
		ID:        "t2",
		Name:      "Extension, phone",
		Hash:      auth.HashToken("gvt_two"),
		CreatedAt: t0.Add(time.Hour),
	}

	t.Run("GIVEN two saved tokens", func(t *testing.T) {
		require.NoError(t, r.SaveAccessToken(ctx, older))
		require.NoError(t, r.SaveAccessToken(ctx, newer))

		t.Run("WHEN they are listed", func(t *testing.T) {
			got, err := r.AccessTokens(ctx)
			require.NoError(t, err)

			t.Run("THEN the newest comes first, and neither was used", func(t *testing.T) {
				assert.Equal(t, []auth.AccessToken{newer, older}, got)
			})
		})

		t.Run("WHEN one is found by its hash and touched", func(t *testing.T) {
			got, err := r.AccessTokenByHash(ctx, auth.HashToken("gvt_one"))
			require.NoError(t, err)
			require.NoError(t, r.TouchAccessToken(ctx, got.ID, t0.Add(2*time.Hour)))

			again, err := r.AccessTokenByHash(ctx, auth.HashToken("gvt_one"))
			require.NoError(t, err)

			t.Run("THEN it records when it was last used", func(t *testing.T) {
				assert.Equal(t, "Extension, laptop", got.Name)
				assert.Equal(t, t0.Add(2*time.Hour), again.LastUsedAt)
			})
		})

		t.Run("WHEN a hash matches no token", func(t *testing.T) {
			_, err := r.AccessTokenByHash(ctx, auth.HashToken("gvt_unknown"))

			t.Run("THEN it is not found", func(t *testing.T) {
				assert.ErrorIs(t, err, auth.ErrNotFound)
			})
		})

		t.Run("WHEN one is deleted", func(t *testing.T) {
			require.NoError(t, r.DeleteAccessToken(ctx, "t1"))
			_, err := r.AccessTokenByHash(ctx, auth.HashToken("gvt_one"))

			t.Run("THEN it is gone, and deleting it again says so", func(t *testing.T) {
				assert.ErrorIs(t, err, auth.ErrNotFound)
				assert.ErrorIs(t, r.DeleteAccessToken(ctx, "t1"), auth.ErrNotFound)
			})
		})
	})
}
