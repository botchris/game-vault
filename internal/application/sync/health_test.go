package sync_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/health"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// flaky is a browser-session source whose scans and keep-alives answer what the test sets.
type flaky struct {
	fetchErr error
	aliveErr error
}

func (f *flaky) Descriptor() source.TypeDescriptor {
	return source.TypeDescriptor{
		Type: "flaky",
		Name: "Flaky store",
	}
}

func (f *flaky) Fetch(context.Context, schema.Settings) ([]game.ImportedCopy, []string, error) {
	return nil, nil, f.fetchErr
}

func (f *flaky) Test(context.Context, schema.Settings) error { return nil }

func (f *flaky) KeepAliveInterval() time.Duration { return time.Hour }

func (f *flaky) KeepAlive(context.Context, schema.Settings) error { return f.aliveErr }

func TestService_healthIssues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	setup := func(t *testing.T) (*sync.Service, *flaky, source.ID) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		p := &flaky{}
		svc := sync.NewService(sqlite.NewSourceRepository(db), sqlite.NewGameRepository(db), db, catalog.NewService(sqlite.NewGameRepository(db), db),
			[]sync.Provider{p}, sync.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

		v, err := svc.Create(ctx, "flaky", source.Config{
			Name:    "My store",
			Enabled: true,
		})
		require.NoError(t, err)

		return svc, p, v.ID()
	}

	t.Run("GIVEN a source whose last scan failed", func(t *testing.T) {
		svc, p, id := setup(t)
		p.fetchErr = errors.New("the store answered HTTP 500: try again later")
		_, _ = svc.Sync(ctx, id)

		t.Run("WHEN the health issues are asked", func(t *testing.T) {
			issues := svc.Issues(ctx)

			t.Run("THEN it is a warning with the error, pointing to the source", func(t *testing.T) {
				require.Len(t, issues, 1)
				assert.Equal(t, health.LevelWarning, issues[0].Level)
				assert.Equal(t, "source_sync_failed", issues[0].Reason)
				assert.Equal(t, health.KindSource, issues[0].Kind)
				assert.Equal(t, "My store", issues[0].Source)
				assert.Contains(t, issues[0].Message, "HTTP 500")
				assert.Equal(t, health.TargetSources, issues[0].Target)
				assert.Equal(t, string(id), issues[0].TargetID)
				assert.False(t, issues[0].Since.IsZero())
			})
		})

		t.Run("WHEN it fails again", func(t *testing.T) {
			first := svc.Issues(ctx)[0].Since

			time.Sleep(10 * time.Millisecond)

			_, _ = svc.Sync(ctx, id)

			t.Run("THEN the issue keeps the time of the first failure", func(t *testing.T) {
				issues := svc.Issues(ctx)
				require.Len(t, issues, 1)
				assert.True(t, issues[0].Since.Equal(first), "since %v, want %v", issues[0].Since, first)
			})
		})

		t.Run("WHEN the next scan works", func(t *testing.T) {
			p.fetchErr = nil
			_, err := svc.Sync(ctx, id)
			require.NoError(t, err)

			t.Run("THEN the issue is gone", func(t *testing.T) {
				assert.Empty(t, svc.Issues(ctx))
			})
		})
	})

	t.Run("GIVEN a source the store signed out", func(t *testing.T) {
		svc, p, id := setup(t)
		p.fetchErr = source.SignedOut("the store did not accept the cookies: sign in again and paste them")
		_, _ = svc.Sync(ctx, id)

		t.Run("WHEN the health issues are asked", func(t *testing.T) {
			issues := svc.Issues(ctx)

			t.Run("THEN it is an error that says to sign in again", func(t *testing.T) {
				require.Len(t, issues, 1)
				assert.Equal(t, health.LevelError, issues[0].Level)
				assert.Equal(t, "source_signed_out", issues[0].Reason)
				assert.Contains(t, issues[0].Message, "sign in again in Sources")
				assert.Contains(t, issues[0].Message, "paste them")
			})
		})

		t.Run("WHEN the source is disabled", func(t *testing.T) {
			_, err := svc.Update(ctx, id, source.Config{
				Name:    "My store",
				Enabled: false,
			})
			require.NoError(t, err)

			t.Run("THEN it needs no attention", func(t *testing.T) {
				assert.Empty(t, svc.Issues(ctx))
			})
		})
	})

	t.Run("GIVEN a browser session whose keep-alive fails", func(t *testing.T) {
		svc, p, id := setup(t)
		p.aliveErr = errors.New("the store did not answer: try again later")

		svc.KeepAliveNow(ctx, id)

		t.Run("WHEN the health issues are asked", func(t *testing.T) {
			issues := svc.Issues(ctx)

			t.Run("THEN the failed renewal is a warning", func(t *testing.T) {
				require.Len(t, issues, 1)
				assert.Equal(t, health.LevelWarning, issues[0].Level)
				assert.Equal(t, "source_keepalive_failed", issues[0].Reason)
				assert.Contains(t, issues[0].Message, "keep")
				assert.Contains(t, issues[0].Message, "did not answer")
			})
		})

		t.Run("WHEN it fails because the session is gone", func(t *testing.T) {
			first := svc.Issues(ctx)[0].Since
			p.aliveErr = source.SignedOut("the session expired: sign in again")

			svc.KeepAliveNow(ctx, id)

			t.Run("THEN it is an error, still dated from the first failure", func(t *testing.T) {
				issues := svc.Issues(ctx)
				require.Len(t, issues, 1)
				assert.Equal(t, health.LevelError, issues[0].Level)
				assert.True(t, issues[0].Since.Equal(first))
			})
		})

		t.Run("WHEN the next renewal works", func(t *testing.T) {
			p.aliveErr = nil

			svc.KeepAliveNow(ctx, id)

			t.Run("THEN the issue is gone", func(t *testing.T) {
				assert.Empty(t, svc.Issues(ctx))
			})
		})
	})
}

