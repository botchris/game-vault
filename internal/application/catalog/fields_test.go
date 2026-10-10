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
}
