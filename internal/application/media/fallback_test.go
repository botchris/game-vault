package media_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/media"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// countingStore has art for the PC edition of games linked to Steam, like the Steam store.
type countingStore struct {
	calls atomic.Int32

	// fallbackAsked records whether it was ever asked with the fallback pass's query.
	fallbackAsked atomic.Bool
}

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

func (*countingStore) Applies(q media.GameQuery) bool {
	return q.ForPC() && q.Links[game.LinkSteam] != ""
}

func (s *countingStore) Covers(_ context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	s.calls.Add(1)

	if q.Fallback {
		s.fallbackAsked.Store(true)
	}

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

	// art makes it find a box.
	art atomic.Bool

	// browsed records whether the last query was the cover picker's (GameQuery.Browse).
	browsed atomic.Bool
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

func (*quotaArt) Applies(q media.GameQuery) bool {
	return q.HasPhysical() || !q.HasStoreLink() || q.Fallback
}

func (a *quotaArt) Covers(_ context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	a.calls.Add(1)
	a.browsed.Store(q.Browse)

	if a.exhausted.Load() {
		return nil, provider.ErrQuotaExhausted
	}

	if a.art.Load() {
		return []media.CoverCandidate{{
			URL:      "https://img.test/quota",
			Provider: "quota",
		}}, nil
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

func (*perGameArt) Applies(q media.GameQuery) bool { return q.Fallback }

func (a *perGameArt) Covers(_ context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
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
	svc     *media.Services
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
	env.svc = newServices(db, env.games, assets, echo{}, []providers.Provider{env.store, env.quota, env.perGame})

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
			img, err := env.svc.Covers.EditionCover(ctx, g.ID(), "PS4")

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
			img, err := env.svc.Covers.EditionCover(ctx, g.ID(), game.SystemPC)

			t.Run("THEN the fallback pass finds one", func(t *testing.T) {
				assert.Equal(t, "https://img.test/per-game", cover(t, img, err))
				assert.Equal(t, int32(1), env.quota.calls.Load())
			})
		})

		t.Run("WHEN its cover candidates are listed", func(t *testing.T) {
			candidates, warnings, _, err := env.svc.Covers.CoverCandidates(ctx, g.ID(), game.SystemPC)
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
			img, err := env.svc.Covers.EditionCover(ctx, g.ID(), game.SystemPC)

			t.Run("THEN the store's art is used and nothing else is asked, as before", func(t *testing.T) {
				assert.Equal(t, "https://img.test/store-620", cover(t, img, err))
				assert.Zero(t, env.quota.calls.Load())
				assert.Zero(t, env.perGame.calls.Load())
			})
		})

		t.Run("WHEN its cover candidates are listed", func(t *testing.T) {
			env.quota.art.Store(true)

			candidates, _, _, err := env.svc.Covers.CoverCandidates(ctx, g.ID(), game.SystemPC)
			require.NoError(t, err)

			t.Run("THEN the fallback providers are offered too, after the store's art", func(t *testing.T) {
				ids := make([]provider.ID, 0, len(candidates))
				for _, c := range candidates {
					ids = append(ids, c.Provider)
				}

				assert.Equal(t, []provider.ID{"store", "quota", "per-game"}, ids)
			})

			t.Run("AND each provider is asked once: one request of the quota per picker", func(t *testing.T) {
				assert.Equal(t, int32(1), env.quota.calls.Load())
				assert.Equal(t, int32(1), env.perGame.calls.Load())
			})

			t.Run("AND the store, which applies to the first pass, gets the first pass's query", func(t *testing.T) {
				assert.False(t, env.store.fallbackAsked.Load())
			})
		})
	})
}

func TestCoverCandidates_providerOnBothPasses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a disc, which the quota-limited provider applies to on both passes", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		env.quota.art.Store(true)
		g := saveGame(t, ctx, env.games, "Disc Quest", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN its cover candidates are listed", func(t *testing.T) {
			candidates, _, _, err := env.svc.Covers.CoverCandidates(ctx, g.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN it is asked once, its box first, then the fallback-only provider's", func(t *testing.T) {
				assert.Equal(t, int32(1), env.quota.calls.Load())
				require.Len(t, candidates, 2)
				assert.Equal(t, provider.ID("quota"), candidates[0].Provider)
				assert.Equal(t, provider.ID("per-game"), candidates[1].Provider)
			})
		})
	})
}

