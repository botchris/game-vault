package sync_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/sqlite"
	"gamevault/internal/application/catalog"
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

func TestSync_invalidSourceSystem(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a source that sends a system that is not a valid name", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		var logged strings.Builder

		bad := item("ls:journey", "Journey")
		bad.System = "--"
		p := &listing{items: []game.ImportedCopy{bad}}
		svc := sync.NewService(sqlite.NewSourceRepository(db), sqlite.NewGameRepository(db), db, catalog.NewService(sqlite.NewGameRepository(db), db),
			[]sync.Provider{p}, sync.WithLogger(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))))

		v, err := svc.Create(ctx, "ls", source.Config{
			Name:    "My listing",
			Enabled: true,
		})
		require.NoError(t, err)

		t.Run("WHEN it is scanned", func(t *testing.T) {
			first, err := svc.Sync(ctx, v.ID())
			require.NoError(t, err)

			t.Run("THEN the scan warns about it", func(t *testing.T) {
				require.Len(t, first.LastSync().Warnings, 1)
				assert.Contains(t, first.LastSync().Warnings[0], "Journey")
				assert.Contains(t, logged.String(), "level=WARN")
			})
		})

		t.Run("WHEN it is scanned again with the same system", func(t *testing.T) {
			logged.Reset()

			again, err := svc.Sync(ctx, v.ID())
			require.NoError(t, err)

			t.Run("THEN the repeat is only a debug line, not a warning", func(t *testing.T) {
				assert.Empty(t, again.LastSync().Warnings)
				assert.NotContains(t, logged.String(), "level=WARN")
				assert.Contains(t, logged.String(), "level=DEBUG")
			})
		})
	})
}

// failingCommit is a transaction manager whose next transaction fails after running, as if the
// commit failed.
type failingCommit struct {
	tx interface {
		WithinTx(context.Context, func(context.Context) error) error
	}
	fail bool
}

func (f *failingCommit) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	if err := f.tx.WithinTx(ctx, fn); err != nil {
		return err
	}

	if f.fail {
		f.fail = false
		return errors.New("commit failed")
	}

	return nil
}

func TestSync_invalidSourceSystemAfterAFailedScan(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a source that sends an invalid system, and a scan whose commit fails", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		bad := item("ls:journey", "Journey")
		bad.System = "--"
		tx := &failingCommit{tx: db}
		svc := sync.NewService(sqlite.NewSourceRepository(db), sqlite.NewGameRepository(db), tx, catalog.NewService(sqlite.NewGameRepository(db), tx),
			[]sync.Provider{&listing{items: []game.ImportedCopy{bad}}}, sync.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

		v, err := svc.Create(ctx, "ls", source.Config{
			Name:    "My listing",
			Enabled: true,
		})
		require.NoError(t, err)

		tx.fail = true
		_, err = svc.Sync(ctx, v.ID())
		require.Error(t, err)

		t.Run("WHEN the next scan succeeds", func(t *testing.T) {
			again, err := svc.Sync(ctx, v.ID())
			require.NoError(t, err)

			t.Run("THEN it still warns: the failed scan did not count", func(t *testing.T) {
				require.Len(t, again.LastSync().Warnings, 1)
				assert.Contains(t, again.LastSync().Warnings[0], "Journey")
			})
		})
	})
}
