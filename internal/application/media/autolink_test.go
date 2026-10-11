package media_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// linkFinder is a cover provider that only finds links, answering what the test sets.
type linkFinder struct {
	id    provider.ID
	order int

	mu    sync.Mutex
	links game.Links
	err   error
	calls int

	// during runs while the finder answers, like an edit the user makes meanwhile.
	during func()
}

func (f *linkFinder) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               f.id,
		Kind:             provider.KindCover,
		Name:             string(f.id),
		EnabledByDefault: true,
		DefaultOrder:     f.order,
	}
}

func (*linkFinder) Test(context.Context, schema.Settings) error { return nil }

func (*linkFinder) Applies(media.CoverQuery) bool { return false }

func (*linkFinder) Covers(context.Context, media.CoverQuery, schema.Settings) ([]media.CoverCandidate, error) {
	return nil, nil
}

func (f *linkFinder) FindLinks(context.Context, media.CoverQuery, schema.Settings) (game.Links, error) {
	f.mu.Lock()
	f.calls++
	links, err, during := f.links, f.err, f.during
	f.mu.Unlock()

	if during != nil {
		during()
	}

	return links, err
}

func (f *linkFinder) set(links game.Links, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.links, f.err = links, err
}

func (f *linkFinder) asked() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.calls
}

type autoLinkEnv struct {
	svc     *media.Service
	games   game.Repository
	assets  *gamedata.Store
	details media.DetailsStore
	trusted *linkFinder
	search  *linkFinder
	now     *time.Time
	catalog *catalog.Service

	// logs holds what the service logged.
	logs *lockedBuffer
}

// lockedBuffer is a buffer the worker's goroutine and the test can share.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func newAutoLinkEnv(t *testing.T, ctx context.Context) autoLinkEnv {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
	require.NoError(t, err)

	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	env := autoLinkEnv{
		games:   sqlite.NewGameRepository(db),
		assets:  assets,
		details: sqlite.NewDetailsStore(db),
		trusted: &linkFinder{
			id:    "trusted",
			order: 10,
		},
		search: &linkFinder{
			id:    "search",
			order: 20,
		},
		now:  &now,
		logs: &lockedBuffer{},
	}
	clock := func() time.Time { return *env.now }
	env.svc = media.NewService(env.games, sqlite.NewProviderRepository(db), assets, nil, env.details, echo{},
		clock, slog.New(slog.NewTextHandler(env.logs, nil)), media.Providers{
			Covers: []media.CoverProvider{env.search, env.trusted},
		})
	env.catalog = catalog.NewService(env.games, db, clock, env.svc, nil, nil)
	env.svc.SetGameLinker(env.catalog)

	return env
}

// runWorker runs the automatic-links worker until the test ends, stopping it before the database
// closes (cleanups run last-registered first).
func (env autoLinkEnv) runWorker(t *testing.T) {
	t.Helper()

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		env.svc.RunAutoLinker(ctx, time.Millisecond)
		close(done)
	}()

	t.Cleanup(func() {
		stop()
		<-done
	})
}

func (env autoLinkEnv) game(t *testing.T, ctx context.Context, id game.ID) *game.Game {
	t.Helper()

	g, err := env.games.Get(ctx, id)
	require.NoError(t, err)

	return g
}

func TestAutoLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	disc := game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
	}

	t.Run("GIVEN a PS4 disc no store links, with a cached cover and sheet, that the trusted finder knows", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		require.NoError(t, env.assets.PutCover(media.GameRef{
			ID:    g.ID(),
			Title: g.Title(),
		}, "PS4", media.Image{
			Data:        []byte("old"),
			ContentType: "image/png",
		}))
		require.NoError(t, env.details.Put(ctx, g.ID(), media.GameDetails{
			Language:  "en",
			Genres:    []string{"Puzzle"},
			FetchedAt: *env.now,
		}))
		env.trusted.set(game.Links{game.LinkSteam: "100", "gog": "200"}, nil)

		t.Run("WHEN links are looked for", func(t *testing.T) {
			require.NoError(t, env.svc.AutoLink(ctx, g.ID()))
			got := env.game(t, ctx, g.ID())

			t.Run("THEN the trusted finder's links are added as automatic, and the title search is not asked", func(t *testing.T) {
				assert.Equal(t, game.Links{game.LinkSteam: "100", "gog": "200"}, got.Links())
				assert.Equal(t, []string{"gog", game.LinkSteam}, got.AutoLinks())
				assert.Zero(t, env.search.asked())
			})

			t.Run("AND the cached cover and sheet are dropped, so Steam's fill them in", func(t *testing.T) {
				_, ok, err := env.assets.GetCover(g.ID(), "PS4")
				require.NoError(t, err)
				assert.False(t, ok)

				_, cached, err := env.details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				assert.False(t, cached)
			})
		})
	})

	t.Run("GIVEN a game the trusted finder does not know", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		env.trusted.set(nil, provider.BadSettings("enter the credentials"))
		env.search.set(game.Links{game.LinkSteam: "4242"}, nil)

		t.Run("WHEN links are looked for", func(t *testing.T) {
			require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

			t.Run("THEN the title search's link is added", func(t *testing.T) {
				assert.Equal(t, "4242", env.game(t, ctx, g.ID()).Links()[game.LinkSteam])
			})
		})
	})

	t.Run("GIVEN a game nobody knows", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		updated := env.game(t, ctx, g.ID()).UpdatedAt()

		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		t.Run("THEN the search is recorded without counting as a change of the game", func(t *testing.T) {
			got := env.game(t, ctx, g.ID())
			assert.True(t, got.AutoLinkTriedAt().Equal(*env.now))
			assert.True(t, got.UpdatedAt().Equal(updated))
		})

		t.Run("WHEN asked again within 30 days THEN nobody is asked again", func(t *testing.T) {
			*env.now = env.now.Add(29 * 24 * time.Hour)
			require.NoError(t, env.svc.AutoLink(ctx, g.ID()))
			assert.Equal(t, 1, env.trusted.asked())
		})

		t.Run("WHEN asked after 30 days THEN the finders are asked again", func(t *testing.T) {
			*env.now = env.now.Add(2 * 24 * time.Hour)
			require.NoError(t, env.svc.AutoLink(ctx, g.ID()))
			assert.Equal(t, 2, env.trusted.asked())
		})
	})

	t.Run("GIVEN a finder asking to slow down", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		env.trusted.set(nil, provider.ErrRateLimited)

		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		t.Run("THEN nothing is recorded on the game, and it is retried within the hour, not at once", func(t *testing.T) {
			assert.True(t, env.game(t, ctx, g.ID()).AutoLinkTriedAt().IsZero())
			assert.Zero(t, env.search.asked(), "a rate limit stops the search: the next finder is less reliable")

			require.NoError(t, env.svc.AutoLink(ctx, g.ID()))
			assert.Equal(t, 1, env.trusted.asked())

			*env.now = env.now.Add(61 * time.Minute)
			env.trusted.set(game.Links{game.LinkSteam: "100"}, nil)
			require.NoError(t, env.svc.AutoLink(ctx, g.ID()))
			assert.Equal(t, 2, env.trusted.asked())
			assert.Equal(t, "100", env.game(t, ctx, g.ID()).Links()[game.LinkSteam])
		})
	})

	t.Run("GIVEN a Steam link the user removed", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		env.trusted.set(game.Links{game.LinkSteam: "100"}, nil)
		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		removed := env.game(t, ctx, g.ID())
		info := removed.Info()
		info.Links = nil
		_, err := removed.UpdateInfo(info, *env.now)
		require.NoError(t, err)
		require.NoError(t, env.games.Save(ctx, removed))

		*env.now = env.now.Add(31 * 24 * time.Hour)
		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		t.Run("THEN it is not added again", func(t *testing.T) {
			assert.Empty(t, env.game(t, ctx, g.ID()).Links())
		})
	})

	t.Run("GIVEN a game linked to Steam", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Steam Quest", game.Links{game.LinkSteam: "620"}, disc)

		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		t.Run("THEN nothing is looked for", func(t *testing.T) {
			assert.Zero(t, env.trusted.asked())
			assert.True(t, env.game(t, ctx, g.ID()).AutoLinkTriedAt().IsZero())
		})
	})

	t.Run("GIVEN a disabled finder", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		_, err := env.svc.ConfigureProvider(ctx, "trusted", false, schema.Settings{})
		require.NoError(t, err)
		env.search.set(game.Links{game.LinkSteam: "4242"}, nil)

		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		t.Run("THEN it is not asked", func(t *testing.T) {
			assert.Zero(t, env.trusted.asked())
			assert.Equal(t, 1, env.search.asked())
		})
	})

	t.Run("GIVEN the user edits the game while a finder answers", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		env.trusted.set(game.Links{game.LinkSteam: "100"}, nil)
		env.trusted.during = func() {
			_, err := env.catalog.AddCopy(ctx, g.ID(), game.CopyDetails{
				Kind:     game.KindPhysical,
				Platform: "PS5",
			}, nil)
			assert.NoError(t, err)
		}

		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		t.Run("THEN the edit and the link are both kept", func(t *testing.T) {
			got := env.game(t, ctx, g.ID())
			assert.Len(t, got.Copies(), 2)
			assert.Equal(t, "100", got.Links()[game.LinkSteam])
		})
	})

	t.Run("GIVEN no service to save links", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		env.svc.SetGameLinker(nil)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		env.trusted.set(game.Links{game.LinkSteam: "100"}, nil)

		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		t.Run("THEN nothing is looked for", func(t *testing.T) {
			assert.Zero(t, env.trusted.asked())
		})
	})

	t.Run("GIVEN a finder failing for a moment (an outage, a timeout)", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		env.trusted.set(nil, errors.New("context deadline exceeded"))

		require.NoError(t, env.svc.AutoLink(ctx, g.ID()))

		t.Run("THEN the game is not marked as searched: it waits an hour in memory, like a rate limit", func(t *testing.T) {
			assert.True(t, env.game(t, ctx, g.ID()).AutoLinkTriedAt().IsZero())
			assert.Zero(t, env.search.asked())

			_, _, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)
			assert.Zero(t, env.svc.AutoLinkQueueLen(), "a game waiting its hour is not queued")

			require.NoError(t, env.svc.AutoLink(ctx, g.ID()))
			assert.Equal(t, 1, env.trusted.asked())

			*env.now = env.now.Add(61 * time.Minute)
			env.trusted.set(nil, provider.ErrUnavailable)
			require.NoError(t, env.svc.AutoLink(ctx, g.ID()))
			assert.Equal(t, 2, env.trusted.asked())
			assert.True(t, env.game(t, ctx, g.ID()).AutoLinkTriedAt().IsZero())
		})
	})

	t.Run("GIVEN a sheet opened twice for a game without a Steam link, and the background worker", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		g := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		env.trusted.set(game.Links{game.LinkSteam: "100"}, nil)

		for range 2 {
			_, _, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)
		}

		t.Run("THEN the game is queued once, not searched yet", func(t *testing.T) {
			assert.Equal(t, 1, env.svc.AutoLinkQueueLen())
			assert.Zero(t, env.trusted.asked())
		})

		t.Run("WHEN the worker runs", func(t *testing.T) {
			env.runWorker(t)

			t.Run("THEN the links are looked for once, in the background", func(t *testing.T) {
				assert.Eventually(t, func() bool {
					got, err := env.games.Get(ctx, g.ID())
					return err == nil && got.Links()[game.LinkSteam] == "100"
				}, 5*time.Second, 10*time.Millisecond)
				assert.Equal(t, 1, env.trusted.asked())
			})
		})
	})

	t.Run("GIVEN a game waiting its hour after a rate limit, queued before the limit, and another game queued after it", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		waiting := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		next := saveGame(t, ctx, env.games, "Disc Saga", nil, disc)

		_, _, err := env.svc.GameDetails(ctx, waiting.ID(), "en", false)
		require.NoError(t, err)

		env.trusted.set(nil, provider.ErrRateLimited)
		require.NoError(t, env.svc.AutoLink(ctx, waiting.ID()))
		env.trusted.set(game.Links{game.LinkSteam: "100"}, nil)

		_, _, err = env.svc.GameDetails(ctx, next.ID(), "en", false)
		require.NoError(t, err)

		t.Run("WHEN its sheet is opened again", func(t *testing.T) {
			_, _, err := env.svc.GameDetails(ctx, waiting.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it is not queued twice", func(t *testing.T) {
				assert.Equal(t, 2, env.svc.AutoLinkQueueLen())
			})
		})

		t.Run("WHEN the worker runs", func(t *testing.T) {
			env.runWorker(t)

			t.Run("THEN skipping the waiting game is no reason to rest: the next game is searched at once", func(t *testing.T) {
				assert.Eventually(t, func() bool {
					got, err := env.games.Get(ctx, next.ID())
					return err == nil && got.Links()[game.LinkSteam] == "100"
				}, 5*time.Second, 10*time.Millisecond)
				assert.Equal(t, 2, env.trusted.asked(), "the waiting game is not asked again")
			})
		})
	})

	t.Run("GIVEN a queued game deleted before the worker reaches it", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		gone := saveGame(t, ctx, env.games, "Console Quest", nil, disc)
		next := saveGame(t, ctx, env.games, "Disc Saga", nil, disc)
		env.trusted.set(game.Links{game.LinkSteam: "100"}, nil)

		for _, g := range []*game.Game{gone, next} {
			_, _, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)
		}

		require.NoError(t, env.games.Delete(ctx, gone.ID()))

		t.Run("WHEN the worker runs", func(t *testing.T) {
			env.runWorker(t)

			t.Run("THEN the deleted game is skipped quietly and the next one searched", func(t *testing.T) {
				assert.Eventually(t, func() bool {
					got, err := env.games.Get(ctx, next.ID())
					return err == nil && got.Links()[game.LinkSteam] == "100"
				}, 5*time.Second, 10*time.Millisecond)
				assert.NotContains(t, env.logs.String(), "level=WARN")
			})
		})
	})

	t.Run("GIVEN the worker running, WHEN the server shuts down", func(t *testing.T) {
		env := newAutoLinkEnv(t, ctx)
		wctx, stop := context.WithCancel(ctx)
		done := make(chan struct{})

		go func() {
			env.svc.RunAutoLinker(wctx, time.Millisecond)
			close(done)
		}()

		stop()

		t.Run("THEN it stops", func(t *testing.T) {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("the worker did not stop")
			}
		})
	})
}
