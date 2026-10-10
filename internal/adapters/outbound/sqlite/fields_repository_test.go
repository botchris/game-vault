package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

func TestFieldsRepository(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	repo := NewSettingsRepository(openTest(t))

	t.Run("GIVEN no fields saved THEN the set is empty", func(t *testing.T) {
		s, err := repo.Fields(ctx)
		require.NoError(t, err)
		assert.Empty(t, s.Definitions())
	})

	t.Run("GIVEN fields of several kinds WHEN saved THEN they read back in order", func(t *testing.T) {
		s := field.NewSet(nil)
		_, err := s.Add(field.Definition{
			Name:  "Sealed",
			Type:  field.TypeBool,
			Scope: field.ScopeCopy,
			Kinds: []game.Kind{game.KindPhysical},
		})
		require.NoError(t, err)
		_, err = s.Add(field.Definition{
			Name:    "Awards",
			Type:    field.TypeMultiList,
			Scope:   field.ScopeGame,
			Choices: []field.Choice{{Name: "GOTY"}},
		})
		require.NoError(t, err)
		_, err = s.Add(field.Definition{
			Name:     "Weight",
			Type:     field.TypeNumber,
			Scope:    field.ScopeCopy,
			Decimals: 2,
			Unit:     "g",
		})
		require.NoError(t, err)
		require.NoError(t, repo.SaveFields(ctx, s))

		got, err := repo.Fields(ctx)
		require.NoError(t, err)
		assert.Equal(t, s.Definitions(), got.Definitions())
	})

	t.Run("GIVEN a document written by a newer Game Vault THEN it is refused", func(t *testing.T) {
		_, err := repo.db.conn(ctx).ExecContext(ctx, `UPDATE settings SET value = '{"v":2,"fields":[]}' WHERE key = 'fields'`)
		require.NoError(t, err)

		_, err = repo.Fields(ctx)
		assert.ErrorIs(t, err, errNewerDocument)
	})
}
