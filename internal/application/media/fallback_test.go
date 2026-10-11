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
type quotaArt struct {
	calls atomic.Int32

	// exhausted makes it answer that its allowance is used up.
	exhausted atomic.Bool
}

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

	if a.exhausted.Load() {
		return nil, provider.ErrQuotaExhausted
	}

	return nil, nil
}

// perGameArt has one cover per game, for any system, and only on the fallback pass, like IGDB.
type perGameArt struct {
	calls atomic.Int32

	// none makes it know no cover; limited makes it ask to slow down.
	none    atomic.Bool
	limited atomic.Bool
}

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

	switch {
	case a.limited.Load():
		return nil, provider.ErrRateLimited
	case a.none.Load():
		return nil, nil
	}

	return []media.CoverCandidate{{
		URL:      "https://img.test/per-game",
		Label:    q.Title,
		Provider: "per-game",
	}}, nil
}

type fallbackEnv struct {
	svc     *media.Service
	games   game.Repository
	assets  *gamedata.Store
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
		assets:  assets,
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

func TestCovers_fallbackMisses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	console := game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
	}

	t.Run("GIVEN the box art database's allowance is used up and the fallback provider knows no cover", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		env.quota.exhausted.Store(true)
		env.perGame.none.Store(true)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, console)

		t.Run("WHEN the cover is asked twice", func(t *testing.T) {
			_, err1 := env.svc.EditionCover(ctx, g.ID(), "PS4")
			_, err2 := env.svc.EditionCover(ctx, g.ID(), "PS4")

			t.Run("THEN the fallback provider, which truly answered none, is asked only once", func(t *testing.T) {
				require.ErrorIs(t, err1, media.ErrNoCover)
				require.ErrorIs(t, err2, media.ErrNoCover)
				assert.Equal(t, int32(1), env.perGame.calls.Load())
			})

			t.Run("AND no miss is remembered, since the box art database said nothing", func(t *testing.T) {
				_, marked := env.assets.CoverMissingSince(g.ID(), "PS4")
				assert.False(t, marked)
				assert.Equal(t, int32(2), env.quota.calls.Load(), "the limited provider is asked again")
			})
		})

		t.Run("WHEN the providers are reconfigured (retry missing covers)", func(t *testing.T) {
			_, err := env.svc.RefreshCovers(ctx, true)
			require.NoError(t, err)

			_, _ = env.svc.EditionCover(ctx, g.ID(), "PS4")

			t.Run("THEN the fallback provider is asked again", func(t *testing.T) {
				assert.Equal(t, int32(2), env.perGame.calls.Load())
			})
		})
	})

	t.Run("GIVEN a fallback provider asking to slow down, after a first pass that found nothing", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		env.perGame.limited.Store(true)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, console)

		t.Run("WHEN the cover is asked", func(t *testing.T) {
			_, err := env.svc.EditionCover(ctx, g.ID(), "PS4")

			t.Run("THEN no miss is remembered: the next view asks again", func(t *testing.T) {
				require.ErrorIs(t, err, media.ErrNoCover)

				_, marked := env.assets.CoverMissingSince(g.ID(), "PS4")
				assert.False(t, marked)
			})
		})
	})

	t.Run("GIVEN a link-less soundtrack key whose base game has a cover", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		base := saveGame(t, ctx, env.games, "Key Quest", game.Links{game.LinkSteam: "10"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		})
		add := saveGame(t, ctx, env.games, "Key Quest Soundtrack", nil, game.CopyDetails{
			Kind:     game.KindKey,
			Platform: "Steam",
		})

		_, err := env.svc.EditionCover(ctx, base.ID(), game.SystemPC)
		require.NoError(t, err)

		t.Run("WHEN its cover is asked", func(t *testing.T) {
			img, err := env.svc.EditionCover(ctx, add.ID(), game.SystemPC)

			t.Run("THEN it borrows the base game's before any fallback search", func(t *testing.T) {
				assert.Equal(t, "https://img.test/store-10", cover(t, img, err))
				assert.Zero(t, env.perGame.calls.Load())
			})
		})
	})
}
