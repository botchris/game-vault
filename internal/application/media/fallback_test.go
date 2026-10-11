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

// countingStore has art for the PC edition of games linked to Steam, like the Steam store.
type countingStore struct{ calls atomic.Int32 }

func (*countingStore) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "store",
		Kind:             provider.KindCover,
		Name:             "Store",
		EnabledByDefault: true,
		DefaultOrder:     20,
	}
}

func (*countingStore) Test(context.Context, schema.Settings) error { return nil }

func (*countingStore) Applies(q media.CoverQuery) bool {
	return q.ForPC() && q.Links[game.LinkSteam] != ""
}

func (s *countingStore) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	s.calls.Add(1)

	return []media.CoverCandidate{{
		URL:      "https://img.test/store-" + q.Links[game.LinkSteam],
		Provider: "store",
	}}, nil
}

// quotaArt follows TheGamesDB's quota rules and finds nothing; it counts the requests it would
// spend.
type quotaArt struct{ calls atomic.Int32 }

func (*quotaArt) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "quota",
		Kind:             provider.KindCover,
		Name:             "Quota",
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (*quotaArt) Test(context.Context, schema.Settings) error { return nil }

func (*quotaArt) Applies(q media.CoverQuery) bool {
	return q.HasPhysical() || !q.HasStoreLink() || q.Fallback
}

func (a *quotaArt) Covers(context.Context, media.CoverQuery, schema.Settings) ([]media.CoverCandidate, error) {
	a.calls.Add(1)
	return nil, nil
}

// perGameArt has one cover per game, for any system, and only on the fallback pass, like IGDB.
type perGameArt struct{ calls atomic.Int32 }

func (*perGameArt) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "per-game",
		Kind:             provider.KindCover,
		Name:             "Per game",
		EnabledByDefault: true,
		DefaultOrder:     85,
	}
}

func (*perGameArt) Test(context.Context, schema.Settings) error { return nil }

func (*perGameArt) Applies(q media.CoverQuery) bool { return q.Fallback }

func (a *perGameArt) Covers(_ context.Context, q media.CoverQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	a.calls.Add(1)

	return []media.CoverCandidate{{
		URL:      "https://img.test/per-game",
		Label:    q.Title,
		Provider: "per-game",
	}}, nil
}

type fallbackEnv struct {
	svc     *media.Service
	games   game.Repository
	store   *countingStore
	quota   *quotaArt
	perGame *perGameArt
}

func newFallbackEnv(t *testing.T, ctx context.Context) fallbackEnv {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
	require.NoError(t, err)

	env := fallbackEnv{
		games:   sqlite.NewGameRepository(db),
		store:   &countingStore{},
		quota:   &quotaArt{},
		perGame: &perGameArt{},
	}
	env.svc = media.NewService(env.games, sqlite.NewProviderRepository(db), assets, nil, nil, echo{}, time.Now,
		slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{
			Covers: []media.CoverProvider{env.store, env.quota, env.perGame},
		})

	return env
}

func TestCovers_fallbackForGamesWithoutLinks(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a PS4 disc of a game no store links, which the box art database does not know", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN its PS4 cover is asked", func(t *testing.T) {
			img, err := env.svc.EditionCover(ctx, g.ID(), "PS4")

			t.Run("THEN the per-game cover is used on the fallback pass", func(t *testing.T) {
				assert.Equal(t, "https://img.test/per-game", cover(t, img, err))
			})

			t.Run("AND the box art database was asked once, as before", func(t *testing.T) {
				assert.Equal(t, int32(1), env.quota.calls.Load())
			})
		})
	})

	t.Run("GIVEN a PC key of a game no store links", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Key Quest", nil, game.CopyDetails{
			Kind:     game.KindKey,
			Platform: "Epic Games",
		})

		t.Run("WHEN its PC cover is asked", func(t *testing.T) {
			img, err := env.svc.EditionCover(ctx, g.ID(), game.SystemPC)

			t.Run("THEN the fallback pass finds one", func(t *testing.T) {
				assert.Equal(t, "https://img.test/per-game", cover(t, img, err))
				assert.Equal(t, int32(1), env.quota.calls.Load())
			})
		})

		t.Run("WHEN its cover candidates are listed", func(t *testing.T) {
			candidates, warnings, err := env.svc.CoverCandidates(ctx, g.ID(), game.SystemPC)
			require.NoError(t, err)

			t.Run("THEN the fallback providers are offered too, since nothing else had one", func(t *testing.T) {
				assert.Empty(t, warnings)
				require.Len(t, candidates, 1)
				assert.Equal(t, provider.ID("per-game"), candidates[0].Provider)
			})
		})
	})

	t.Run("GIVEN a Steam game whose store has art", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Steam Quest", game.Links{game.LinkSteam: "620"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		})

		t.Run("WHEN its PC cover is asked", func(t *testing.T) {
			img, err := env.svc.EditionCover(ctx, g.ID(), game.SystemPC)

			t.Run("THEN the store's art is used and nothing else is asked, as before", func(t *testing.T) {
				assert.Equal(t, "https://img.test/store-620", cover(t, img, err))
				assert.Zero(t, env.quota.calls.Load())
				assert.Zero(t, env.perGame.calls.Load())
			})
		})

		t.Run("WHEN its cover candidates are listed", func(t *testing.T) {
			candidates, _, err := env.svc.CoverCandidates(ctx, g.ID(), game.SystemPC)
			require.NoError(t, err)

			t.Run("THEN only the store is offered, so no quota is spent", func(t *testing.T) {
				require.Len(t, candidates, 1)
				assert.Equal(t, provider.ID("store"), candidates[0].Provider)
				assert.Zero(t, env.quota.calls.Load())
			})
		})
	})
}
