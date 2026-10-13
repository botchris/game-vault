package media_test

import (
	"context"
	"errors"
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

func (*storeDetails) Applies(q media.GameQuery) bool { return q.Links[game.LinkSteam] != "" }

func (p *storeDetails) Details(context.Context, media.GameQuery, string, schema.Settings) (*media.GameDetails, error) {
	p.calls.Add(1)

	return p.sheet, nil
}

// quotaDetails is a metadata provider that, like TheGamesDB, keeps its quota for physical copies,
// games no store knows and the fallback pass.
type quotaDetails struct {
	limited atomic.Bool

	// failing makes it answer like a server error (HTTP 5xx), not a rate limit.
	failing atomic.Bool
	calls   atomic.Int32

	// asked is the query of the last call.
	asked media.GameQuery
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

func (*quotaDetails) Applies(q media.GameQuery) bool {
	return q.HasPhysical() || !q.HasStoreLink() || q.Fallback
}

func (p *quotaDetails) Details(_ context.Context, q media.GameQuery, _ string, _ schema.Settings) (*media.GameDetails, error) {
	p.calls.Add(1)
	p.asked = q

	if p.limited.Load() {
		return nil, provider.ErrRateLimited
	}

	if p.failing.Load() {
		return nil, errors.New("server error 503: try again later")
	}

	return &media.GameDetails{
		Summary: "A console adventure.",
		Genres:  []string{"Adventure"},
	}, nil
}

type detailsEnv struct {
	svc     *media.Services
	games   game.Repository
	details media.DetailsStore
	store   *storeDetails
	quota   *quotaDetails
}

func newDetailsEnv(t *testing.T, ctx context.Context, now func() time.Time, storeSheet *media.GameDetails, opts ...media.Option) detailsEnv {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
	require.NoError(t, err)

	games, details := sqlite.NewGameRepository(db), sqlite.NewDetailsStore(db)
	store, quota := &storeDetails{sheet: storeSheet}, &quotaDetails{}
	svc := newServicesAt(db, now, games, assets, echo{}, []providers.Provider{store, quota}, append([]media.Option{media.WithDetailsStore(details)}, opts...)...)

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
			d, warnings, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
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
			d, _, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
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
			d, warnings, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it is empty this time; the provider's status, not the sheet, says it is busy", func(t *testing.T) {
				assert.Empty(t, d.Summary)
				assert.Empty(t, warnings)
				assert.Len(t, env.svc.Registry.Statuses(), 1)
			})

			t.Run("AND nothing is cached, so the next view asks again", func(t *testing.T) {
				_, ok, err := env.details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				assert.False(t, ok)
			})
		})
	})
}

func TestGameDetails_sheetsCachedByOlderLogic(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// These sheets carry no logic version: the code before the fallback pass cached them, an hour ago
	// (well within the 30-day TTL), so only the version can tell they are outdated.
	// still within the 30-day TTL.
	now := func() time.Time { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
	before := now().Add(-time.Hour)

	t.Run("GIVEN a PlayStation-only game with an empty sheet cached by the code before the fallback pass", func(t *testing.T) {
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
			first, _, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			second, _, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
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
			d, _, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the fallback pass runs", func(t *testing.T) {
				assert.Equal(t, "A console adventure.", d.Summary)
				assert.Equal(t, int32(1), env.quota.calls.Load())
			})
		})
	})

	t.Run("GIVEN a game with a sheet that has a summary, cached by the code before the fallback pass", func(t *testing.T) {
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
			d, _, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the cached sheet stays and no provider is asked", func(t *testing.T) {
				assert.Equal(t, "Written by hand long ago.", d.Summary)
				assert.Zero(t, env.quota.calls.Load())
				assert.Zero(t, env.store.calls.Load())
			})
		})
	})
}

func TestGameDetails_sparseSheetWithAFailedFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a game linked to Steam and PlayStation, and a quota-limited provider failing with a server error", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, &media.GameDetails{Genres: []string{"Puzzle"}})
		env.quota.failing.Store(true)
		g := saveGame(t, ctx, env.games, "Console Quest", game.Links{
			"playstation":  "PPSA0001",
			game.LinkSteam: "10",
		}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "PS4",
		})

		t.Run("WHEN its sheet is opened and the store knows only its genres", func(t *testing.T) {
			d, warnings, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it shows the genres and the failure", func(t *testing.T) {
				assert.Equal(t, []string{"Puzzle"}, d.Genres)
				assert.Len(t, warnings, 1)
			})

			t.Run("AND the sparse sheet is cached as still owing its fallback pass", func(t *testing.T) {
				cached, ok, err := env.details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				require.True(t, ok)
				assert.True(t, cached.FallbackSkipped)
			})
		})

		t.Run("WHEN the provider recovers and the sheet is opened again", func(t *testing.T) {
			env.quota.failing.Store(false)

			d, warnings, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the fallback pass fills it", func(t *testing.T) {
				assert.Empty(t, warnings)
				assert.Equal(t, "A console adventure.", d.Summary)
			})
		})
	})
}

