package media_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// knownCode is a barcode database that knows every code as a PS4 game.
type knownCode struct{}

func (knownCode) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "codes",
		Kind:             provider.KindBarcode,
		Name:             "Codes",
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (knownCode) Test(context.Context, schema.Settings) error { return nil }

func (knownCode) Lookup(context.Context, game.Barcode, schema.Settings) ([]media.BarcodeMatch, error) {
	return []media.BarcodeMatch{{
		Raw:      "Shelf Quest PS4",
		Title:    "Shelf Quest",
		Platform: "PS4",
		Provider: "codes",
	}}, nil
}

func TestSuggest_browse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a barcode database and a box art database that records whether it was asked to browse", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		art := &quotaArt{}
		svc := newServices(db, sqlite.NewGameRepository(db), assets, echo{}, []providers.Provider{art, knownCode{}})

		t.Run("WHEN the user types a title on the scan page", func(t *testing.T) {
			_, err := svc.Identifier.SuggestGames(ctx, "Shelf Quest", "PS4")
			require.NoError(t, err)

			t.Run("THEN the box art database is asked to browse, since the user picks from the list", func(t *testing.T) {
				assert.Equal(t, int32(1), art.calls.Load())
				assert.True(t, art.browsed.Load())
			})
		})

		t.Run("WHEN a scanned code is identified automatically", func(t *testing.T) {
			res, err := svc.Identifier.IdentifyBarcode(ctx, "4006381333931")
			require.NoError(t, err)
			require.NotNil(t, res.Match)

			t.Run("THEN the box art database is not asked to browse", func(t *testing.T) {
				assert.Equal(t, int32(2), art.calls.Load())
				assert.False(t, art.browsed.Load())
			})
		})
	})
}

func TestSuggest_providerStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a box art database whose allowance is used up", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		art := &quotaArt{}
		art.exhausted.Store(true)

		svc := newServices(db, sqlite.NewGameRepository(db), assets, echo{}, []providers.Provider{art})

		t.Run("WHEN the user types a title on the scan page", func(t *testing.T) {
			res, err := svc.Identifier.SuggestGames(ctx, "Shelf Quest", "PS4")
			require.NoError(t, err)

			t.Run("THEN the used-up allowance is the provider's status, not a warning", func(t *testing.T) {
				assert.Empty(t, res.Warnings)
				assert.True(t, res.ProvidersUnavailable, "the scan page says some providers are unavailable")

				st := svc.Registry.Statuses()
				require.Len(t, st, 1)
				assert.Equal(t, "Quota", st[0].Name)
			})

			t.Run("AND a later answer clears the status", func(t *testing.T) {
				art.exhausted.Store(false)

				_, err := svc.Identifier.SuggestGames(ctx, "Shelf Quest", "PS4")
				require.NoError(t, err)
				assert.Empty(t, svc.Registry.Statuses())
			})
		})
	})
}

// blockedCodes is a barcode database behind a bot check.
type blockedCodes struct{}

func (blockedCodes) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "blocked",
		Kind:             provider.KindBarcode,
		Name:             "Blocked",
		EnabledByDefault: true,
		DefaultOrder:     5,
	}
}

func (blockedCodes) Test(context.Context, schema.Settings) error { return nil }

func (blockedCodes) Lookup(context.Context, game.Barcode, schema.Settings) ([]media.BarcodeMatch, error) {
	return nil, fmt.Errorf("asking for a browser check: %w", provider.ErrUnavailable)
}

func TestIdentifyBarcode_providerStatus(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a barcode database behind a bot check, then one that knows the code", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		svc := newServices(db, sqlite.NewGameRepository(db), assets, echo{}, []providers.Provider{&quotaArt{}, blockedCodes{}, knownCode{}})

		t.Run("WHEN a code is scanned", func(t *testing.T) {
			res, err := svc.Identifier.IdentifyBarcode(ctx, "4006381333931")
			require.NoError(t, err)

			t.Run("THEN the bot check is the provider's status and the answer's notice, not a warning", func(t *testing.T) {
				require.NotNil(t, res.Match)
				assert.Empty(t, res.Warnings)
				assert.True(t, res.ProvidersUnavailable)

				st := svc.Registry.Statuses()
				require.Len(t, st, 1)
				assert.Equal(t, "Blocked", st[0].Name)
			})
		})
	})
}
