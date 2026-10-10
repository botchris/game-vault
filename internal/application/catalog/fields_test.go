package catalog_test

import (
	"context"
	"errors"
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

func TestCatalogValidatesFieldValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	defs := sqlite.NewSettingsRepository(db)
	set := field.NewSet(nil)
	review, _ := set.Add(field.Definition{
		Name:  "Review",
		Type:  field.TypeText,
		Scope: field.ScopeGame,
	})
	sealed, _ := set.Add(field.Definition{
		Name:  "Sealed",
		Type:  field.TypeBool,
		Scope: field.ScopeCopy,
		Kinds: []game.Kind{game.KindPhysical},
	})
	require.NoError(t, defs.SaveFields(ctx, set))

	svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, nil, nil, defs)
	yes := true

	t.Run("GIVEN a game with a valid game value and a copy with a valid copy value", func(t *testing.T) {
		g, err := svc.CreateGame(ctx, game.Info{
			Title:  "Halo 3",
			Fields: game.FieldValues{review.ID: {Text: " Great "}},
		},
			[]game.CopyDetails{{Kind: game.KindPhysical}}, game.FieldValues{sealed.ID: {Bool: &yes}})
		require.NoError(t, err)

		t.Run("THEN both are stored, normalized", func(t *testing.T) {
			assert.Equal(t, "Great", g.Fields()[review.ID].Text)
			assert.True(t, *g.Copies()[0].Fields[sealed.ID].Bool)
		})

		t.Run("WHEN a key gets the physical-only field THEN it is refused, naming the field", func(t *testing.T) {
			_, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{Kind: game.KindKey}, game.FieldValues{sealed.ID: {Bool: &yes}})

			var v *field.ValidationError
			require.True(t, errors.As(err, &v))
			assert.Contains(t, err.Error(), "Sealed")
		})

		t.Run("WHEN the game gets a value for a field that does not exist THEN it is refused", func(t *testing.T) {
			info := g.Info()
			info.Fields = game.FieldValues{"gone": {Text: "x"}}
			_, err := svc.UpdateGame(ctx, g.ID(), info)
			assert.Error(t, err)
		})

		t.Run("WHEN a copy's details are updated without its values THEN its values are cleared", func(t *testing.T) {
			got, err := svc.UpdateCopy(ctx, g.ID(), g.Copies()[0].ID, game.CopyDetails{Kind: game.KindPhysical}, nil)
			require.NoError(t, err)
			assert.Empty(t, got.Copies()[0].Fields)
		})
	})

	t.Run("GIVEN a new game whose key copy has a physical-only value", func(t *testing.T) {
		t.Run("WHEN it is created", func(t *testing.T) {
			_, err := svc.CreateGame(ctx, game.Info{Title: "Halo 2"},
				[]game.CopyDetails{{Kind: game.KindKey}}, game.FieldValues{sealed.ID: {Bool: &yes}})

			t.Run("THEN it is refused, naming the field", func(t *testing.T) {
				var v *field.ValidationError
				require.ErrorAs(t, err, &v)
				assert.Contains(t, err.Error(), "Sealed")
			})

			t.Run("AND no game was stored", func(t *testing.T) {
				list, err := sqlite.NewGameRepository(db).List(ctx)
				require.NoError(t, err)

				for _, g := range list {
					assert.NotEqual(t, "Halo 2", g.Title())
				}
			})
		})
	})
}

// racingTx is a port.TxManager that runs a hook once, just before the next transaction begins: it
// stands for another request committing between a use case's start and its write.
type racingTx struct {
	db     *sqlite.DB
	before func()
}

func (r *racingTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if hook := r.before; hook != nil {
		r.before = nil

		hook()
	}

	return r.db.WithinTx(ctx, fn)
}

func TestCatalogFieldValues_fieldDeletedBeforeTheWrite(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// setup returns a catalog whose next transaction races with the deletion of both fields, a game with one copy, and the
	// ids of a game field and a copy field.
	setup := func(t *testing.T) (*catalog.Service, *game.Game, string, string) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		defs, games := sqlite.NewSettingsRepository(db), sqlite.NewGameRepository(db)
		fieldSvc := fields.NewService(defs, games, db, time.Now)

		review, err := fieldSvc.Create(ctx, field.Definition{
			Name:  "Review",
			Type:  field.TypeText,
			Scope: field.ScopeGame,
		})
		require.NoError(t, err)

		given, err := fieldSvc.Create(ctx, field.Definition{
			Name:  "Given to",
			Type:  field.TypeText,
			Scope: field.ScopeCopy,
		})
		require.NoError(t, err)

		tx := &racingTx{db: db}
		svc := catalog.NewService(games, tx, time.Now, nil, nil, defs)

		g, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3"}, []game.CopyDetails{{Kind: game.KindPhysical}})
		require.NoError(t, err)

		// The next write races with the deletion of both fields.
		tx.before = func() {
			_, _, err := fieldSvc.Delete(ctx, review.ID)
			require.NoError(t, err)
			_, _, err = fieldSvc.Delete(ctx, given.ID)
			require.NoError(t, err)
		}

		return svc, g, review.ID, given.ID
	}

	cases := map[string]func(svc *catalog.Service, g *game.Game, gameField, copyField string) error{
		"a game is created with a value": func(svc *catalog.Service, _ *game.Game, gameField, _ string) error {
			_, err := svc.CreateGame(ctx, game.Info{
				Title:  "Halo 2",
				Fields: game.FieldValues{gameField: {Text: "Great"}},
			}, nil)

			return err
		},
		"a game is updated with a value": func(svc *catalog.Service, g *game.Game, gameField, _ string) error {
			info := g.Info()
			info.Fields = game.FieldValues{gameField: {Text: "Great"}}
			_, err := svc.UpdateGame(ctx, g.ID(), info)

			return err
		},
		"a copy is added with a value": func(svc *catalog.Service, g *game.Game, _, copyField string) error {
			_, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{Kind: game.KindPhysical}, game.FieldValues{copyField: {Text: "Someone"}})
			return err
		},
		"a copy is updated with a value": func(svc *catalog.Service, g *game.Game, _, copyField string) error {
			_, err := svc.UpdateCopy(ctx, g.ID(), g.Copies()[0].ID, game.CopyDetails{Kind: game.KindPhysical}, game.FieldValues{copyField: {Text: "Someone"}})
			return err
		},
	}

	t.Run("GIVEN fields deleted after the use case starts but before its transaction", func(t *testing.T) {
		for name, write := range cases {
			t.Run("WHEN "+name, func(t *testing.T) {
				svc, g, gameField, copyField := setup(t)
				err := write(svc, g, gameField, copyField)

				t.Run("THEN it is refused because the field no longer exists", func(t *testing.T) {
					var v *field.ValidationError
					require.True(t, errors.As(err, &v), "got %v", err)
				})
			})
		}
	})
}
