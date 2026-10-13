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
)

// errDiskFull is a database failure.
var errDiskFull = errors.New("database or disk is full")

// secondSaveFails is a game repository whose second Save fails.
type secondSaveFails struct {
	game.Repository

	saves int
}

func (r *secondSaveFails) Save(ctx context.Context, g *game.Game) error {
	r.saves++
	if r.saves == 2 {
		return errDiskFull
	}

	return r.Repository.Save(ctx, g)
}

func TestConsolidate_aSaveFailsMidway(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an empty catalog whose second game save fails", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		stored := sqlite.NewGameRepository(db)
		cache := &covers{}
		svc := catalog.NewService(&secondSaveFails{Repository: stored}, db, catalog.WithCoverCache(cache))

		t.Run("WHEN a source imports two games", func(t *testing.T) {
			res, err := svc.Consolidate(ctx, "src1", func(context.Context) ([]game.ImportedCopy, error) {
				return []game.ImportedCopy{
					{
						ExternalID: "x:1",
						Title:      "Halo 3",
						Details: game.CopyDetails{
							Kind:     game.KindLibrary,
							Platform: "Steam",
						},
					},
					{
						ExternalID: "x:2",
						Title:      "Celeste",
						Details: game.CopyDetails{
							Kind:     game.KindLibrary,
							Platform: "Steam",
						},
					},
				}, nil
			})

			t.Run("THEN it fails with nothing to report", func(t *testing.T) {
				require.ErrorIs(t, err, errDiskFull)
				assert.Equal(t, catalog.Consolidation{}, res)
			})

			t.Run("AND the first game's save is rolled back", func(t *testing.T) {
				all, err := stored.List(ctx)
				require.NoError(t, err)
				assert.Empty(t, all)
			})

			t.Run("AND no cached cover is dropped", func(t *testing.T) {
				assert.Empty(t, cache.editions)
				assert.Empty(t, cache.invalidated)
			})
		})
	})
}
