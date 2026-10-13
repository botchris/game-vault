package media_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/gamedata"
	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/health"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// allowanceUsedUp is a quota error that knows when the allowance renews, like TheGamesDB's.
type allowanceUsedUp struct{ until time.Time }

func (e allowanceUsedUp) Error() string        { return "the allowance is used up until it renews" }
func (e allowanceUsedUp) Unwrap() error        { return provider.ErrQuotaExhausted }
func (e allowanceUsedUp) ResumesAt() time.Time { return e.until }

// switchArt applies to every edition and answers with err, or one cover when err is nil.
type switchArt struct {
	mu  sync.Mutex
	err error
}

func (*switchArt) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               "switch",
		Kind:             provider.KindCover,
		Name:             "Switch art",
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (*switchArt) Test(context.Context, schema.Settings) error { return nil }

func (*switchArt) Applies(media.GameQuery) bool { return true }

func (a *switchArt) set(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.err = err
}

func (a *switchArt) Covers(context.Context, media.GameQuery, schema.Settings) ([]media.CoverCandidate, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.err != nil {
		return nil, a.err
	}

	return []media.CoverCandidate{{
		URL:      "https://img.test/switch",
		Provider: "switch",
	}}, nil
}

func TestProviderStatus_covers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a cover provider whose allowance is used up for 27 days", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
		require.NoError(t, err)

		now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
		clock := func() time.Time { return now }
		art := &switchArt{}
		games := sqlite.NewGameRepository(db)
		svc := media.NewService(games, sqlite.NewProviderRepository(db), assets, nil, nil, echo{}, clock,
			slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{Covers: []media.CoverProvider{art}})

		renews := now.Add(27 * 24 * time.Hour)
		art.set(allowanceUsedUp{until: renews})

		g := saveGame(t, ctx, games, "Console Quest", nil, game.CopyDetails{
			Kind:     game.KindPhysical,
			Platform: "PS4",
		})

		t.Run("WHEN a cover is looked up", func(t *testing.T) {
			_, err := svc.EditionCover(ctx, g.ID(), "PS4")
			require.ErrorIs(t, err, media.ErrNoCover)

			t.Run("THEN the provider's status says its allowance is used up until it renews", func(t *testing.T) {
				st := svc.ProviderStatuses()
				require.Len(t, st, 1)
				assert.Equal(t, provider.ID("switch"), st[0].Provider)
				assert.Equal(t, "Switch art", st[0].Name)
				assert.Equal(t, media.StatusQuota, st[0].Kind)
				assert.Equal(t, renews, st[0].Until)
				assert.Equal(t, now, st[0].Since)
				assert.Contains(t, st[0].Message, "allowance")
			})
		})

		t.Run("WHEN the cover picker lists candidates", func(t *testing.T) {
			_, warnings, _, err := svc.CoverCandidates(ctx, g.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN the provider-level problem is not a warning on the game", func(t *testing.T) {
				assert.Empty(t, warnings)
			})
		})

		t.Run("WHEN the provider fails for this game only", func(t *testing.T) {
			art.set(errors.New("server error 500 for this game: try again later"))

			_, warnings, _, err := svc.CoverCandidates(ctx, g.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN that failure is still shown", func(t *testing.T) {
				assert.Equal(t, []string{"Switch art: server error 500 for this game: try again later"}, warnings)
			})
		})

		t.Run("WHEN the provider answers again", func(t *testing.T) {
			art.set(nil)

			_, err := svc.EditionCover(ctx, g.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN its status is cleared", func(t *testing.T) {
				assert.Empty(t, svc.ProviderStatuses())
			})
		})

		t.Run("WHEN it asks to slow down and the time it was given passes", func(t *testing.T) {
			art.set(provider.ErrRateLimited)

			_, _, _, err := svc.CoverCandidates(ctx, g.ID(), "PS4")
			require.NoError(t, err)

			busy := svc.ProviderStatuses()
			now = now.Add(time.Hour)

			t.Run("THEN it was listed as busy, and is no longer", func(t *testing.T) {
				require.Len(t, busy, 1)
				assert.Equal(t, media.StatusRateLimited, busy[0].Kind)
				assert.Empty(t, svc.ProviderStatuses())
			})
		})

		t.Run("WHEN it is down, or rejects its settings", func(t *testing.T) {
			kinds := map[error]media.StatusKind{
				provider.ErrUnavailable: media.StatusUnavailable,
				provider.ErrBadSettings: media.StatusSettings,
			}

			for cause, want := range kinds {
				art.set(cause)

				_, warnings, _, err := svc.CoverCandidates(ctx, g.ID(), "PS4")
				require.NoError(t, err)

				st := svc.ProviderStatuses()

				t.Run("THEN the status says so, and the game shows no warning: "+string(want), func(t *testing.T) {
					require.Len(t, st, 1)
					assert.Equal(t, want, st[0].Kind)
					assert.Empty(t, warnings)
				})
			}
		})
	})
}

