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

func physical(platform, barcode string) game.CopyDetails {
	b, err := game.ParseBarcode(barcode)
	if err != nil {
		panic(err)
	}

	return game.CopyDetails{
		Kind:     game.KindPhysical,
		Status:   game.StatusOwned,
		Platform: platform,
		Barcode:  b,
	}
}

func TestAddScannedCopies(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	setup := func(t *testing.T) (*catalog.Service, *game.Game) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		svc := catalog.NewService(sqlite.NewGameRepository(db), db, time.Now, nil, nil, nil)
		halo, err := svc.CreateGame(ctx, game.Info{Title: "Halo 3"}, nil)
		require.NoError(t, err)

		return svc, halo
	}

	t.Run("GIVEN a scanning session with copies of a game you have and of a new game twice", func(t *testing.T) {
		svc, halo := setup(t)
		items := []catalog.ScannedCopy{
			{
				Ref:     "a",
				GameID:  halo.ID(),
				Details: physical("Xbox 360", "882224536691"),
			},
			{
				Ref:      "b",
				Title:    "Dead Space 3",
				CoverURL: "https://example.com/ds3.jpg",
				Details:  physical("Xbox 360", "5030934110075"),
			},
			{
				Ref:     "c",
				Title:   "dead space  3!",
				Details: physical("PS3", "5030941110075"),
			},
			{
				Ref:     "d",
				GameID:  halo.ID(),
				Details: physical("Xbox 360", "882224536691"),
			},
		}

		t.Run("WHEN they are saved", func(t *testing.T) {
			results, games, err := svc.AddScannedCopies(ctx, items)
			require.NoError(t, err)

			t.Run("THEN every item has its copy and no error", func(t *testing.T) {
				require.Len(t, results, 4)

				for _, r := range results {
					assert.NoError(t, r.Err, r.Ref)
					assert.NotEmpty(t, r.CopyID, r.Ref)
				}
			})

			t.Run("AND the new game is created once, with both copies and the first cover", func(t *testing.T) {
				require.Len(t, games, 2)

				ds3 := games[1]
				assert.Equal(t, "Dead Space 3", ds3.Title())
				assert.Len(t, ds3.Copies(), 2)
				assert.Equal(t, "https://example.com/ds3.jpg", ds3.CoverURL())
				assert.Equal(t, ds3.ID(), results[1].GameID)
				assert.Equal(t, ds3.ID(), results[2].GameID)
			})

			t.Run("AND the game you have gets both copies in one save", func(t *testing.T) {
				assert.Equal(t, halo.ID(), games[0].ID())
				assert.Len(t, games[0].Copies(), 2)

				all, err := svc.ListGames(ctx)
				require.NoError(t, err)
				assert.Len(t, all, 2)
			})
		})
	})

	t.Run("GIVEN a copy for a game deleted meanwhile next to a valid one", func(t *testing.T) {
		svc, halo := setup(t)
		require.NoError(t, svc.DeleteGame(ctx, halo.ID()))

		results, games, err := svc.AddScannedCopies(ctx, []catalog.ScannedCopy{
			{
				Ref:     "gone",
				GameID:  halo.ID(),
				Details: physical("Xbox 360", "882224536691"),
			},
			{
				Ref:     "ok",
				Title:   "Dead Space 3",
				Details: physical("Xbox 360", "5030934110075"),
			},
		})
		require.NoError(t, err)

		t.Run("THEN only that copy fails, saying to choose another game", func(t *testing.T) {
			assert.ErrorIs(t, results[0].Err, catalog.ErrScannedGameGone)
			assert.NoError(t, results[1].Err)
			assert.Len(t, games, 1)
		})
	})

	t.Run("GIVEN a copy the domain refuses next to a valid one for the same new game", func(t *testing.T) {
		svc, _ := setup(t)
		bad := physical("Xbox 360", "5030934110075")
		bad.Grade = game.Grade("scratched") // not a grade the domain knows

		results, games, err := svc.AddScannedCopies(ctx, []catalog.ScannedCopy{
			{
				Ref:     "bad",
				Title:   "Dead Space 3",
				Details: bad,
			},
			{
				Ref:     "ok",
				Title:   "Dead Space 3",
				Details: physical("PS3", "5030941110075"),
			},
		})
		require.NoError(t, err)

		t.Run("THEN the bad copy fails alone and the game is created with the other", func(t *testing.T) {
			var v *game.ValidationError
			assert.True(t, errors.As(results[0].Err, &v))
			assert.NoError(t, results[1].Err)
			require.Len(t, games, 1)
			assert.Len(t, games[0].Copies(), 1)
		})
	})

	t.Run("GIVEN two new games whose titles are only punctuation", func(t *testing.T) {
		svc, _ := setup(t)

		_, games, err := svc.AddScannedCopies(ctx, []catalog.ScannedCopy{
			{
				Ref:     "a",
				Title:   "!!!",
				Details: physical("PS3", "5030934110075"),
			},
			{
				Ref:     "b",
				Title:   "???",
				Details: physical("PS3", "5030941110075"),
			},
		})
		require.NoError(t, err)

		t.Run("THEN they stay two games (their match keys are empty, not equal)", func(t *testing.T) {
			assert.Len(t, games, 2)
		})
	})

	t.Run("GIVEN requests the scan page never sends", func(t *testing.T) {
		svc, _ := setup(t)
		tooMany := make([]catalog.ScannedCopy, catalog.MaxScannedCopies+1)

		for i := range tooMany {
			tooMany[i] = catalog.ScannedCopy{
				Title:   "X",
				Details: physical("PS3", ""),
			}
		}

		t.Run("THEN more than the limit, or a new game without a title, is invalid input", func(t *testing.T) {
			_, _, err := svc.AddScannedCopies(ctx, tooMany)
			assert.ErrorIs(t, err, catalog.ErrInvalidScannedCopies)

			_, _, err = svc.AddScannedCopies(ctx, []catalog.ScannedCopy{{
				Title:   " ",
				Details: physical("PS3", ""),
			}})
			assert.ErrorIs(t, err, catalog.ErrInvalidScannedCopies)
		})
	})
}
