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
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/transfer"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/settings"
)

// covers records the editions whose cached cover was dropped ("<game id>|<system>").
type covers struct {
	editions []string
}

func (c *covers) Invalidate(context.Context, game.ID) error { return nil }

func (c *covers) DropLegacyCover(context.Context, game.ID) error { return nil }

func (c *covers) InvalidateEdition(_ context.Context, id game.ID, system string) error {
	c.editions = append(c.editions, string(id)+"|"+system)

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
		svc := transfer.NewService(sqlite.NewGameRepository(db), catalog.NewService(sqlite.NewGameRepository(db), db, catalog.WithCoverCache(cache)),
			sqlite.NewSettingsRepository(db), csvfile.Codec{})

		_, err = svc.Import(ctx, []byte("title,platform,kind\nHalo 3,Steam,library\nHalo 3,PS3,physical\n"))
		require.NoError(t, err)

		games, err := sqlite.NewGameRepository(db).List(ctx)
		require.NoError(t, err)
		require.Len(t, games, 1)

		id := string(games[0].ID())
		assert.ElementsMatch(t, []string{id + "|PC", id + "|PS3"}, cache.editions, "a new game's editions all count")

		cache.editions = nil

		t.Run("WHEN a file adds a physical PC copy", func(t *testing.T) {
			_, err := svc.Import(ctx, []byte("title,platform,kind\nHalo 3,Steam,library\nHalo 3,PS3,physical\nHalo 3,PC,physical\n"))
			require.NoError(t, err)

			t.Run("THEN only the PC edition's cached cover is dropped", func(t *testing.T) {
				assert.Equal(t, []string{id + "|PC"}, cache.editions)
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

func TestImport_defaultCurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// newImport returns the import service and the catalog it fills, with the given default
	// currency (empty: none chosen yet).
	newImport := func(t *testing.T, currency string) (*transfer.Service, *sqlite.GameRepository) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		prefs := sqlite.NewSettingsRepository(db)
		require.NoError(t, prefs.SavePreferences(ctx, settings.Preferences{Currency: currency}))

		games := sqlite.NewGameRepository(db)

		return transfer.NewService(games, catalog.NewService(games, db), prefs, csvfile.Codec{}), games
	}

	price := func(t *testing.T, games *sqlite.GameRepository) game.Money {
		t.Helper()

		list, err := games.List(ctx)
		require.NoError(t, err)
		require.Len(t, list, 1)

		return list[0].Copies()[0].Price
	}

	for _, tc := range []struct {
		currency string
		want     int64
	}{
		{"GBP", 1250},
		{"JPY", 13}, // no decimals: 12.50 rounds half up
	} {
		t.Run("GIVEN "+tc.currency+" as the default currency", func(t *testing.T) {
			svc, games := newImport(t, tc.currency)

			t.Run("WHEN a row has a price without currency", func(t *testing.T) {
				_, err := svc.Import(ctx, []byte("title,kind,price\nOkami,physical,12.50\n"))
				require.NoError(t, err)

				t.Run("THEN it gets the default currency, in that currency's decimals", func(t *testing.T) {
					assert.Equal(t, game.Money{
						Amount:   tc.want,
						Currency: tc.currency,
					}, price(t, games))
				})
			})
		})
	}

	t.Run("GIVEN no default currency", func(t *testing.T) {
		svc, games := newImport(t, "")

		t.Run("WHEN a row has a price without currency", func(t *testing.T) {
			report, err := svc.Import(ctx, []byte("title,kind,price\nOkami,physical,12.50\n"))
			require.NoError(t, err)

			t.Run("THEN the price is left out, with a warning saying where to set the currency", func(t *testing.T) {
				assert.True(t, price(t, games).IsZero())
				require.Len(t, report.Warnings, 1)
				assert.Contains(t, report.Warnings[0], "System → Preferences")
			})
		})
	})
}
