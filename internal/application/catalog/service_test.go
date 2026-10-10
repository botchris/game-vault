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

// covers records the games whose cached cover and details were dropped, the editions whose
// cached cover was dropped ("<game id>|<system>"), and the games whose cover cached before
// editions was dropped.
type covers struct {
	invalidated []game.ID
	editions    []string
	legacy      []game.ID
}

func (c *covers) Invalidate(_ context.Context, id game.ID) error {
	c.invalidated = append(c.invalidated, id)

	return nil
}

func (c *covers) InvalidateEdition(_ context.Context, id game.ID, system string) error {
	c.editions = append(c.editions, string(id)+"|"+system)
	return nil
}

func (c *covers) DropLegacyCover(_ context.Context, id game.ID) error {
	c.legacy = append(c.legacy, id)
	return nil
}

func photoID(n int) game.PhotoID { return game.PhotoID(fmt.Sprintf("%064x", n)) }

func TestEditionCoverCache(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// setup returns a service and a game with two PS3 discs; the first disc's photo is the PS3 cover.
	setup := func(t *testing.T) (*catalog.Service, *covers, *game.Game) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		cache := &covers{}
		svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, cache, nil, nil)

		g, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3"}, []game.CopyDetails{
			{
				Kind:     game.KindPhysical,
				Platform: "PS3",
			},
			{
				Kind:     game.KindPhysical,
				Platform: "PS3",
			},
		})
		require.NoError(t, err)

		g, err = svc.AddCopyPhotos(ctx, g.ID(), g.Copies()[0].ID, []game.Photo{{ID: photoID(1)}})
		require.NoError(t, err)

		g, err = svc.SetEditionCover(ctx, g.ID(), "PS3", game.EditionCover{Photo: photoID(1)})
		require.NoError(t, err)
		assert.Equal(t, []string{string(g.ID()) + "|PS3"}, cache.editions, "choosing a cover drops that edition's cached one")

		cache.editions = nil

		return svc, cache, g
	}

	t.Run("GIVEN a game whose PS3 cover is a photo of its first disc", func(t *testing.T) {
		cases := map[string]func(svc *catalog.Service, g *game.Game) error{
			"the photo is removed": func(svc *catalog.Service, g *game.Game) error {
				_, err := svc.RemoveCopyPhoto(ctx, g.ID(), g.Copies()[0].ID, photoID(1))
				return err
			},
			"the disc is deleted": func(svc *catalog.Service, g *game.Game) error {
				_, err := svc.DeleteCopy(ctx, g.ID(), g.Copies()[0].ID)
				return err
			},
			"the disc is moved to a new game": func(svc *catalog.Service, g *game.Game) error {
				_, _, err := svc.MoveCopy(ctx, g.ID(), g.Copies()[0].ID, "", "Halo 3 (Limited)")
				return err
			},
			"another cover URL is chosen for the edition": func(svc *catalog.Service, g *game.Game) error {
				_, err := svc.SetEditionCover(ctx, g.ID(), "PS3", game.EditionCover{URL: "https://example.test/halo.jpg"})
				return err
			},
		}

		for what, change := range cases {
			t.Run("WHEN "+what, func(t *testing.T) {
				svc, cache, g := setup(t)
				require.NoError(t, change(svc, g))

				t.Run("THEN the edition has no cover photo and the cached cover is dropped", func(t *testing.T) {
					got, err := svc.GetGame(ctx, g.ID())
					require.NoError(t, err)
					assert.Empty(t, got.Covers()["PS3"].Photo)
					assert.Contains(t, cache.editions, string(g.ID())+"|PS3")
					assert.Empty(t, cache.invalidated, "the game's details stay")
				})
			})
		}

		t.Run("WHEN only the title is edited THEN the cover photo stays", func(t *testing.T) {
			svc, _, g := setup(t)
			info := g.Info()
			info.Title = "Halo 3 (2007)"
			got, err := svc.UpdateGame(ctx, g.ID(), info)
			require.NoError(t, err)
			assert.Equal(t, photoID(1), got.Covers()["PS3"].Photo)
		})

		t.Run("WHEN a cover is chosen for a system the game has no copy on THEN it is refused, naming the system", func(t *testing.T) {
			svc, _, g := setup(t)
			_, err := svc.SetEditionCover(ctx, g.ID(), "Wii", game.EditionCover{URL: "https://example.test/wii.jpg"})

			var ve *game.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Contains(t, err.Error(), "Wii")
		})

		t.Run("WHEN an Xbox 360 disc is added THEN only the new edition's cached cover is dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			_, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "Xbox 360",
			}, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{string(g.ID()) + "|Xbox 360"}, cache.editions)
		})

		t.Run("WHEN a disc's notes change THEN no cached cover is dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			c := g.Copies()[1]
			d := c.CopyDetails
			d.Notes = "signed"
			_, err := svc.UpdateCopy(ctx, g.ID(), c.ID, d, nil)
			require.NoError(t, err)
			assert.Empty(t, cache.editions)
		})

		t.Run("WHEN a second PS3 disc is added THEN no cached cover is dropped: the edition's cover inputs are the same", func(t *testing.T) {
			svc, cache, g := setup(t)
			_, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "PS3",
			}, nil)
			require.NoError(t, err)
			assert.Empty(t, cache.editions)
		})

		t.Run("WHEN PS3 has no chosen cover and an override moves the second disc to PS4 THEN only the new PS4 edition's cover is dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			g, err := svc.SetEditionCover(ctx, g.ID(), "PS3", game.EditionCover{})
			require.NoError(t, err)

			cache.editions = nil
			c := g.Copies()[1]
			d := c.CopyDetails
			d.System = "PS4"
			_, err = svc.UpdateCopy(ctx, g.ID(), c.ID, d, nil)
			require.NoError(t, err)
			assert.Equal(t, []string{string(g.ID()) + "|PS4"}, cache.editions)
		})

		t.Run("WHEN an override moves the photographed disc to PS4 THEN the PS3 cover photo goes and the PS3 and PS4 covers are dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			c := g.Copies()[0]
			d := c.CopyDetails
			d.System = "PS4"
			got, err := svc.UpdateCopy(ctx, g.ID(), c.ID, d, nil)
			require.NoError(t, err)
			assert.Empty(t, got.Covers()["PS3"].Photo)
			assert.ElementsMatch(t, []string{string(g.ID()) + "|PS3", string(g.ID()) + "|PS4"}, cache.editions)
		})

		t.Run("WHEN the main edition is chosen THEN no cached cover is dropped", func(t *testing.T) {
			svc, cache, g := setup(t)
			got, err := svc.SetMainSystem(ctx, g.ID(), "PS3")
			require.NoError(t, err)
			assert.Equal(t, "PS3", got.MainSystem())
			assert.Empty(t, cache.editions)
			assert.Empty(t, cache.invalidated)
		})
	})

	t.Run("GIVEN a game without copies with a chosen cover", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		cache := &covers{}
		svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, cache, nil, nil)

		g, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3"}, nil)
		require.NoError(t, err)

		_, err = svc.SetEditionCover(ctx, g.ID(), "", game.EditionCover{URL: "https://example.test/halo.jpg"})
		require.NoError(t, err)
		assert.Equal(t, []string{string(g.ID()) + "|"}, cache.editions, "choosing the game's cover drops its cached one")

		t.Run("WHEN its first copy, a PS3 disc, is added THEN the cover moves to PS3 and both cached covers are dropped", func(t *testing.T) {
			cache.editions = nil
			got, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "PS3",
			}, nil)
			require.NoError(t, err)
			assert.Equal(t, "https://example.test/halo.jpg", got.Covers()["PS3"].URL)
			assert.ElementsMatch(t, []string{string(g.ID()) + "|", string(g.ID()) + "|PS3"}, cache.editions)
		})
	})

	t.Run("GIVEN a game with a PS3 disc and a Wii disc", func(t *testing.T) {
		svc, cache, g := setup(t)
		_, err := svc.AddCopy(ctx, g.ID(), game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "Wii",
		}, nil)
		require.NoError(t, err)

		t.Run("WHEN the Wii edition is made the main one THEN the cover cached before editions is dropped", func(t *testing.T) {
			got, err := svc.SetMainSystem(ctx, g.ID(), "Wii")
			require.NoError(t, err)
			assert.Equal(t, "Wii", got.MainEdition().System)
			assert.Equal(t, []game.ID{g.ID()}, cache.legacy, "the new main edition must not adopt the old one's image")
		})
	})

	t.Run("GIVEN a game with a single PS3 disc", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		cache := &covers{}
		svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, cache, nil, nil)

		g, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3"}, []game.CopyDetails{{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		}})
		require.NoError(t, err)

		t.Run("WHEN the disc is moved to a new game THEN everything cached for the emptied game is dropped", func(t *testing.T) {
			src, _, err := svc.MoveCopy(ctx, g.ID(), g.Copies()[0].ID, "", "Halo 3 (Limited)")
			require.NoError(t, err)
			assert.Nil(t, src)
			assert.Equal(t, []game.ID{g.ID()}, cache.invalidated)
		})
	})
}
