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
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

func TestConsolidate(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	library := func(ext, title string) game.ImportedCopy {
		return game.ImportedCopy{
			ExternalID: ext,
			Title:      title,
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: "Steam",
			},
		}
	}
	listed := func(copies ...game.ImportedCopy) func(context.Context) ([]game.ImportedCopy, error) {
		return func(context.Context) ([]game.ImportedCopy, error) { return copies, nil }
	}

	t.Run("GIVEN an empty catalog", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		games := sqlite.NewGameRepository(db)
		cache := &covers{}
		svc := catalog.NewService(games, db, catalog.WithCoverCache(cache))

		t.Run("WHEN a source imports two games", func(t *testing.T) {
			res, err := svc.Consolidate(ctx, "src1", listed(library("x:1", "Halo 3"), library("x:2", "Celeste")))
			require.NoError(t, err)

			t.Run("THEN they are saved and their new editions' covers dropped", func(t *testing.T) {
				assert.Equal(t, 2, res.GamesCreated)
				assert.Equal(t, 2, res.CopiesAdded)

				all, err := games.List(ctx)
				require.NoError(t, err)
				assert.Len(t, all, 2)
				assert.Len(t, cache.editions, 2)
			})

			t.Run("AND the counts and warnings go to the report", func(t *testing.T) {
				report := source.SyncReport{Warnings: []string{"from the fetch"}}
				res.Warnings = []string{"from the consolidation"}
				res.AddTo(&report)

				assert.Equal(t, 2, report.CopiesAdded)
				assert.Equal(t, 2, report.GamesCreated)
				assert.Equal(t, []string{"from the fetch", "from the consolidation"}, report.Warnings)
			})
		})

		t.Run("WHEN the source withdraws one of them", func(t *testing.T) {
			withdrawn := library("x:2", "Celeste")
			withdrawn.Withdrawn = true

			res, err := svc.Consolidate(ctx, "src1", listed(withdrawn))
			require.NoError(t, err)

			t.Run("THEN the game left without copies is deleted, and its cached files dropped", func(t *testing.T) {
				require.Len(t, res.Emptied, 1)
				_, err := games.Get(ctx, res.Emptied[0])
				require.ErrorIs(t, err, game.ErrGameNotFound)
				assert.Equal(t, []game.ID{res.Emptied[0]}, cache.invalidated)
			})
		})

		t.Run("WHEN reading what to import fails", func(t *testing.T) {
			cache.editions = nil
			boom := errors.New("boom")

			_, err := svc.Consolidate(ctx, "src1", func(context.Context) ([]game.ImportedCopy, error) { return nil, boom })

			t.Run("THEN nothing is saved and no cover is dropped", func(t *testing.T) {
				require.ErrorIs(t, err, boom)

				all, err := games.List(ctx)
				require.NoError(t, err)
				assert.Len(t, all, 1)
				assert.Empty(t, cache.editions)
			})
		})
	})
}
