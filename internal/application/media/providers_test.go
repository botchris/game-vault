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

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// grouped is a provider of some kind in the settings group "shop", which needs a key.
type grouped struct {
	id   provider.ID
	kind provider.Kind
}

func (g grouped) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:            g.id,
		Kind:          g.kind,
		Name:          string(g.id),
		SettingsGroup: "shop",
		DefaultOrder:  10,
		Fields: schema.Fields{{
			Key:      "key",
			LabelKey: "key",
			Kind:     schema.FieldSecret,
			Required: true,
		}},
	}
}

func (grouped) Test(context.Context, schema.Settings) error { return nil }

func (grouped) Lookup(context.Context, game.Barcode, schema.Settings) ([]media.BarcodeMatch, error) {
	return nil, nil
}

func TestProviders_settingsGroups(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an enabled barcode provider with a key, and a price provider of the same service seen for the first time", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		svc := media.NewService(nil, sqlite.NewProviderRepository(db), nil, nil, nil, nil, time.Now,
			slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{
				Barcodes:   []media.BarcodeProvider{grouped{"shop", provider.KindBarcode}},
				Valuations: []media.Provider{grouped{"shop-prices", provider.KindValuation}},
			})

		_, err = svc.Providers(ctx, provider.KindBarcode)
		require.NoError(t, err)
		_, err = svc.ConfigureProvider(ctx, "shop", true, schema.Settings{"key": "k"})
		require.NoError(t, err)

		t.Run("WHEN the price providers are listed", func(t *testing.T) {
			views, err := svc.Providers(ctx, provider.KindValuation)
			require.NoError(t, err)

			t.Run("THEN the price provider shares the key but stays off until the user turns it on", func(t *testing.T) {
				require.Len(t, views, 1)
				assert.Equal(t, "k", views[0].Settings()["key"])
				assert.False(t, views[0].Enabled())
			})
		})
	})
}
