package plugin_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/home"
	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin"
	"gamevault/internal/application/sync"
	"gamevault/internal/application/valuation"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

type fakeSource struct{ typ source.Type }

func (f fakeSource) Descriptor() source.TypeDescriptor { return source.TypeDescriptor{Type: f.typ} }
func (fakeSource) Fetch(context.Context, schema.Settings) ([]game.ImportedCopy, []string, error) {
	return nil, nil, nil
}
func (fakeSource) Test(context.Context, schema.Settings) error { return nil }

type fakeCovers struct{ id provider.ID }

func (f fakeCovers) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:   f.id,
		Kind: provider.KindCover,
	}
}
func (fakeCovers) Test(context.Context, schema.Settings) error { return nil }
func (fakeCovers) Applies(media.GameQuery) bool                { return true }
func (fakeCovers) Covers(context.Context, media.GameQuery, schema.Settings) ([]media.CoverCandidate, error) {
	return nil, nil
}

func TestRegistry(t *testing.T) {
	store := plugin.Plugin{
		ID:      "store",
		Sources: []sync.Provider{fakeSource{"store"}},
		Covers:  []media.CoverProvider{fakeCovers{"store"}},
	}
	db := plugin.Plugin{
		ID:     "db",
		Covers: []media.CoverProvider{fakeCovers{"db"}},
	}

	t.Run("GIVEN plugins with unique ids", func(t *testing.T) {
		r, err := plugin.NewRegistry(store, db)
		require.NoError(t, err)

		t.Run("THEN each piece goes to its service, in plugin order", func(t *testing.T) {
			require.Len(t, r.Sources(), 1)
			covers := r.Providers()
			require.Len(t, covers, 2)
			assert.Equal(t, provider.ID("store"), covers[0].Descriptor().ID)
			assert.Equal(t, provider.ID("db"), covers[1].Descriptor().ID)
		})
	})

	t.Run("GIVEN two plugins with a provider under the same id", func(t *testing.T) {
		_, err := plugin.NewRegistry(store, plugin.Plugin{
			ID:     "copy",
			Covers: []media.CoverProvider{fakeCovers{"store"}},
		})

		t.Run("THEN the registry refuses them, naming both", func(t *testing.T) {
			assert.ErrorIs(t, err, plugin.ErrDuplicate)
			assert.ErrorContains(t, err, `provider "store" in plugins "store" and "copy"`)
		})
	})

	t.Run("GIVEN two plugins with the same source type or id", func(t *testing.T) {
		_, errType := plugin.NewRegistry(store, plugin.Plugin{
			ID:      "other",
			Sources: []sync.Provider{fakeSource{"store"}},
		})
		_, errID := plugin.NewRegistry(store, plugin.Plugin{ID: "store"})

		t.Run("THEN both are refused", func(t *testing.T) {
			assert.ErrorIs(t, errType, plugin.ErrDuplicate)
			assert.ErrorIs(t, errID, plugin.ErrDuplicate)
		})
	})
}

type fakePrices struct{ id provider.ID }

func (f fakePrices) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:   f.id,
		Kind: provider.KindValuation,
	}
}
func (fakePrices) Test(context.Context, schema.Settings) error { return nil }
func (fakePrices) Estimate(context.Context, schema.Settings, game.Barcode) (game.Estimate, error) {
	return game.Estimate{}, valuation.ErrNotListed
}

func TestRegistry_valuations(t *testing.T) {
	shop := plugin.Plugin{
		ID:         "shop",
		Valuations: []valuation.Provider{fakePrices{"shop-prices"}},
	}

	t.Run("GIVEN a plugin with a price provider", func(t *testing.T) {
		r, err := plugin.NewRegistry(shop)
		require.NoError(t, err)

		t.Run("THEN the valuation service gets it, and the providers registry configures it", func(t *testing.T) {
			require.Len(t, r.Valuations(), 1)

			require.Len(t, r.Providers(), 1)
			assert.Equal(t, provider.ID("shop-prices"), r.Providers()[0].Descriptor().ID)
		})
	})

	t.Run("GIVEN two plugins with the same price provider id", func(t *testing.T) {
		_, err := plugin.NewRegistry(shop, plugin.Plugin{
			ID:         "other",
			Valuations: []valuation.Provider{fakePrices{"shop-prices"}},
		})

		t.Run("THEN they are refused", func(t *testing.T) {
			assert.ErrorIs(t, err, plugin.ErrDuplicate)
		})
	})
}

type fakeDeals struct{ id provider.ID }

func (f fakeDeals) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:   f.id,
		Kind: provider.KindDeals,
	}
}

func (fakeDeals) Test(context.Context, schema.Settings) error { return nil }

func (fakeDeals) Deals(context.Context, schema.Settings, home.DealQuery) ([]game.Deal, error) {
	return nil, nil
}

type fakeReleases struct{ id provider.ID }

func (f fakeReleases) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:   f.id,
		Kind: provider.KindReleases,
	}
}

func (fakeReleases) Test(context.Context, schema.Settings) error { return nil }

func (fakeReleases) Anticipated(context.Context, schema.Settings, time.Duration) ([]game.Release, error) {
	return nil, nil
}

func (fakeReleases) Sequels(context.Context, schema.Settings, []home.OwnedRef) ([]game.Release, error) {
	return nil, nil
}

func TestRegistry_homeProviders(t *testing.T) {
	t.Run("GIVEN a deals plugin and a game database that also lists releases", func(t *testing.T) {
		deals := plugin.Plugin{
			ID:    "deals",
			Name:  "Deals",
			Deals: []home.DealsProvider{fakeDeals{"deals"}},
		}
		db := plugin.Plugin{
			ID:       "db",
			Name:     "DB",
			Covers:   []media.CoverProvider{fakeCovers{"db"}},
			Releases: []home.ReleasesProvider{fakeReleases{"db-releases"}},
		}

		r, err := plugin.NewRegistry(deals, db)
		require.NoError(t, err)

		t.Run("THEN the home service gets them, and the providers registry configures them with the others", func(t *testing.T) {
			require.Len(t, r.Deals(), 1)
			require.Len(t, r.Releases(), 1)

			all := r.Providers()
			ids := make([]provider.ID, 0, len(all))

			for _, p := range all {
				ids = append(ids, p.Descriptor().ID)
			}

			assert.Equal(t, []provider.ID{"db", "deals", "db-releases"}, ids, "kind by kind, in provider.Kinds' order")
		})
	})

	t.Run("GIVEN a releases provider whose id another plugin's deals provider uses", func(t *testing.T) {
		_, err := plugin.NewRegistry(
			plugin.Plugin{
				ID:    "a",
				Deals: []home.DealsProvider{fakeDeals{"same"}},
			},
			plugin.Plugin{
				ID:       "b",
				Releases: []home.ReleasesProvider{fakeReleases{"same"}},
			},
		)

		t.Run("THEN the registry refuses them", func(t *testing.T) {
			assert.ErrorIs(t, err, plugin.ErrDuplicate)
		})
	})
}
