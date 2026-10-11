package media_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
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

func (otherDetails) Applies(media.CoverQuery) bool { return true }

func (otherDetails) Details(context.Context, media.CoverQuery, string, schema.Settings) (*media.GameDetails, error) {
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
		svc := media.NewService(games, sqlite.NewProviderRepository(db), assets, nil, details, echo{}, time.Now,
			slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{
				Covers:   []media.CoverProvider{tgdb},
				Metadata: []media.MetadataProvider{otherDetails{}, thegamesdb.NewDetails(tgdb)},
			})
		_, err = svc.ConfigureProvider(ctx, thegamesdb.ID, true, schema.Settings{"api_key": "k"})
		require.NoError(t, err)

		g := saveGame(t, ctx, games, "Halo 3", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		})

		t.Run("WHEN the PS3 edition's cover is asked", func(t *testing.T) {
			_, err := svc.EditionCover(ctx, g.ID(), "PS3")

			t.Run("THEN it has no cover this time, and no miss is remembered", func(t *testing.T) {
				require.ErrorIs(t, err, media.ErrNoCover)
				assert.ErrorIs(t, err, provider.ErrRateLimited)

				_, marked := assets.CoverMissingSince(g.ID(), "PS3")
				assert.False(t, marked)
			})
		})

		t.Run("WHEN the game sheet is asked", func(t *testing.T) {
			d, warnings, err := svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it shows what the other provider knows and says TheGamesDB is busy for now", func(t *testing.T) {
				assert.Equal(t, []string{"Shooter"}, d.Genres)
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], "too many requests")
				assert.NotContains(t, warnings[0], "TheGamesDB: TheGamesDB")
			})

			t.Run("AND the sheet is not cached", func(t *testing.T) {
				_, cached, err := details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				assert.False(t, cached)
			})
		})

		t.Run("WHEN TheGamesDB answers again and both are asked once more", func(t *testing.T) {
			busy.Store(false)

			img, coverErr := svc.EditionCover(ctx, g.ID(), "PS3")
			d, warnings, err := svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the edition gets its box and the sheet TheGamesDB's summary", func(t *testing.T) {
				assert.Equal(t, "https://cdn.test/large/front/1.jpg", cover(t, img, coverErr))
				assert.Empty(t, warnings)
				assert.Equal(t, "Finish the fight.", d.Summary)
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
		warning   string
	}{
		{
			"this month's allowance used up", 0, 1036800, provider.ErrQuotaExhausted,
			"TheGamesDB: the API key has used this month's allowance: covers and sheets resume when it renews in 12 days",
		},
		{
			"an unknown key", 0, 0, provider.ErrRateLimited,
			"TheGamesDB: the API key was not recognized: check it in the provider settings",
		},
	} {
		testUnavailableTheGamesDB(t, ctx, quota.name, quota.remaining, quota.refresh, quota.cause, quota.warning)
	}
}

// testUnavailableTheGamesDB checks a TheGamesDB that answers HTTP 429 and whose API/Limit reports
// remaining and refresh: covers fail with cause, the sheet shows warning, and nothing is remembered
// from it.
func testUnavailableTheGamesDB(t *testing.T, ctx context.Context, name string, remaining, refresh int, cause error, warning string) {
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
		svc := media.NewService(games, sqlite.NewProviderRepository(db), assets, nil, details, echo{}, time.Now,
			slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{
				Covers:   []media.CoverProvider{tgdb},
				Metadata: []media.MetadataProvider{otherDetails{}, thegamesdb.NewDetails(tgdb)},
			})
		_, err = svc.ConfigureProvider(ctx, thegamesdb.ID, true, schema.Settings{"api_key": "k"})
		require.NoError(t, err)

		g := saveGame(t, ctx, games, "Halo 3", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS3",
		})

		t.Run("WHEN the PS3 edition's cover is asked", func(t *testing.T) {
			_, err := svc.EditionCover(ctx, g.ID(), "PS3")

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
			d, warnings, err := svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN it shows what the other provider knows and says why TheGamesDB has nothing", func(t *testing.T) {
				assert.Equal(t, []string{"Shooter"}, d.Genres)
				require.Len(t, warnings, 1)
				assert.Equal(t, warning, warnings[0])
				assert.NotContains(t, warnings[0], "busy")
				assert.NotContains(t, warnings[0], "too many requests")
			})

			t.Run("AND the sheet is not cached", func(t *testing.T) {
				_, cached, err := details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				assert.False(t, cached)
			})
		})
	})
}
