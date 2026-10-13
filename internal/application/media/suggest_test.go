package media_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
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
		svc := media.NewService(sqlite.NewGameRepository(db), sqlite.NewProviderRepository(db), assets, nil, nil, echo{},
			time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{
				Covers:   []media.CoverProvider{art},
				Barcodes: []media.BarcodeProvider{knownCode{}},
			})

		t.Run("WHEN the user types a title on the scan page", func(t *testing.T) {
			_, _, _, err := svc.SuggestGames(ctx, "Shelf Quest", "PS4")
			require.NoError(t, err)

			t.Run("THEN the box art database is asked to browse, since the user picks from the list", func(t *testing.T) {
				assert.Equal(t, int32(1), art.calls.Load())
				assert.True(t, art.browsed.Load())
			})
		})

		t.Run("WHEN a scanned code is identified automatically", func(t *testing.T) {
			res, err := svc.IdentifyBarcode(ctx, "4006381333931")
			require.NoError(t, err)
			require.NotNil(t, res.Match)

			t.Run("THEN the box art database is not asked to browse", func(t *testing.T) {
				assert.Equal(t, int32(2), art.calls.Load())
				assert.False(t, art.browsed.Load())
			})
		})
	})
}
