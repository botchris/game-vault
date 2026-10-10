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
}

func TestFieldsRepository_storedDocumentItCannotRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// withStored returns a repository whose custom fields row holds raw.
	withStored := func(t *testing.T, raw string) *SettingsRepository {
		t.Helper()

		repo := NewSettingsRepository(openTest(t))
		_, err := repo.db.conn(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ('fields', ?, '')`, raw)
		require.NoError(t, err)

		return repo
	}

	t.Run("GIVEN a document written by a newer Game Vault WHEN the fields are read", func(t *testing.T) {
		_, err := withStored(t, `{"v":2,"fields":[]}`).Fields(ctx)

		t.Run("THEN it is refused as newer", func(t *testing.T) {
			assert.ErrorIs(t, err, errNewerDocument)
		})
	})

	t.Run("GIVEN a document that is not valid JSON WHEN the fields are read", func(t *testing.T) {
		_, err := withStored(t, `{"v":1,"fields":[`).Fields(ctx)

		t.Run("THEN the error says the custom fields could not be read", func(t *testing.T) {
			require.Error(t, err)
			assert.Contains(t, err.Error(), "reading custom fields")
		})
	})
}
