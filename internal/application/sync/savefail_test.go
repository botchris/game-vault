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
	"gamevault/internal/application/sync"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/source"
)

// errLocked is a database failure.
var errLocked = errors.New("database is locked")

// lockingSources is a source repository whose Save fails once locked is set.
type lockingSources struct {
	source.Repository

	locked bool
}

func (l *lockingSources) Save(ctx context.Context, s *source.Source) error {
	if l.locked {
		return errLocked
	}

	return l.Repository.Save(ctx, s)
}

func TestSync_aFailedSaveIsInternal(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a store that signed the source out, and a database that refuses the report", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		sources := &lockingSources{Repository: sqlite.NewSourceRepository(db)}
		p := &flaky{fetchErr: source.SignedOut("sign in again")}
		svc := sync.NewService(sources, sqlite.NewGameRepository(db), db, catalog.NewService(sqlite.NewGameRepository(db), db),
			[]sync.Provider{p}, sync.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

		v, err := svc.Create(ctx, "flaky", source.Config{Name: "Flaky"})
		require.NoError(t, err)

		sources.locked = true

		t.Run("WHEN it is scanned", func(t *testing.T) {
			_, err := svc.Sync(ctx, v.ID())

			t.Run("THEN both errors are kept, and the lost report makes it a failure of Game Vault", func(t *testing.T) {
				require.ErrorIs(t, err, errLocked)
				require.ErrorIs(t, err, source.ErrSignedOut)
				assert.Equal(t, errkind.Unknown, errkind.Of(err), "the store's sign-out must not hide the database failure")
			})
		})
	})
}
