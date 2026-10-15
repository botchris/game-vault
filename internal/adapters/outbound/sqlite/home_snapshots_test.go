package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/domain/game"
)

func TestHomeSnapshots(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	repo := NewHomeSnapshots(db)
	fetched := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)

	t.Run("GIVEN no snapshot stored", func(t *testing.T) {
		t.Run("WHEN the deals block is read", func(t *testing.T) {
			s, ok, err := repo.Snapshot(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN there is none, named after its block", func(t *testing.T) {
				assert.False(t, ok)
				assert.Equal(t, home.BlockDeals, s.Block)
			})
		})
	})

	t.Run("GIVEN a deals snapshot with both lists", func(t *testing.T) {
		in := home.Snapshot{
			Block: home.BlockDeals,
			Featured: []game.Deal{{
				ProviderGameID: "018d937f-0000-7000-8000-000000000001",
				Title:          "Lantern Keep",
				SteamAppID:     "4410",
				Shop:           "Steam",
				Price: game.Money{
					Amount:   499,
					Currency: "EUR",
				},
				Regular: game.Money{
					Amount:   1999,
					Currency: "EUR",
				},
				Cut:           75,
				HistoricalLow: true,
				Expires:       time.Date(2026, 10, 20, 17, 0, 0, 0, time.UTC),
				URL:           "https://itad.link/018d9386-0000-7000-8000-00000000aaaa/",
				ImageURL:      "https://assets.isthereanydeal.com/018d937f-0000-7000-8000-000000000001/boxart.jpg",
			}},
			HistoricalLows: []game.Deal{{Title: "Echo Valley"}},
			FetchedAt:      fetched,
		}

		require.NoError(t, repo.SaveSnapshot(ctx, in))

		t.Run("WHEN it is read back", func(t *testing.T) {
			got, ok, err := repo.Snapshot(ctx, home.BlockDeals)
			require.NoError(t, err)
			require.True(t, ok)

			t.Run("THEN every field survives, and the unset times stay zero", func(t *testing.T) {
				assert.Equal(t, in, got)
				assert.True(t, got.FailedAt.IsZero())
				assert.True(t, got.HistoricalLows[0].Expires.IsZero())
			})
		})

		t.Run("WHEN a failed refresh is saved over it, keeping the data", func(t *testing.T) {
			failed := in
			failed.Error = "the service is not answering: try again later"
			failed.ErrorKind = home.ErrorUnavailable
			failed.FailedAt = fetched.Add(6 * time.Hour)
			require.NoError(t, repo.SaveSnapshot(ctx, failed))

			got, _, err := repo.Snapshot(ctx, home.BlockDeals)
			require.NoError(t, err)

			t.Run("THEN the failure is stored next to the last good data", func(t *testing.T) {
				assert.Equal(t, failed, got)
			})
		})
	})

	t.Run("GIVEN a releases snapshot", func(t *testing.T) {
		in := home.Snapshot{
			Block: home.BlockReleases,
			FromSeries: []game.Release{{
				ProviderGameID: "2001",
				Title:          "Lantern Keep II",
				Date:           "2026-11-20",
				Platforms:      []string{game.SystemPC, game.SystemPS5},
				CoverURL:       "https://images.igdb.com/igdb/image/upload/t_cover_big/co-lk2.jpg",
				URL:            "https://www.igdb.com/games/lantern-keep-ii",
				Because:        "g1",
				BecauseTitle:   "Lantern Keep",
			}},
			Anticipated: []game.Release{{
				ProviderGameID: "2002",
				Title:          "Harbor Lights",
				Date:           "2026-12-01",
			}},
			FetchedAt: fetched,
		}

		require.NoError(t, repo.SaveSnapshot(ctx, in))

		t.Run("WHEN it is read back", func(t *testing.T) {
			got, ok, err := repo.Snapshot(ctx, home.BlockReleases)
			require.NoError(t, err)

			t.Run("THEN it is whole and the deals block is untouched", func(t *testing.T) {
				require.True(t, ok)
				assert.Equal(t, in, got)

				deals, _, err := repo.Snapshot(ctx, home.BlockDeals)
				require.NoError(t, err)
				assert.Len(t, deals.Featured, 1)
			})
		})
	})

	t.Run("GIVEN a row whose document cannot be read", func(t *testing.T) {
		_, err := db.conn(ctx).ExecContext(ctx, `UPDATE home_snapshots SET doc = '{"featured": 7}' WHERE block = 'deals'`)
		require.NoError(t, err)

		t.Run("WHEN it is read", func(t *testing.T) {
			_, ok, err := repo.Snapshot(ctx, home.BlockDeals)

			t.Run("THEN it counts as missing, so the block is gathered again rather than failing the page", func(t *testing.T) {
				require.NoError(t, err)
				assert.False(t, ok)
			})
		})
	})
}
