package catalog_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
)

func TestMergeAndMove_refuseAMoveToTheSameGame(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game with a copy", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		svc := catalog.NewService(sqlite.NewGameRepository(db), db)

		g, err := svc.CreateGame(ctx, game.Info{Title: "Example Quest"}, []game.CopyDetails{{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		}})
		require.NoError(t, err)

		t.Run("WHEN the game is merged into itself", func(t *testing.T) {
			_, err := svc.MergeGames(ctx, g.ID(), []game.ID{g.ID()})

			t.Run("THEN it is refused as invalid input", func(t *testing.T) {
				require.ErrorIs(t, err, catalog.ErrMergeIntoItself)
				assert.Equal(t, errkind.Invalid, errkind.Of(err))
			})
		})

		t.Run("WHEN its copy is moved to the game it is in", func(t *testing.T) {
			_, _, err := svc.MoveCopy(ctx, g.ID(), g.Copies()[0].ID, g.ID(), "")

			t.Run("THEN it is refused as invalid input", func(t *testing.T) {
				require.ErrorIs(t, err, catalog.ErrCopyAlreadyThere)
				assert.Equal(t, errkind.Invalid, errkind.Of(err))
			})
		})
	})
}
