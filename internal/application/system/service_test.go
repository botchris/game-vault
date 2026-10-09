package system_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/system"
)

// fileBackup writes an empty file where the database copy would go.
type fileBackup struct{}

func (fileBackup) BackupTo(_ context.Context, path string) error {
	return os.WriteFile(path, []byte("db"), 0o600)
}

// writeBackup creates a backup file with the given modification time.
func writeBackup(t *testing.T, dir, name string, at time.Time) {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("db"), 0o600))
	require.NoError(t, os.Chtimes(path, at, at))
}

func TestBackups_rotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// The files' dates come from the real clock, like the backups the service writes.
	now := time.Now()

	t.Run("GIVEN a pre-migration copy from an upgrade, older than the scheduled backups after it", func(t *testing.T) {
		dir := t.TempDir()
		writeBackup(t, dir, "pre-migration-0009.db", now.Add(-72*time.Hour))
		writeBackup(t, dir, "gamevault-20261018-120000.db", now.Add(-48*time.Hour))
		writeBackup(t, dir, "gamevault-20261019-120000.db", now.Add(-24*time.Hour))

		svc := system.NewService(nil, fileBackup{}, nil, func() time.Time { return now },
			slog.New(slog.NewTextHandler(io.Discard, nil)), system.Status{}, dir, 3)

		t.Run("WHEN the backups are listed", func(t *testing.T) {
			list, err := svc.ListBackups(ctx)
			require.NoError(t, err)

			t.Run("THEN they come newest first, by date, whatever their name", func(t *testing.T) {
				require.Len(t, list, 3)
				assert.Equal(t, "gamevault-20261019-120000.db", list[0].Name)
				assert.Equal(t, "pre-migration-0009.db", list[2].Name)
			})
		})

		t.Run("WHEN a new backup goes over the number kept", func(t *testing.T) {
			b, err := svc.CreateBackup(ctx)
			require.NoError(t, err)

			t.Run("THEN the oldest one goes, the pre-migration copy included, and the new one stays", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(dir, b.Name))
				assert.NoFileExists(t, filepath.Join(dir, "pre-migration-0009.db"))
			})
		})
	})
}