func TestProviderStatus_details(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	disc := game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
	}

	t.Run("GIVEN a sheet whose quota-limited provider asks to slow down", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, nil)
		env.quota.limited.Store(true)
		g := saveGame(t, ctx, env.games, "Disc Quest", nil, disc)

		t.Run("WHEN the sheet is asked", func(t *testing.T) {
			_, warnings, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the sheet shows no warning, the status records it, and the sheet is not cached", func(t *testing.T) {
				assert.Empty(t, warnings)

				st := env.svc.ProviderStatuses()
				require.Len(t, st, 1)
				assert.Equal(t, provider.ID("quota-details"), st[0].Provider)

				_, cached, err := env.details.Get(ctx, g.ID(), "en")
				require.NoError(t, err)
				assert.False(t, cached)
			})
		})
	})

	t.Run("GIVEN a sheet whose provider fails with a server error", func(t *testing.T) {
		env := newDetailsEnv(t, ctx, time.Now, nil)
		env.quota.failing.Store(true)
		g := saveGame(t, ctx, env.games, "Disc Quest", nil, disc)

		t.Run("WHEN the sheet is asked", func(t *testing.T) {
			_, warnings, err := env.svc.GameDetails(ctx, g.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the failure is shown, and no provider status is set", func(t *testing.T) {
				assert.Len(t, warnings, 1)
				assert.Empty(t, env.svc.ProviderStatuses())
			})
		})
	})
}

// serviceProvider is a cover or details provider of a service (settings group) whose answer the
// test sets: err, or a cover / a sheet when nil.
type serviceProvider struct {
	id    provider.ID
	name  string
	group string
	kind  provider.Kind

	mu  sync.Mutex
	err error
}

func (p *serviceProvider) Descriptor() provider.Descriptor {
	return provider.Descriptor{
		ID:               p.id,
		Kind:             p.kind,
		Name:             p.name,
		SettingsGroup:    p.group,
		EnabledByDefault: true,
		DefaultOrder:     10,
	}
}

func (p *serviceProvider) set(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.err = err
}

func (p *serviceProvider) answer() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.err
}

func (p *serviceProvider) Test(context.Context, schema.Settings) error { return p.answer() }

func (*serviceProvider) Applies(media.GameQuery) bool { return true }

func (p *serviceProvider) Covers(context.Context, media.GameQuery, schema.Settings) ([]media.CoverCandidate, error) {
	if err := p.answer(); err != nil {
		return nil, err
	}

	return []media.CoverCandidate{{
		URL:      "https://img.test/" + string(p.id),
		Provider: p.id,
	}}, nil
}

func (p *serviceProvider) Details(context.Context, media.GameQuery, string, schema.Settings) (*media.GameDetails, error) {
	if err := p.answer(); err != nil {
		return nil, err
	}

	return &media.GameDetails{Summary: "A sheet."}, nil
}