// sheetDetails is a metadata provider that knows every game and answers a fixed sheet.
type sheetDetails struct {
	id    provider.ID
	order int
	sheet media.GameDetails
}

func (p *sheetDetails) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               p.id,
		Kind:             provider.KindMetadata,
		Name:             string(p.id),
		EnabledByDefault: true,
		DefaultOrder:     p.order,
	}
}

func (*sheetDetails) Test(context.Context, schema.Settings) error { return nil }

func (*sheetDetails) Applies(media.GameQuery) bool { return true }

func (p *sheetDetails) Details(context.Context, media.GameQuery, string, schema.Settings) (*media.GameDetails, error) {
	sheet := p.sheet
	return &sheet, nil
}

func TestGameDetails_summarySourceAndTrailers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an encyclopedia whose summary needs credit, then a game database, both with a trailer", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		credit := media.Attribution{
			Name:       "Wikipedia",
			URL:        "https://en.wikipedia.org/wiki/Example_Quest",
			License:    "CC BY-SA 4.0",
			LicenseURL: "https://creativecommons.org/licenses/by-sa/4.0/",
		}
		encyclopedia := &sheetDetails{
			id:    "encyclopedia",
			order: 1,
			sheet: media.GameDetails{
				Summary:       "Example Quest is a game.",
				SummarySource: credit,
				Videos:        []media.Video{{YouTubeID: "yt-shared"}},
			},
		}
		database := &sheetDetails{
			id:    "database",
			order: 2,
			sheet: media.GameDetails{
				Summary:     "A game database's summary.",
				Genres:      []string{"Adventure"},
				Screenshots: []media.Screenshot{{FullURL: "https://cdn.test/1.jpg"}},
				Videos:      []media.Video{{YouTubeID: "yt-shared"}, {YouTubeID: "yt-own"}},
			},
		}

		games, details := sqlite.NewGameRepository(db), sqlite.NewDetailsStore(db)
		svc := newServices(db, games, assets, echo{}, []providers.Provider{database, encyclopedia}, media.WithDetailsStore(details))
		g := saveGame(t, ctx, games, "Example Quest", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN the sheet is asked", func(t *testing.T) {
			d, warnings, err := svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)
			require.Empty(t, warnings)

			t.Run("THEN the first summary wins together with its credit", func(t *testing.T) {
				assert.Equal(t, "Example Quest is a game.", d.Summary)
				assert.Equal(t, credit, d.SummarySource)

				t.Run("AND the later provider fills the gaps", func(t *testing.T) {
					assert.Equal(t, []string{"Adventure"}, d.Genres)
					assert.Len(t, d.Screenshots, 1)
				})

				t.Run("AND a trailer both know is listed once", func(t *testing.T) {
					ids := make([]string, 0, len(d.Videos))
					for _, v := range d.Videos {
						ids = append(ids, v.YouTubeID)
					}

					assert.Equal(t, []string{"yt-shared", "yt-own"}, ids)
				})
			})

			t.Run("AND the credit is cached with the sheet", func(t *testing.T) {
				cached, ok, err := details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				require.True(t, ok)
				assert.Equal(t, credit, cached.SummarySource)
			})
		})
	})
}

func TestGameDetails_notFoundBy(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a Steam game the store does not know, and a database that does", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, nil)
		g := saveGame(t, ctx, env.games, "Gone Quest", game.Links{game.LinkSteam: "10"}, game.CopyDetails{
			Kind:     game.KindLibrary,
			Platform: "Steam",
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			d, warnings, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the store is listed as tried without a match, which is no failure", func(t *testing.T) {
				assert.Equal(t, []provider.ID{"store-details"}, d.NotFoundBy)
				assert.Empty(t, warnings)
				assert.Equal(t, "A console adventure.", d.Summary)
			})

			t.Run("AND the sheet is cached with it", func(t *testing.T) {
				cached, ok, err := env.details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				require.True(t, ok)
				assert.Equal(t, []provider.ID{"store-details"}, cached.NotFoundBy)
			})
		})
	})

	t.Run("GIVEN a game the store does not apply to", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, nil)
		g := saveGame(t, ctx, env.games, "Disc Quest", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			d, _, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the store is not listed: it was not tried", func(t *testing.T) {
				assert.Empty(t, d.NotFoundBy)
			})
		})
	})
}

func TestGameDetails_notFoundByLeavesOutLimitedProviders(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a disc whose only provider asks to slow down", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, nil)
		env.quota.limited.Store(true)
		g := saveGame(t, ctx, env.games, "Disc Quest", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			d, warnings, err := env.svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it is not \"no clear match\" (it said nothing about the game), nor a warning on the game", func(t *testing.T) {
				assert.Empty(t, warnings)
				assert.Empty(t, d.NotFoundBy)
			})
		})
	})
}
