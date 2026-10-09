package catalog_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/domain/game"
)

// covers records the games whose cached cover was dropped.
type covers struct{ invalidated []game.ID }

func (c *covers) Invalidate(_ context.Context, id game.ID) error {
	c.invalidated = append(c.invalidated, id)

	return nil
}

func photoID(n int) game.PhotoID { return game.PhotoID(fmt.Sprintf("%064x", n)) }

func TestCoverPhotoCache(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// setup returns a service and a game with two copies; the first copy's photo is the cover.
	setup := func(t *testing.T) (*catalog.Service, *covers, *game.Game) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		cache := &covers{}
		svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, cache, nil)

		g, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3"}, []game.CopyDetails{{Kind: game.KindPhysical}, {Kind: game.KindPhysical}})
		require.NoError(t, err)

		g, err = svc.AddCopyPhotos(ctx, g.ID(), g.Copies()[0].ID, []game.Photo{{ID: photoID(1)}})
		require.NoError(t, err)

		g, err = svc.SetCoverPhoto(ctx, g.ID(), photoID(1))
		require.NoError(t, err)
		assert.Equal(t, []game.ID{g.ID()}, cache.invalidated, "choosing a cover photo drops the cached cover")

		cache.invalidated = nil

		return svc, cache, g
	}

	t.Run("GIVEN a game whose cover is a photo of its first copy", func(t *testing.T) {
		cases := map[string]func(svc *catalog.Service, g *game.Game) error{
			"the photo is removed": func(svc *catalog.Service, g *game.Game) error {
				_, err := svc.RemoveCopyPhoto(ctx, g.ID(), g.Copies()[0].ID, photoID(1))
				return err
			},
			"the copy is deleted": func(svc *catalog.Service, g *game.Game) error {
				_, err := svc.DeleteCopy(ctx, g.ID(), g.Copies()[0].ID)
				return err
			},
			"the copy is moved to a new game": func(svc *catalog.Service, g *game.Game) error {
				_, _, err := svc.MoveCopy(ctx, g.ID(), g.Copies()[0].ID, "", "Halo 3 (Limited)")
				return err
			},
			"a new custom cover URL is chosen": func(svc *catalog.Service, g *game.Game) error {
				info := g.Info()
				info.CoverURL = "https://example.test/halo.jpg"
				_, err := svc.UpdateGame(ctx, g.ID(), info)

				return err
			},
		}

		for what, change := range cases {
			t.Run("WHEN "+what, func(t *testing.T) {
				svc, cache, g := setup(t)
				require.NoError(t, change(svc, g))

				t.Run("THEN the game has no cover photo and its cached cover is dropped", func(t *testing.T) {
					got, err := svc.GetGame(ctx, g.ID())
					require.NoError(t, err)
					assert.Empty(t, got.CoverPhoto())
					assert.Contains(t, cache.invalidated, g.ID())
				})
			})
		}

		t.Run("WHEN only the title is edited", func(t *testing.T) {
			svc, _, g := setup(t)
			info := g.Info()
			info.CoverPhoto = ""
			info.Title = "Halo 3 (2007)"
			got, err := svc.UpdateGame(ctx, g.ID(), info)
			require.NoError(t, err)

			t.Run("THEN the cover photo stays", func(t *testing.T) {
				assert.Equal(t, photoID(1), got.CoverPhoto())
			})
		})
	})
}