type serviceEnv struct {
	svc     *media.Service
	games   game.Repository
	details media.DetailsStore
	covers  *serviceProvider
	sheets  *serviceProvider
	game    *game.Game
}

// newServiceEnv builds a media service with one service's covers and details sharing a settings
// group, and a PS4 disc.
func newServiceEnv(t *testing.T, ctx context.Context) serviceEnv {
	t.Helper()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	assets, err := gamedata.Open(filepath.Join(t.TempDir(), "game-data"))
	require.NoError(t, err)

	env := serviceEnv{
		games:   sqlite.NewGameRepository(db),
		details: sqlite.NewDetailsStore(db),
		covers: &serviceProvider{
			id:    "db",
			name:  "Database",
			group: "db",
			kind:  provider.KindCover,
		},
		sheets: &serviceProvider{
			id:    "db-details",
			name:  "Database",
			group: "db",
			kind:  provider.KindMetadata,
		},
	}
	env.svc = media.NewService(env.games, sqlite.NewProviderRepository(db), assets, nil, env.details, echo{}, time.Now,
		slog.New(slog.NewTextHandler(io.Discard, nil)), media.Providers{
			Covers:   []media.CoverProvider{env.covers},
			Metadata: []media.MetadataProvider{env.sheets},
		})
	env.game = saveGame(t, ctx, env.games, "Disc Quest", nil, game.CopyDetails{
		Kind:     game.KindPhysical,
		Platform: "PS4",
	})

	return env
}

func TestProviderStatus_service(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	rejected := provider.BadSettings("the key was rejected: check it in the provider settings")

	t.Run("GIVEN a service whose details reject the key", func(t *testing.T) {
		env := newServiceEnv(t, ctx)
		env.sheets.set(rejected)

		t.Run("WHEN a sheet is asked", func(t *testing.T) {
			_, warnings, err := env.svc.GameDetails(ctx, env.game.ID(), "en", false)
			require.NoError(t, err)

			t.Run("THEN the sheet has no warning and the service's status says its settings are rejected", func(t *testing.T) {
				assert.Empty(t, warnings)

				st := env.svc.ProviderStatuses()
				require.Len(t, st, 1)
				assert.Equal(t, media.StatusSettings, st[0].Kind)
				assert.Equal(t, provider.ID("db"), st[0].Provider)
				assert.Equal(t, []provider.ID{"db", "db-details"}, st[0].Providers)
			})
		})

		t.Run("WHEN its covers, without a key either, answer the same", func(t *testing.T) {
			env.covers.set(rejected)
			_, _, unavailable, err := env.svc.CoverCandidates(ctx, env.game.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN the status stays, and the picker is told providers are unavailable", func(t *testing.T) {
				assert.Len(t, env.svc.ProviderStatuses(), 1)
				assert.True(t, unavailable)
			})
		})

		t.Run("WHEN its covers really answer with the same settings", func(t *testing.T) {
			env.covers.set(nil)
			_, _, unavailable, err := env.svc.CoverCandidates(ctx, env.game.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN the settings work after all: the status is cleared", func(t *testing.T) {
				assert.Empty(t, env.svc.ProviderStatuses())
				assert.False(t, unavailable)
			})
		})
	})

	t.Run("GIVEN a service in trouble", func(t *testing.T) {
		env := newServiceEnv(t, ctx)
		env.covers.set(provider.ErrUnavailable)

		_, _, _, err := env.svc.CoverCandidates(ctx, env.game.ID(), "PS4")
		require.NoError(t, err)
		require.Len(t, env.svc.ProviderStatuses(), 1)

		t.Run("WHEN its settings are saved", func(t *testing.T) {
			_, err := env.svc.ConfigureProvider(ctx, "db", true, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN the status is cleared until the next answer tells", func(t *testing.T) {
				assert.Empty(t, env.svc.ProviderStatuses())
			})
		})

		t.Run("WHEN its connection is tested and fails, then works", func(t *testing.T) {
			env.covers.set(rejected)
			require.Error(t, env.svc.TestProvider(ctx, "db", schema.Settings{}))
			failed := env.svc.ProviderStatuses()

			env.covers.set(nil)
			require.NoError(t, env.svc.TestProvider(ctx, "db", schema.Settings{}))

			t.Run("THEN the test sets the status, and a successful one clears it", func(t *testing.T) {
				require.Len(t, failed, 1)
				assert.Equal(t, media.StatusSettings, failed[0].Kind)
				assert.Empty(t, env.svc.ProviderStatuses())
			})
		})
	})

	t.Run("GIVEN two services with the same name in trouble", func(t *testing.T) {
		env := newServiceEnv(t, ctx)
		env.covers.group, env.sheets.group = "", "" // like Steam's covers and details

		env.covers.set(provider.ErrRateLimited)
		env.sheets.set(provider.ErrRateLimited)

		_, _, _, err := env.svc.CoverCandidates(ctx, env.game.ID(), "PS4")
		require.NoError(t, err)
		_, _, err = env.svc.GameDetails(ctx, env.game.ID(), "en", false)
		require.NoError(t, err)

		t.Run("WHEN their statuses are listed", func(t *testing.T) {
			st := env.svc.ProviderStatuses()

			t.Run("THEN they come by name, then by id, always in the same order", func(t *testing.T) {
				require.Len(t, st, 2)
				assert.Equal(t, provider.ID("db"), st[0].Provider)
				assert.Equal(t, provider.ID("db-details"), st[1].Provider)
			})
		})
	})

	t.Run("GIVEN answers and listings at the same time", func(t *testing.T) {
		env := newServiceEnv(t, ctx)

		t.Run("WHEN they run concurrently", func(t *testing.T) {
			var wg sync.WaitGroup

			for i := range 20 {
				wg.Go(func() {
					if i%2 == 0 {
						_ = env.svc.TestProvider(ctx, "db", schema.Settings{})
						return
					}

					_ = env.svc.ProviderStatuses()
				})
			}

			wg.Wait()

			t.Run("THEN nothing breaks", func(t *testing.T) {
				assert.Empty(t, env.svc.ProviderStatuses())
			})
		})
	})
}

