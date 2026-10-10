package media_test

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
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

// storeDetails is a metadata provider that, like the Steam store, knows only games linked to it.
type storeDetails struct {
	sheet *media.GameDetails
	calls atomic.Int32
}

func (*storeDetails) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "store-details",
		Kind:             provider.KindMetadata,
		Name:             "Store",
		EnabledByDefault: true,
		DefaultOrder:     1,
	}
}

func (*storeDetails) Test(context.Context, schema.Settings) error { return nil }

func (*storeDetails) Applies(q media.CoverQuery) bool { return q.Links[game.LinkSteam] != "" }

func (p *storeDetails) Details(context.Context, media.CoverQuery, string, schema.Settings) (*media.GameDetails, error) {
	p.calls.Add(1)

	return p.sheet, nil
}

// quotaDetails is a metadata provider that, like TheGamesDB, keeps its quota for physical copies,
// games no store knows and the fallback pass.
type quotaDetails struct {
	limited atomic.Bool
	calls   atomic.Int32

	// asked is the query of the last call.
	asked media.CoverQuery
}

func (*quotaDetails) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "quota-details",
		Kind:             provider.KindMetadata,
		Name:             "Quota",
		EnabledByDefault: true,
		DefaultOrder:     2,
	}
}

func (*quotaDetails) Test(context.Context, schema.Settings) error { return nil }

func (*quotaDetails) Applies(q media.CoverQuery) bool {
	return q.HasPhysical() || !q.HasStoreLink() || q.Fallback
}

func (p *quotaDetails) Details(_ context.Context, q media.CoverQuery, _ string, _ schema.Settings) (*media.GameDetails, error) {
	p.calls.Add(1)
	p.asked = q

	if p.limited.Load() {
		return nil, provider.ErrRateLimited
	}

	return &media.GameDetails{
		Summary: "A console adventure.",
		Genres:  []string{"Adventure"},
	}, nil
}

type detailsEnv struct {
	svc     *media.Service
	games   game.Repository
	details media.DetailsStore
	store   *storeDetails
	quota   *quotaDetails
}

func newDetailsEnv(t *testing.T, ctx context.Context, now func() time.Time, storeSheet *media.GameDetails) detailsEnv {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
	require.NoError(t, err)

	games, details := sqlite.NewGameRepository(db), sqlite.NewDetailsStore(db)
	store, quota := &storeDetails{sheet: storeSheet}, &quotaDetails{}
	svc := media.NewService(games, sqlite.NewProviderRepository(db), assets, nil, details, echo{}, now,
		slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{
			Metadata: []media.MetadataProvider{store, quota},
		})

	return detailsEnv{
		svc:     svc,
		games:   games,
		details: details,
		store:   store,
		quota:   quota,
	}
}

func TestGameDetails_fallbackPass(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game owned only as a PS4 library copy, linked to the PlayStation store", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, nil)
		g := saveGame(t, ctx, env.games, "Console Quest", game.Links{"playstation": "PPSA0001"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PS4",
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			d, warnings, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the quota-limited provider fills it in the fallback pass", func(t *testing.T) {
				assert.Empty(t, warnings)
				assert.Equal(t, "A console adventure.", d.Summary)
				assert.Equal(t, []provider.ID{"quota-details"}, d.Sources)
				assert.True(t, env.quota.asked.Fallback)
				assert.Equal(t, []string{"PS4"}, env.quota.asked.Platforms)

				t.Run("AND the sheet is cached", func(t *testing.T) {
					cached, ok, err := env.details.Get(ctx, g.ID(), "en")
					require.NoError(t, err)
					require.True(t, ok)
					assert.Equal(t, "A console adventure.", cached.Summary)
				})
			})
		})
	})

	t.Run("GIVEN a Steam game whose store has a full sheet", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, &media.GameDetails{
			Summary:     "A PC classic.",
			Screenshots: []media.Screenshot{{FullURL: "https://cdn.test/1.jpg"}},
		})
		g := saveGame(t, ctx, env.games, "PC Classic", game.Links{game.LinkSteam: "10"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			d, _, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN only the store is asked: the fallback pass never runs", func(t *testing.T) {
				assert.Equal(t, "A PC classic.", d.Summary)
				assert.Equal(t, int32(1), env.store.calls.Load())
				assert.Zero(t, env.quota.calls.Load())
			})
		})
	})

	t.Run("GIVEN a PlayStation-only game and a quota-limited provider asking to slow down", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, nil)
		env.quota.limited.Store(true)
		g := saveGame(t, ctx, env.games, "Console Quest", game.Links{"playstation": "PPSA0001"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PS4",
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			d, warnings, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it is empty this time and says the provider is busy", func(t *testing.T) {
				assert.Empty(t, d.Summary)
				assert.Len(t, warnings, 1)
			})

			t.Run("AND nothing is cached, so the next view asks again", func(t *testing.T) {
				_, ok, err := env.details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				assert.False(t, ok)
			})
		})
	})
}

func TestGameDetails_sheetsCachedBeforeTheFallbackPass(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// The fallback pass arrived on 2026-10-10; these sheets were cached five days before and are
	// still within the 30-day TTL.
	now := func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
	before := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN a PlayStation-only game with an empty sheet cached before the fallback pass", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, now, nil)
		g := saveGame(t, ctx, env.games, "Console Quest", game.Links{"playstation": "PPSA0001"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PS4",
		})
		require.NoError(t, env.details.Put(ctx, g.ID(), media.GameDetails{
			Language:  "en",
			FetchedAt: before,
		}))

		t.Run("WHEN its sheet is asked twice", func(t *testing.T) {
			first, _, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			second, _, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it is fetched again once, and the new sheet is served from the cache", func(t *testing.T) {
				assert.Equal(t, "A console adventure.", first.Summary)
				assert.Equal(t, "A console adventure.", second.Summary)
				assert.Equal(t, int32(1), env.quota.calls.Load())
			})
		})
	})

	t.Run("GIVEN a PlayStation-only game whose sparse sheet the background scan cached without the fallback pass", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, now, nil)
		g := saveGame(t, ctx, env.games, "Console Quest", game.Links{"playstation": "PPSA0001"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PS4",
		})
		require.NoError(t, env.details.Put(ctx, g.ID(), media.GameDetails{
			Genres:          []string{"Adventure"},
			FallbackSkipped: true,
			Language:        "en",
			FetchedAt:       now().Add(-time.Hour),
		}))

		t.Run("WHEN its sheet is opened", func(t *testing.T) {
			d, _, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the fallback pass runs", func(t *testing.T) {
				assert.Equal(t, "A console adventure.", d.Summary)
				assert.Equal(t, int32(1), env.quota.calls.Load())
			})
		})
	})

	t.Run("GIVEN a game with a sheet that has a summary, cached before the fallback pass", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, now, nil)
		g := saveGame(t, ctx, env.games, "Console Quest", game.Links{"playstation": "PPSA0001"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PS4",
		})
		require.NoError(t, env.details.Put(ctx, g.ID(), media.GameDetails{
			Summary:   "Written by hand long ago.",
			Language:  "en",
			FetchedAt: before,
		}))

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			d, _, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the cached sheet stays and no provider is asked", func(t *testing.T) {
				assert.Equal(t, "Written by hand long ago.", d.Summary)
				assert.Zero(t, env.quota.calls.Load())
				assert.Zero(t, env.store.calls.Load())
			})
		})
	})
}
