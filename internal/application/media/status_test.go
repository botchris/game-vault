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

func (*switchArt) Applies(media.CoverQuery) bool { return true }

func (a *switchArt) set(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.err = err
}

func (a *switchArt) Covers(context.Context, media.CoverQuery, schema.Settings) ([]media.CoverCandidate, error) {
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
			_, warnings, err := svc.CoverCandidates(ctx, g.ID(), "PS4")
			require.NoError(t, err)

			t.Run("THEN the provider-level problem is not a warning on the game", func(t *testing.T) {
				assert.Empty(t, warnings)
			})
		})

		t.Run("WHEN the provider fails for this game only", func(t *testing.T) {
			art.set(errors.New("server error 500 for this game: try again later"))

			_, warnings, err := svc.CoverCandidates(ctx, g.ID(), "PS4")
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

			_, _, err := svc.CoverCandidates(ctx, g.ID(), "PS4")
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

				_, warnings, err := svc.CoverCandidates(ctx, g.ID(), "PS4")
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
