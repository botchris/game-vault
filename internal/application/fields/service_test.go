package fields_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/fields"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

func TestFieldsService(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	defs, games := sqlite.NewSettingsRepository(db), sqlite.NewGameRepository(db)
	svc := fields.NewService(defs, games, db)
	cat := catalog.NewService(games, db, time.Now, nil, nil, defs)

	awards, err := svc.Create(ctx, field.Definition{
		Name:    "Awards",
		Type:    field.TypeMultiList,
		Scope:   field.ScopeGame,
		Choices: []field.Choice{{Name: "GOTY"}, {Name: "Best art"}},
	})
	require.NoError(t, err)
	given, err := svc.Create(ctx, field.Definition{
		Name:  "Given to",
		Type:  field.TypeText,
		Scope: field.ScopeCopy,
	})
	require.NoError(t, err)

	goty, art := awards.Choices[0].ID, awards.Choices[1].ID
	_, err = cat.CreateGame(ctx, game.Info{
		Title:  "A",
		Fields: game.FieldValues{awards.ID: {Choices: []string{goty, art}}},
	},
		[]game.CopyDetails{{Kind: game.KindPhysical}}, game.FieldValues{given.ID: {Text: "Someone"}})
	require.NoError(t, err)
	_, err = cat.CreateGame(ctx, game.Info{
		Title:  "B",
		Fields: game.FieldValues{awards.ID: {Choices: []string{goty}}},
	}, nil)
	require.NoError(t, err)

	t.Run("GIVEN values in two games WHEN a value is merged into another", func(t *testing.T) {
		require.NoError(t, svc.RemoveChoice(ctx, awards.ID, goty, art))

		t.Run("THEN every game holds the target once and the list lost the value", func(t *testing.T) {
			list, err := games.List(ctx)
			require.NoError(t, err)

			for _, g := range list {
				assert.Equal(t, []string{art}, g.Fields()[awards.ID].Choices, g.Title())
			}

			defsNow, err := svc.List(ctx)
			require.NoError(t, err)
			assert.Len(t, defsNow[0].Choices, 1)
		})
	})

	t.Run("GIVEN a copy field with a value WHEN it is deleted", func(t *testing.T) {
		gamesN, copiesN, err := svc.Usage(ctx, given.ID)
		require.NoError(t, err)
		assert.Equal(t, 0, gamesN)
		assert.Equal(t, 1, copiesN)

		gamesN, copiesN, err = svc.Delete(ctx, given.ID)
		require.NoError(t, err)

		t.Run("THEN its values are gone and the counts said where they were", func(t *testing.T) {
			assert.Equal(t, 1, copiesN)
			assert.Equal(t, 0, gamesN)

			list, err := games.List(ctx)
			require.NoError(t, err)

			for _, g := range list {
				for _, c := range g.Copies() {
					assert.NotContains(t, c.Fields, given.ID)
				}
			}
		})
	})

	t.Run("GIVEN a multilist WHEN a value is typed that exists with another case THEN it is reused", func(t *testing.T) {
		c, err := svc.AddChoice(ctx, awards.ID, "best ART")
		require.NoError(t, err)
		assert.Equal(t, art, c.ID)
	})
}
