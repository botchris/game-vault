package providers_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/providers"
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

// countingRepo counts the reads of the configurations.
type countingRepo struct {
	provider.Repository
	lists atomic.Int32
}

func (r *countingRepo) List(ctx context.Context, kind provider.Kind) ([]*provider.Provider, error) {
	r.lists.Add(1)
	return r.Repository.List(ctx, kind)
}

func newRepo(t *testing.T, ctx context.Context) *countingRepo {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	return &countingRepo{Repository: sqlite.NewProviderRepository(db)}
}

func TestProviders_settingsGroups(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an enabled barcode provider with a key, and a price provider of the same service seen for the first time", func(t *testing.T) {
		reg := providers.NewRegistry(newRepo(t, ctx), []providers.Impl{
			grouped{"shop", provider.KindBarcode},
			grouped{"shop-prices", provider.KindValuation},
		})

		_, err := reg.Providers(ctx, provider.KindBarcode)
		require.NoError(t, err)
		_, err = reg.ConfigureProvider(ctx, "shop", true, schema.Settings{"key": "k"})
		require.NoError(t, err)

		t.Run("WHEN the price providers are listed", func(t *testing.T) {
			views, err := reg.Providers(ctx, provider.KindValuation)
			require.NoError(t, err)

			t.Run("THEN the price provider shares the key but stays off until the user turns it on", func(t *testing.T) {
				require.Len(t, views, 1)
				assert.Equal(t, "k", views[0].Settings()["key"])
				assert.False(t, views[0].Enabled())
			})
		})
	})
}

func TestRegistry_readsOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a registry with a barcode provider", func(t *testing.T) {
		repo := newRepo(t, ctx)
		reg := providers.NewRegistry(repo, []providers.Impl{grouped{"shop", provider.KindBarcode}})

		t.Run("WHEN its chain is read many times, and the provider configured in between", func(t *testing.T) {
			for range 5 {
				_, err := reg.Enabled(ctx, provider.KindBarcode)
				require.NoError(t, err)
			}

			_, err := reg.ConfigureProvider(ctx, "shop", true, schema.Settings{"key": "k"})
			require.NoError(t, err)

			views, err := reg.Enabled(ctx, provider.KindBarcode)
			require.NoError(t, err)

			t.Run("THEN storage was read once, and the chain shows the change", func(t *testing.T) {
				assert.Equal(t, int32(len(provider.Kinds())), repo.lists.Load(), "one read per kind, once")
				require.Len(t, views, 1)
				assert.Equal(t, "k", views[0].Settings()["key"])
			})

			t.Run("AND the change was saved", func(t *testing.T) {
				stored, err := repo.Repository.List(ctx, provider.KindBarcode)
				require.NoError(t, err)
				require.Len(t, stored, 1)
				assert.True(t, stored[0].Enabled())
			})
		})

		t.Run("WHEN a caller changes the view it was given", func(t *testing.T) {
			views, err := reg.Providers(ctx, provider.KindBarcode)
			require.NoError(t, err)
			require.NoError(t, views[0].Configure(views[0].Descriptor, false, nil, time.Now()))

			again, err := reg.Providers(ctx, provider.KindBarcode)
			require.NoError(t, err)

			t.Run("THEN the registry keeps its own configuration", func(t *testing.T) {
				assert.True(t, again[0].Enabled())
			})
		})
	})
}

// stateful is a barcode provider with a key the user enters and a state field it learns.
type stateful struct{}

func (stateful) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "stateful",
		Kind:             provider.KindBarcode,
		Name:             "Stateful",
		EnabledByDefault: true,
		Fields: schema.Fields{
			{
				Key:      "key",
				LabelKey: "key",
				Kind:     schema.FieldSecret,
			},
			{
				Key:  "hits",
				Kind: schema.FieldState,
			},
		},
	}
}

func (stateful) Test(context.Context, schema.Settings) error { return nil }

func TestRegistry_saveStateWhileConfiguring(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a provider that learns state during lookups", func(t *testing.T) {
		repo := newRepo(t, ctx)
		reg := providers.NewRegistry(repo, []providers.Impl{stateful{}})

		views, err := reg.Enabled(ctx, provider.KindBarcode)
		require.NoError(t, err)

		// The lookup read its settings before the user saved a new key, like a slow lookup.
		lookup := views[0].Settings()
		lookup["hits"] = "es"

		t.Run("WHEN the user saves a key while the lookup saves what it learned", func(t *testing.T) {
			var wg sync.WaitGroup

			wg.Go(func() {
				_, err := reg.ConfigureProvider(ctx, "stateful", true, schema.Settings{"key": "new"})
				assert.NoError(t, err)
			})
			wg.Go(func() { assert.NoError(t, reg.SaveState(ctx, "stateful", lookup)) })
			wg.Wait()

			t.Run("THEN the configuration keeps both, in memory and in storage", func(t *testing.T) {
				views, err := reg.Providers(ctx, provider.KindBarcode)
				require.NoError(t, err)
				assert.Equal(t, schema.Settings{"key": "new", "hits": "es"}, views[0].Settings())

				stored, err := repo.Repository.List(ctx, provider.KindBarcode)
				require.NoError(t, err)
				assert.Equal(t, schema.Settings{"key": "new", "hits": "es"}, stored[0].Settings())
			})
		})
	})
}

func TestRegistry_concurrentReadsAndChanges(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a registry with a provider", func(t *testing.T) {
		reg := providers.NewRegistry(newRepo(t, ctx), []providers.Impl{stateful{}})

		t.Run("WHEN its chain is read while it is configured, many times at once", func(t *testing.T) {
			var wg sync.WaitGroup

			for i := range 40 {
				wg.Go(func() {
					if i%4 == 0 {
						_, err := reg.ConfigureProvider(ctx, "stateful", i%8 == 0, schema.Settings{"key": fmt.Sprint(i)})
						assert.NoError(t, err)

						return
					}

					views, err := reg.Enabled(ctx, provider.KindBarcode)
					assert.NoError(t, err)

					for _, v := range views {
						_ = v.Settings()["key"] // reads the view while others change the registry
					}
				})
			}

			wg.Wait()

			t.Run("THEN the provider is still listed once", func(t *testing.T) {
				views, err := reg.Providers(ctx, provider.KindBarcode)
				require.NoError(t, err)
				assert.Len(t, views, 1)
			})
		})
	})
}

func TestProviders_scheduledKindsStayOff(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an enabled metadata provider with a key, and a releases provider of the same service seen for the first time", func(t *testing.T) {
		reg := providers.NewRegistry(newRepo(t, ctx), []providers.Impl{
			grouped{"shop", provider.KindMetadata},
			grouped{"shop-releases", provider.KindReleases},
		})

		_, err := reg.Providers(ctx, provider.KindMetadata)
		require.NoError(t, err)
		_, err = reg.ConfigureProvider(ctx, "shop", true, schema.Settings{"key": "k"})
		require.NoError(t, err)

		t.Run("WHEN the releases providers are listed", func(t *testing.T) {
			views, err := reg.Providers(ctx, provider.KindReleases)
			require.NoError(t, err)

			t.Run("THEN it shares the key but stays off: it would send requests on its own schedule", func(t *testing.T) {
				require.Len(t, views, 1)
				assert.Equal(t, "k", views[0].Settings()["key"])
				assert.False(t, views[0].Enabled())
			})
		})
	})
}