func TestProviderStatus_healthIssues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a service whose details reject the key and another whose allowance is used up", func(t *testing.T) {
		env := newServiceEnv(t, ctx)
		env.covers.group, env.sheets.group = "", "" // two services, each its own status
		env.sheets.set(provider.BadSettings("the key was rejected: check it in the provider settings"))
		_, _, err := env.svc.GameDetails(ctx, env.game.ID(), "en", false)
		require.NoError(t, err)

		env.covers.set(provider.ErrQuotaExhausted)
		_, _, _, err = env.svc.CoverCandidates(ctx, env.game.ID(), "PS4")
		require.NoError(t, err)

		t.Run("WHEN the health issues are asked", func(t *testing.T) {
			issues := env.svc.Issues(ctx)

			t.Run("THEN rejected settings are an error, a used-up allowance a warning, both pointing to Providers", func(t *testing.T) {
				require.Len(t, issues, 2)

				byKey := map[string]health.Issue{}
				for _, i := range issues {
					byKey[i.Key] = i
				}

				assert.Equal(t, health.LevelError, byKey["provider:db-details"].Level)
				assert.Equal(t, "provider_settings", byKey["provider:db-details"].Reason)
				assert.Equal(t, health.LevelWarning, byKey["provider:db"].Level)
				assert.Equal(t, "provider_quota", byKey["provider:db"].Reason)
				assert.NotEmpty(t, byKey["provider:db"].Params["until"], "when the allowance renews, for the translated reason")

				for _, i := range issues {
					assert.Equal(t, health.KindProvider, i.Kind)
					assert.Equal(t, health.TargetProviders, i.Target)
					assert.Equal(t, "Database", i.Source)
					assert.NotEmpty(t, i.Message)
					assert.False(t, i.Since.IsZero())
				}
			})
		})
	})
}
