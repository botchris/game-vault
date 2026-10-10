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

func TestFieldsService_updateWithStoredValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	defs, games := sqlite.NewSettingsRepository(db), sqlite.NewGameRepository(db)
	svc := fields.NewService(defs, games, db)
	cat := catalog.NewService(games, db, time.Now, nil, nil, defs)

	create := func(t *testing.T, d field.Definition) field.Definition {
		t.Helper()

		out, err := svc.Create(ctx, d)
		require.NoError(t, err)

		return out
	}

	weight := create(t, field.Definition{
		Name:  "Weight",
		Type:  field.TypeNumber,
		Scope: field.ScopeGame,
	})
	rating := create(t, field.Definition{
		Name:  "Rating",
		Type:  field.TypeNumber,
		Scope: field.ScopeGame,
	})
	price := create(t, field.Definition{
		Name:  "Paid",
		Type:  field.TypeMoney,
		Scope: field.ScopeGame,
	})
	sealed := create(t, field.Definition{
		Name:  "Sealed",
		Type:  field.TypeBool,
		Scope: field.ScopeCopy,
	})

	five, yes := int64(5), true
	_, err = cat.CreateGame(ctx, game.Info{
		Title: "Halo 3",
		Fields: game.FieldValues{
			weight.ID: {Number: &five},
			price.ID: {Money: &game.Money{
				Amount:   1999,
				Currency: "EUR",
			}},
		},
	},
		[]game.CopyDetails{{Kind: game.KindPhysical}}, game.FieldValues{sealed.ID: {Bool: &yes}})
	require.NoError(t, err)

	t.Run("GIVEN a number field with a value in a game", func(t *testing.T) {
		t.Run("WHEN its decimals change", func(t *testing.T) {
			d := weight
			d.Decimals = 2
			_, err := svc.Update(ctx, d)

			t.Run("THEN it is refused, naming the field and how many games hold a value", func(t *testing.T) {
				require.ErrorIs(t, err, fields.ErrValuesConflict)
				assert.Contains(t, err.Error(), "Weight")
				assert.Contains(t, err.Error(), "1 game")
				assert.Contains(t, err.Error(), "decimals")
			})
		})

		t.Run("WHEN it is renamed and gets a unit", func(t *testing.T) {
			d := weight
			d.Name = "Mass"
			d.Unit = "g"
			got, err := svc.Update(ctx, d)

			t.Run("THEN it is allowed", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "Mass", got.Name)
				assert.Equal(t, 0, got.Decimals)
			})
		})
	})

	t.Run("GIVEN a number field without values WHEN its decimals change THEN it is allowed", func(t *testing.T) {
		d := rating
		d.Decimals = 2
		got, err := svc.Update(ctx, d)
		require.NoError(t, err)
		assert.Equal(t, 2, got.Decimals)
	})

	t.Run("GIVEN a money field with a value in euros", func(t *testing.T) {
		t.Run("WHEN its currency becomes the euro THEN it is allowed", func(t *testing.T) {
			d := price
			d.Currency = "EUR"
			_, err := svc.Update(ctx, d)
			require.NoError(t, err)
		})

		t.Run("WHEN its currency becomes the dollar", func(t *testing.T) {
			d := price
			d.Currency = "USD"
			_, err := svc.Update(ctx, d)

			t.Run("THEN it is refused, naming the field and how many games do not fit", func(t *testing.T) {
				require.ErrorIs(t, err, fields.ErrValuesConflict)
				assert.Contains(t, err.Error(), "Paid")
				assert.Contains(t, err.Error(), "1 game")
			})
		})
	})

	t.Run("GIVEN a copy field for every kind with a value on a physical copy", func(t *testing.T) {
		t.Run("WHEN it is limited to keys", func(t *testing.T) {
			d := sealed
			d.Kinds = []game.Kind{game.KindKey}
			_, err := svc.Update(ctx, d)

			t.Run("THEN it is refused, because the physical copy would keep a value", func(t *testing.T) {
				require.ErrorIs(t, err, fields.ErrValuesConflict)
				assert.Contains(t, err.Error(), "Sealed")
			})
		})

		t.Run("WHEN it is limited to physical copies THEN it is allowed", func(t *testing.T) {
			d := sealed
			d.Kinds = []game.Kind{game.KindPhysical}
			_, err := svc.Update(ctx, d)
			require.NoError(t, err)
		})

		t.Run("WHEN it applies to every kind again THEN it is allowed", func(t *testing.T) {
			_, err := svc.Update(ctx, sealed)
			require.NoError(t, err)
		})
	})
}
