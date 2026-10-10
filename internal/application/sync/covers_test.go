package sync_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// withdrawing is a library source that reports one game, and withdraws it once withdraw is set.
type withdrawing struct{ withdraw bool }

func (w *withdrawing) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "wd",
		Name: "Withdrawing",
	}
}

func (w *withdrawing) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	return []game.ImportedCopy{{
		ExternalID: "wd:1",
		Withdrawn:  w.withdraw,
		Title:      "Halo 3",
		Details: game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		},
	}}, nil, nil
}

func (w *withdrawing) Test(context.Context, source.Settings) error { return nil }

// covers records the games whose cached cover and details were dropped, and the editions whose
// cached cover was dropped ("<game id>|<system>").
type covers struct {
	invalidated []game.ID
	editions    []string
}

func (c *covers) Invalidate(_ context.Context, id game.ID) error {
	c.invalidated = append(c.invalidated, id)

	return nil
}

func (c *covers) InvalidateEdition(_ context.Context, id game.ID, system string) error {
	c.editions = append(c.editions, string(id)+"|"+system)
	return nil
}

// photographCover gives the source's copy of the only game a photo used as the cover, and adds a
// manual copy so the game survives when the source's copy goes. It returns the game and the system
// of the photographed copy.
func photographCover(ctx context.Context, t *testing.T, games game.Repository) (game.ID, string) {
	t.Helper()

	list, err := games.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	g := list[0]
	photo := game.PhotoID(fmt.Sprintf("%064x", 1))
	_, err = g.AddPhotos(g.Copies()[0].ID, []game.Photo{{ID: photo}}, time.Now())
	require.NoError(t, err)

	_, err = g.AddCopy(game.CopyDetails{Kind: game.KindPhysical}, time.Now())
	require.NoError(t, err)

	require.NoError(t, g.SetEditionCover(g.Copies()[0].System(), game.EditionCover{Photo: photo}, time.Now()))
	require.NoError(t, games.Save(ctx, g))

	return g.ID(), g.Copies()[0].System()
}

func TestCoverPhotoLeavesWithTheSourcesCopy(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, how := range []string{"a scan withdraws it", "the source is deleted with its copies"} {
		t.Run("GIVEN a game whose cover is a photo of a copy a source manages, WHEN "+how, func(t *testing.T) {
			db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
			require.NoError(t, err)
			t.Cleanup(func() { db.Close() })

			games := sqlite.NewGameRepository(db)
			p := &withdrawing{}
			cache := &covers{}
			svc := sync.NewService(sqlite.NewSourceRepository(db), games, db, cache, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), p)

			v, err := svc.Create(ctx, "wd", source.Config{Enabled: true})
			require.NoError(t, err)
			_, err = svc.Sync(ctx, v.ID())
			require.NoError(t, err)

			id, system := photographCover(ctx, t, games)
			cache.editions = nil

			if how == "a scan withdraws it" {
				p.withdraw = true
				_, err = svc.Sync(ctx, v.ID())
			} else {
				err = svc.Delete(ctx, v.ID(), true)
			}

			require.NoError(t, err)

			t.Run("THEN the game has no cover photo and its cached cover is dropped", func(t *testing.T) {
				g, err := games.Get(ctx, id)
				require.NoError(t, err)
				assert.Empty(t, g.Covers())
				assert.Contains(t, cache.editions, string(id)+"|"+system)
			})
		})
	}
}

// consoleSource is a library source whose game is for PS5, and which also reports the game on Steam
// once steam is set.
type consoleSource struct{ steam bool }

func (*consoleSource) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "console",
		Name: "Console store",
	}
}

func (s *consoleSource) Fetch(context.Context, source.Settings) ([]game.ImportedCopy, []string, error) {
	copies := []game.ImportedCopy{{
		ExternalID: "psn:1",
		Title:      "Astro Bot",
		System:     "PS5",
		Details: game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PlayStation Store",
			Status:   game.StatusOwned,
		},
	}}
	if s.steam {
		copies = append(copies, game.ImportedCopy{
			ExternalID: "steam:1",
			Title:      "Astro Bot",
			Details: game.CopyDetails{
				Kind:     game.KindLibrary,
				Platform: "Steam",
				Status:   game.StatusOwned,
			},
		})
	}

	return copies, nil, nil
}

func (*consoleSource) Test(context.Context, source.Settings) error { return nil }

func TestSync_editions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a source whose game is for PS5", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		games := sqlite.NewGameRepository(db)
		p := &consoleSource{}
		cache := &covers{}
		svc := sync.NewService(sqlite.NewSourceRepository(db), games, db, cache, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), p)

		v, err := svc.Create(ctx, "console", source.Config{Enabled: true})
		require.NoError(t, err)

		t.Run("WHEN it is scanned", func(t *testing.T) {
			_, err := svc.Sync(ctx, v.ID())
			require.NoError(t, err)

			list, err := games.List(ctx)
			require.NoError(t, err)
			require.Len(t, list, 1)

			g := list[0]

			t.Run("THEN the copy is on PS5, as the source says, and the new edition's cached cover is dropped", func(t *testing.T) {
				assert.Equal(t, "PS5", g.Copies()[0].System())
				assert.Equal(t, []string{string(g.ID()) + "|PS5"}, cache.editions)
			})

			t.Run("WHEN the user moves it to PS4 and the source is scanned again", func(t *testing.T) {
				d := g.Copies()[0].CopyDetails
				d.System = "PS4"
				_, err := g.UpdateCopy(g.Copies()[0].ID, d, time.Now())
				require.NoError(t, err)
				require.NoError(t, games.Save(ctx, g))

				cache.editions = nil
				_, err = svc.Sync(ctx, v.ID())
				require.NoError(t, err)

				got, err := games.Get(ctx, g.ID())
				require.NoError(t, err)

				t.Run("THEN the user's system survives the scan, and no cached cover is dropped", func(t *testing.T) {
					assert.Equal(t, "PS4", got.Copies()[0].System())
					assert.Equal(t, "PS5", got.Copies()[0].SourceSystem)
					assert.Empty(t, cache.editions)
				})
			})

			t.Run("WHEN a scan brings the game on Steam too THEN only the new PC edition's cached cover is dropped", func(t *testing.T) {
				p.steam = true
				cache.editions = nil
				_, err := svc.Sync(ctx, v.ID())
				require.NoError(t, err)
				assert.Equal(t, []string{string(g.ID()) + "|PC"}, cache.editions)
			})
		})
	})
}
