package media_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/adapters/outbound/thegamesdb"
	"gamevault/internal/application/media"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// busyTheGamesDB is a fake TheGamesDB that answers searches with HTTP 429 while busy is set, as the
// real one did to a burst of cover lookups on 2026-10-10 and does once the key's monthly allowance
// is used up, and with one PS3 box and a sheet otherwise. API/Limit (free) reports remaining
// requests left, renewing in refresh seconds (0 for an unknown key).
func busyTheGamesDB(t *testing.T, busy *atomic.Bool, remaining, refresh int) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/API/Limit":
			fmt.Fprintf(w, `{"remaining_monthly_allowance":%d,"extra_allowance":0,"allowance_refresh_timer":%d}`, remaining, refresh)
		case r.URL.Path == "/v1/Platforms":
			w.Write([]byte(`{"data":{"platforms":{"12":{"id":12,"name":"Sony Playstation 3","alias":"sony-playstation-3"}}}}`))
		case busy.Load():
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"code":429,"status":"Unknown Error Code","remaining_monthly_allowance":0}`))
		case r.URL.Path == "/v1.1/Games/ByGameName":
			w.Write([]byte(`{"data":{"games":[{"id":1,"game_title":"Halo 3","release_date":"2007-09-25","platform":12,"overview":"Finish the fight."}]},
			 "include":{"boxart":{"base_url":{"large":"https://cdn.test/large/"},"data":{"1":[{"side":"front","filename":"front/1.jpg"}]}}}}`))
		default:
			w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// otherDetails is a metadata provider that always knows the game's genres, like the Steam store.
type otherDetails struct{}

func (otherDetails) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "other-details",
		Kind:             provider.KindMetadata,
		Name:             "Other",
		EnabledByDefault: true,
		DefaultOrder:     1,
	}
}

func (otherDetails) Test(context.Context, schema.Settings) error { return nil }

func (otherDetails) Applies(media.GameQuery) bool { return true }

func (otherDetails) Details(context.Context, media.GameQuery, string, schema.Settings) (*media.GameDetails, error) {
	return &media.GameDetails{Genres: []string{"Shooter"}}, nil
}

func TestRateLimitedProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN TheGamesDB asking to slow down, and a game with a PS3 disc", func(t *testing.T) {
		var busy atomic.Bool

		busy.Store(true)

		api := apiclient.New(busyTheGamesDB(t, &busy, 500, 1036800).URL)
		tgdb := &thegamesdb.Provider{API: api}

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		games, details := sqlite.NewGameRepository(db), sqlite.NewDetailsStore(db)
		svc := newServices(db, games, assets, echo{}, []providers.Provider{tgdb, otherDetails{}, thegamesdb.NewDetails(tgdb)}, media.WithDetailsStore(details))
		_, err = svc.Registry.ConfigureProvider(ctx, thegamesdb.ID, true, schema.Settings{"api_key": "k"})
		require.NoError(t, err)

		g := saveGame(t, ctx, games, "Halo 3", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		})

		t.Run("WHEN the PS3 edition's cover is asked", func(t *testing.T) {
			_, err := svc.Covers.EditionCover(ctx, g.ID(), "PS3")

			t.Run("THEN it has no cover this time, and no miss is remembered", func(t *testing.T) {
				require.ErrorIs(t, err, media.ErrNoCover)
				assert.ErrorIs(t, err, provider.ErrRateLimited)

				_, marked := assets.CoverMissingSince(g.ID(), "PS3")
				assert.False(t, marked)
			})
		})

		t.Run("WHEN the game sheet is asked", func(t *testing.T) {
			d, warnings, err := svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it shows what the other provider knows; TheGamesDB's status, not the sheet, says it is busy", func(t *testing.T) {
				assert.Equal(t, []string{"Shooter"}, d.Genres)
				assert.Empty(t, warnings)

				st := svc.Registry.Statuses()
				require.NotEmpty(t, st)
				assert.Equal(t, providers.StatusRateLimited, st[0].Kind)
				assert.Contains(t, st[0].Message, "too many requests")
			})

			t.Run("AND the sheet is not cached", func(t *testing.T) {
				_, cached, err := details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				assert.False(t, cached)
			})
		})

		t.Run("WHEN TheGamesDB answers again and both are asked once more", func(t *testing.T) {
			busy.Store(false)

			img, coverErr := svc.Covers.EditionCover(ctx, g.ID(), "PS3")
			d, warnings, err := svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the edition gets its box and the sheet TheGamesDB's summary", func(t *testing.T) {
				assert.Equal(t, "https://cdn.test/large/front/1.jpg", cover(t, img, coverErr))
				assert.Empty(t, warnings)
				assert.Equal(t, "Finish the fight.", d.Summary)

				t.Run("AND its status is cleared", func(t *testing.T) {
					assert.Empty(t, svc.Registry.Statuses())
				})
			})
		})
	})
}

func TestQuotaExhaustedProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, quota := range []struct {
		name      string
		remaining int
		refresh   int
		cause     error
		message   string
		kind      providers.StatusKind
	}{
		{
			"this month's allowance used up", 0, 1036800, provider.ErrQuotaExhausted,
			"the API key has used this month's allowance: covers and sheets resume when it renews in 12 days",
			providers.StatusQuota,
		},
		{
			"an unknown key", 0, 0, provider.ErrRateLimited,
			"the API key was not recognized: check it in the provider settings",
			providers.StatusSettings,
		},
	} {
		testUnavailableTheGamesDB(t, ctx, quota.name, quota.remaining, quota.refresh, quota.cause, quota.message, quota.kind)
	}
}

// testUnavailableTheGamesDB checks a TheGamesDB that answers HTTP 429 and whose API/Limit reports
// remaining and refresh: covers fail with cause, TheGamesDB's status says message (of kind), the
// sheet shows no warning, and nothing is remembered from it.
func testUnavailableTheGamesDB(t *testing.T, ctx context.Context, name string, remaining, refresh int, cause error, message string,
	kind providers.StatusKind) {
	t.Helper()

	t.Run("GIVEN TheGamesDB answering 429 with "+name+", and a game with a PS3 disc", func(t *testing.T) {
		var busy atomic.Bool

		busy.Store(true)

		tgdb := &thegamesdb.Provider{API: apiclient.New(busyTheGamesDB(t, &busy, remaining, refresh).URL)}

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		games, details := sqlite.NewGameRepository(db), sqlite.NewDetailsStore(db)
		svc := newServices(db, games, assets, echo{}, []providers.Provider{tgdb, otherDetails{}, thegamesdb.NewDetails(tgdb)}, media.WithDetailsStore(details))
		_, err = svc.Registry.ConfigureProvider(ctx, thegamesdb.ID, true, schema.Settings{"api_key": "k"})
		require.NoError(t, err)

		g := saveGame(t, ctx, games, "Halo 3", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		})

		t.Run("WHEN the PS3 edition's cover is asked", func(t *testing.T) {
			_, err := svc.Covers.EditionCover(ctx, g.ID(), "PS3")

			t.Run("THEN it has no cover this time, and says why", func(t *testing.T) {
				require.ErrorIs(t, err, media.ErrNoCover)
				assert.ErrorIs(t, err, cause)

				t.Run("AND no miss is remembered", func(t *testing.T) {
					_, marked := assets.CoverMissingSince(g.ID(), "PS3")
					assert.False(t, marked)
				})
			})
		})

		t.Run("WHEN the game sheet is asked", func(t *testing.T) {
			d, warnings, err := svc.Details.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it shows what the other provider knows, without TheGamesDB's provider-level problem", func(t *testing.T) {
				assert.Equal(t, []string{"Shooter"}, d.Genres)
				assert.Empty(t, warnings)
			})

			t.Run("AND TheGamesDB's status says why it has nothing", func(t *testing.T) {
				st := svc.Registry.Statuses()
				require.Len(t, st, 1)
				assert.Equal(t, kind, st[0].Kind)
				assert.Equal(t, message, st[0].Message)
			})

			t.Run("AND the sheet is not cached", func(t *testing.T) {
				_, cached, err := details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				assert.False(t, cached)
			})
		})
	})
}