func TestService_healthIssuesClearedByTheUser(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	setup := func(t *testing.T) (*sync.Service, source.ID) {
		t.Helper()

		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		p := &flaky{aliveErr: errors.New("the store did not answer")}
		svc := sync.NewService(sqlite.NewSourceRepository(db), sqlite.NewGameRepository(db), db, catalog.NewService(sqlite.NewGameRepository(db), db),
			[]sync.Provider{p}, sync.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

		v, err := svc.Create(ctx, "flaky", source.Config{
			Name:    "My store",
			Enabled: true,
		})
		require.NoError(t, err)

		svc.KeepAliveNow(ctx, v.ID())
		require.Len(t, svc.Issues(ctx), 1)

		return svc, v.ID()
	}

	t.Run("GIVEN a failed keep-alive, WHEN the source's settings are saved", func(t *testing.T) {
		svc, id := setup(t)
		_, err := svc.Update(ctx, id, source.Config{
			Name:    "My store",
			Enabled: true,
		})
		require.NoError(t, err)

		t.Run("THEN the failure is forgotten: the new settings get their chance", func(t *testing.T) {
			assert.Empty(t, svc.Issues(ctx))
		})
	})

	t.Run("GIVEN a failed keep-alive, WHEN a scan works", func(t *testing.T) {
		svc, id := setup(t)
		_, err := svc.Sync(ctx, id)
		require.NoError(t, err)

		t.Run("THEN the session works after all", func(t *testing.T) {
			assert.Empty(t, svc.Issues(ctx))
		})
	})

	t.Run("GIVEN a failed keep-alive, WHEN the source is deleted", func(t *testing.T) {
		svc, id := setup(t)
		require.NoError(t, svc.Delete(ctx, id))

		t.Run("THEN nothing is reported about it", func(t *testing.T) {
			assert.Empty(t, svc.Issues(ctx))
		})
	})
}

// failingSources is a source repository whose List fails, like a locked database.
type failingSources struct{ source.Repository }

func (failingSources) List(context.Context) ([]*source.Source, error) {
	return nil, errors.New("database is locked")
}

func TestService_healthIssuesWhenTheSourcesCannotBeRead(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	svc := sync.NewService(failingSources{sqlite.NewSourceRepository(db)}, sqlite.NewGameRepository(db), db, catalog.NewService(sqlite.NewGameRepository(db), db),
		[]sync.Provider{&flaky{}}, sync.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	t.Run("GIVEN sources that cannot be read, WHEN the health is asked", func(t *testing.T) {
		issues := svc.Issues(ctx)

		t.Run("THEN it says so rather than reporting all is well", func(t *testing.T) {
			require.Len(t, issues, 1)
			assert.Equal(t, health.LevelWarning, issues[0].Level)
			assert.Equal(t, "source_list_failed", issues[0].Reason)
			assert.Contains(t, issues[0].Message, "database is locked")
		})
	})
}
