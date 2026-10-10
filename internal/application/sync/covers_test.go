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

// covers records the games whose cached cover was dropped.
type covers struct{ invalidated []game.ID }

func (c *covers) Invalidate(_ context.Context, id game.ID) error {
	c.invalidated = append(c.invalidated, id)

	return nil
}

// photographCover gives the source's copy of the only game a photo used as the cover, and adds a
// manual copy so the game survives when the source's copy goes.
func photographCover(ctx context.Context, t *testing.T, games game.Repository) game.ID {
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

	return g.ID()
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

			id := photographCover(ctx, t, games)
			cache.invalidated = nil

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
				assert.Equal(t, []game.ID{id}, cache.invalidated)
			})
		})
	}
}