// TestGameQuery_browse checks that only the cover picker asks for near matches: the automatic
// resolution stores the first candidate, which must then be the game itself.
func TestGameQuery_browse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a disc", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		env.quota.art.Store(true)
		g := saveGame(t, ctx, env.games, "Disc Quest", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN its cover candidates are listed in the picker", func(t *testing.T) {
			_, _, _, err := env.svc.Covers.CoverCandidates(ctx, g.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN the providers are asked to browse", func(t *testing.T) {
				assert.True(t, env.quota.browsed.Load())
			})
		})

		t.Run("WHEN its cover is resolved automatically", func(t *testing.T) {
			_, err := env.svc.Covers.EditionCover(ctx, g.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN the providers are not asked to browse", func(t *testing.T) {
				assert.False(t, env.quota.browsed.Load())
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
			_, err1 := env.svc.Covers.EditionCover(ctx, g.ID(), "PS4")
			_, err2 := env.svc.Covers.EditionCover(ctx, g.ID(), "PS4")

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
			_, err := env.svc.Covers.RefreshCovers(ctx, true)
			require.NoError(t, err)

			_, _ = env.svc.Covers.EditionCover(ctx, g.ID(), "PS4")

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
			_, err := env.svc.Covers.EditionCover(ctx, g.ID(), "PS4")

			t.Run("THEN no miss is remembered: the next view asks again", func(t *testing.T) {
				require.ErrorIs(t, err, media.ErrNoCover)

				_, marked := env.assets.CoverMissingSince(g.ID(), "PS4")
				assert.False(t, marked)
			})

			t.Run("AND it is the provider being unavailable, not a missing cover", func(t *testing.T) {
				assert.Equal(t, errkind.Unavailable, errkind.Of(err))
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

		_, err := env.svc.Covers.EditionCover(ctx, base.ID(), game.SystemPC)
		require.NoError(t, err)

		t.Run("WHEN its cover is asked", func(t *testing.T) {
			img, err := env.svc.Covers.EditionCover(ctx, add.ID(), game.SystemPC)

			t.Run("THEN it borrows the base game's before any fallback search", func(t *testing.T) {
				assert.Equal(t, "https://img.test/store-10", cover(t, img, err))
				assert.Zero(t, env.perGame.calls.Load())
			})
		})
	})
}

func TestCovers_pcBoxForConsoleOnlyGames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	ps4 := game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
	}

	t.Run("GIVEN a PS4-only game linked to Steam", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", game.Links{"psn": "PPSA0001", game.LinkSteam: "100"}, ps4)

		t.Run("WHEN its PC box is asked", func(t *testing.T) {
			img, err := env.svc.Covers.EditionCover(ctx, g.ID(), game.SystemPC)

			t.Run("THEN the PC store's art is served, and neither the box art database nor the fallback providers are asked", func(t *testing.T) {
				assert.Equal(t, "https://img.test/store-100", cover(t, img, err))
				assert.Zero(t, env.quota.calls.Load())
				assert.Zero(t, env.perGame.calls.Load())
			})
		})
	})

	t.Run("GIVEN a PS4-only game linked to the PlayStation store only", func(t *testing.T) {
		env := newFallbackEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", game.Links{"psn": "PPSA0002"}, ps4)

		t.Run("WHEN its PC box is asked", func(t *testing.T) {
			_, err := env.svc.Covers.EditionCover(ctx, g.ID(), game.SystemPC)

			t.Run("THEN there is none, and nothing is asked", func(t *testing.T) {
				require.ErrorIs(t, err, media.ErrNoCover)
				assert.Zero(t, env.store.calls.Load())
				assert.Zero(t, env.quota.calls.Load())
			})
		})
	})
}

// missingStore applies to PC editions linked to Steam, like the Steam store, records the queries it
// gets, and has no art.
type missingStore struct {
	calls   atomic.Int32
	queries []media.GameQuery
}

