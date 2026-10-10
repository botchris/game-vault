package transfer_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/csvfile"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/transfer"
	"gamevault/internal/domain/game"
)

// covers records the editions whose cached cover was dropped ("<system>"; the game is not told apart).
type covers struct {
	editions []string
}

func (c *covers) Invalidate(context.Context, game.ID) error { return nil }

func (c *covers) InvalidateEdition(_ context.Context, _ game.ID, system string) error {
	c.editions = append(c.editions, system)

	return nil
}

func TestImport_coverCache(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game with a digital PC edition and a PS3 disc", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		cache := &covers{}
		svc := transfer.NewService(sqlite.NewGameRepository(db), db, sqlite.NewSettingsRepository(db), time.Now, csvfile.Codec{}, cache)

		_, err = svc.Import(ctx, []byte("title,platform,kind\nHalo 3,Steam,library\nHalo 3,PS3,physical\n"))
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"PC", "PS3"}, cache.editions, "a new game's editions all count")

		cache.editions = nil

		t.Run("WHEN a file adds a physical PC copy", func(t *testing.T) {
			_, err := svc.Import(ctx, []byte("title,platform,kind\nHalo 3,Steam,library\nHalo 3,PS3,physical\nHalo 3,PC,physical\n"))
			require.NoError(t, err)

			t.Run("THEN only the PC edition's cached cover is dropped", func(t *testing.T) {
				assert.Equal(t, []string{"PC"}, cache.editions)
			})
		})

		t.Run("WHEN the same file is imported again", func(t *testing.T) {
			cache.editions = nil

			_, err := svc.Import(ctx, []byte("title,platform,kind\nHalo 3,Steam,library\nHalo 3,PS3,physical\nHalo 3,PC,physical\n"))
			require.NoError(t, err)

			t.Run("THEN no cached cover is dropped", func(t *testing.T) {
				assert.Empty(t, cache.editions)
			})
		})
	})
}
