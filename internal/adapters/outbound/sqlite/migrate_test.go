package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_backupBeforeMigrating(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a new database", func(t *testing.T) {
		dir := t.TempDir()
		backups := filepath.Join(dir, "backups")

		t.Run("WHEN it is opened", func(t *testing.T) {
			db, err := Open(ctx, filepath.Join(dir, "new.db"), backups)
			require.NoError(t, err)
			db.Close()

			t.Run("THEN nothing is backed up: there is nothing to lose", func(t *testing.T) {
				files, _ := filepath.Glob(filepath.Join(backups, "*.db"))
				assert.Empty(t, files)
			})
		})
	})

	t.Run("GIVEN a database migrated up to 0007", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "old.db")
		backups := filepath.Join(dir, "backups")

		db, err := openUpTo(ctx, path, "", "0007")
		require.NoError(t, err)
		db.Close()

		t.Run("WHEN it is opened with the pending migrations", func(t *testing.T) {
			db, err := Open(ctx, path, backups)
			require.NoError(t, err)
			db.Close()

			t.Run("THEN a copy named after the first pending migration was written first", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(backups, "pre-migration-0008.db"))
			})
		})

		t.Run("WHEN that copy is restored and opened again", func(t *testing.T) {
			restored := filepath.Join(dir, "restored.db")
			data, err := os.ReadFile(filepath.Join(backups, "pre-migration-0008.db"))
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(restored, data, 0o600))

			db, err := Open(ctx, restored, backups)

			t.Run("THEN it migrates, keeping the first copy and writing a timestamped one", func(t *testing.T) {
				require.NoError(t, err)
				db.Close()

				files, _ := filepath.Glob(filepath.Join(backups, "pre-migration-0008*.db"))
				assert.Len(t, files, 2)
			})
		})
	})
}
