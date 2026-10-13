package backupdir_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/backupdir"
	"gamevault/internal/application/system"
	"gamevault/internal/domain/game"
)

var _ system.BackupStore = (*backupdir.Dir)(nil)

func photoID(n int) game.PhotoID { return game.PhotoID(fmt.Sprintf("%064x", n)) }

// write creates a file in dir with the given modification time.
func write(t *testing.T, dir, name, content string, at time.Time) {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	require.NoError(t, os.Chtimes(path, at, at))
}

func TestDir(t *testing.T) {
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN a directory that does not exist yet", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "backups")
		d := backupdir.New(path)

		t.Run("THEN there are no backups", func(t *testing.T) {
			list, err := d.List()
			require.NoError(t, err)
			assert.Empty(t, list)
		})

		t.Run("WHEN a backup is created", func(t *testing.T) {
			file, err := d.Create("gamevault-20261012-120000.db")
			require.NoError(t, err)

			t.Run("THEN the directory exists and the copy goes in it, under the backup's name", func(t *testing.T) {
				assert.DirExists(t, path)
				assert.Equal(t, filepath.Join(path, "gamevault-20261012-120000.db"), file)
			})
		})
	})

	t.Run("GIVEN a backup with a photo list, a pre-migration copy without one, and other files", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "gamevault-20261012-120000.db", "database", at)
		write(t, dir, "pre-migration-0009.db", "db", at.Add(-time.Hour))
		write(t, dir, "notes.txt", "not a backup", at)
		require.NoError(t, os.Mkdir(filepath.Join(dir, "photos"), 0o755))

		d := backupdir.New(dir)
		require.NoError(t, d.WritePhotoList("gamevault-20261012-120000.db", []game.PhotoID{photoID(1), photoID(2)}))

		t.Run("WHEN they are listed", func(t *testing.T) {
			list, err := d.List()
			require.NoError(t, err)

			t.Run("THEN only the .db files are backups, dated by their files, with their photo counts", func(t *testing.T) {
				assert.ElementsMatch(t, []system.Backup{
					{
						Name:      "gamevault-20261012-120000.db",
						SizeBytes: int64(len("database")),
						CreatedAt: at,
						Photos:    2,
					},
					{
						Name:      "pre-migration-0009.db",
						SizeBytes: 2,
						CreatedAt: at.Add(-time.Hour),
					},
				}, normalized(list))
			})
		})

		t.Run("WHEN one is looked up by name", func(t *testing.T) {
			b, err := d.Stat("gamevault-20261012-120000.db")
			require.NoError(t, err)

			t.Run("THEN it is described as in the list", func(t *testing.T) {
				assert.Equal(t, 2, b.Photos)
				assert.Equal(t, int64(len("database")), b.SizeBytes)
			})
		})

		t.Run("THEN the photo list is one id per line next to the backup", func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, "gamevault-20261012-120000.photos"))
			require.NoError(t, err)
			assert.Equal(t, string(photoID(1))+"\n"+string(photoID(2))+"\n", string(data))

			ids, err := d.PhotoList("gamevault-20261012-120000.db")
			require.NoError(t, err)
			assert.Equal(t, []game.PhotoID{photoID(1), photoID(2)}, ids)
		})

		t.Run("AND a backup without a list needs no photos", func(t *testing.T) {
			ids, err := d.PhotoList("pre-migration-0009.db")
			require.NoError(t, err)
			assert.Empty(t, ids)
		})
	})

	t.Run("GIVEN a backup with its photo list and one without", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "gamevault-20261012-120000.db", "db", at)
		write(t, dir, "pre-migration-0009.db", "db", at)

		d := backupdir.New(dir)
		require.NoError(t, d.WritePhotoList("gamevault-20261012-120000.db", []game.PhotoID{photoID(1)}))

		t.Run("WHEN both are removed", func(t *testing.T) {
			require.NoError(t, d.Remove("gamevault-20261012-120000.db"))
			require.NoError(t, d.Remove("pre-migration-0009.db"))

			t.Run("THEN the backups and the list are gone", func(t *testing.T) {
				entries, err := os.ReadDir(dir)
				require.NoError(t, err)
				assert.Empty(t, entries)
			})
		})
	})

	t.Run("GIVEN names that are not file names THEN they are refused", func(t *testing.T) {
		d := backupdir.New(t.TempDir())

		for _, name := range []string{"", ".", "..", "../gamevault.db", "sub/gamevault.db"} {
			_, err := d.Create(name)
			assert.Error(t, err, "%q", name)
			assert.Error(t, d.Remove(name), "%q", name)
		}
	})
}

// normalized drops the monotonic and location parts of the dates, which the file system adds.
func normalized(list []system.Backup) []system.Backup {
	for i := range list {
		list[i].CreatedAt = list[i].CreatedAt.UTC()
	}

	return list
}