func (*missingStore) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "missing-store",
		Kind:             provider.KindCover,
		Name:             "Missing store",
		EnabledByDefault: true,
		DefaultOrder:     20,
	}
}

func (*missingStore) Test(context.Context, schema.Settings) error { return nil }

func (*missingStore) Applies(q media.GameQuery) bool {
	return q.ForPC() && q.Links[game.LinkSteam] != ""
}

func (s *missingStore) Covers(_ context.Context, q media.GameQuery, _ schema.Settings) ([]media.CoverCandidate, error) {
	s.calls.Add(1)
	s.queries = append(s.queries, q)

	return nil, nil
}

func TestCovers_pcBoxMisses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	setup := func(t *testing.T) (*media.Services, game.Repository, *gamedata.Store, *missingStore, *perGameArt) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		games, store, perGame := sqlite.NewGameRepository(db), &missingStore{}, &perGameArt{}
		svc := newServices(db, games, assets, echo{}, []providers.Provider{store, perGame})

		return svc, games, assets, store, perGame
	}

	t.Run("GIVEN a PS4-only game linked to Steam, whose store has no art", func(t *testing.T) {
		svc, games, assets, store, perGame := setup(t)
		g := saveGame(t, ctx, games, "Console Quest", game.Links{game.LinkSteam: "100"}, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN its PC box is asked twice", func(t *testing.T) {
			_, err1 := svc.Covers.EditionCover(ctx, g.ID(), game.SystemPC)
			_, err2 := svc.Covers.EditionCover(ctx, g.ID(), game.SystemPC)

			t.Run("THEN the miss is marked under PC and the second answer comes from the marker", func(t *testing.T) {
				require.ErrorIs(t, err1, media.ErrNoCover)
				require.ErrorIs(t, err2, media.ErrNoCover)

				_, marked := assets.CoverMissingSince(g.ID(), game.SystemPC)
				assert.True(t, marked)
				assert.Equal(t, int32(1), store.calls.Load())
			})

			t.Run("AND only store links were usable, with no fallback pass", func(t *testing.T) {
				require.NotEmpty(t, store.queries)
				assert.True(t, store.queries[0].LinksOnly)
				assert.Zero(t, perGame.calls.Load())
			})
		})
	})

	t.Run("GIVEN a game with a real PC copy linked to Steam, whose store has no art", func(t *testing.T) {
		svc, games, _, store, _ := setup(t)
		g := saveGame(t, ctx, games, "PC Quest", game.Links{game.LinkSteam: "100"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		})

		t.Run("WHEN its PC cover is asked", func(t *testing.T) {
			img, err := svc.Covers.EditionCover(ctx, g.ID(), game.SystemPC)

			t.Run("THEN the fallback pass still runs, as for any real edition", func(t *testing.T) {
				assert.Equal(t, "https://img.test/per-game", cover(t, img, err))
				assert.False(t, store.queries[0].LinksOnly)
			})
		})
	})

	t.Run("GIVEN a PS4-only soundtrack linked to Steam, whose base game has a PC cover", func(t *testing.T) {
		svc, games, _, store, _ := setup(t)
		base := saveGame(t, ctx, games, "Base Quest", nil, game.CopyDetails{
			Kind:     game.KindKey,
			Platform: "Epic Games",
		})
		require.NoError(t, func() error {
			g, err := games.Get(ctx, base.ID())
			if err != nil {
				return err
			}

			if err := g.SetEditionCover(game.SystemPC, game.EditionCover{URL: "https://img.test/base-pc"}, time.Now()); err != nil {
				return err
			}

			return games.Save(ctx, g)
		}())

		_, err := svc.Covers.EditionCover(ctx, base.ID(), game.SystemPC)
		require.NoError(t, err)

		add := saveGame(t, ctx, games, "Base Quest Soundtrack", game.Links{game.LinkSteam: "100"}, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN its PC box is asked", func(t *testing.T) {
			_, err := svc.Covers.EditionCover(ctx, add.ID(), game.SystemPC)

			t.Run("THEN it does not borrow the base game's: a PC box is the store's art, or none", func(t *testing.T) {
				require.ErrorIs(t, err, media.ErrNoCover)
				assert.Equal(t, int32(1), store.calls.Load())
			})
		})
	})
}
