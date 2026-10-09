package system_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/system"
	"gamevault/internal/domain/game"
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

		svc := system.NewService(nil, fileBackup{}, nil, nil, func() time.Time { return now },
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

// photoGames is a game.Repository whose List returns games with the given photos on one copy.
type photoGames struct {
	game.Repository

	ids []game.PhotoID
}

func (r *photoGames) List(context.Context) ([]*game.Game, error) {
	photos := make([]game.Photo, 0, len(r.ids))
	for _, id := range r.ids {
		photos = append(photos, game.Photo{ID: id})
	}

	return []*game.Game{game.Rehydrate("g1", game.Info{Title: "Halo 3"}, []game.Copy{{
		ID: "c1",
		CopyDetails: game.CopyDetails{
			Kind: game.KindPhysical,
		},
		Photos: photos,
	}}, time.Now(), time.Now())}, nil
}

// fakeArchive records what the backups put in and keep in the shared photo store.
type fakeArchive struct {
	stored map[game.PhotoID]bool
	adds   int
}

func (a *fakeArchive) Add(ids []game.PhotoID) error {
	for _, id := range ids {
		if !a.stored[id] {
			a.stored[id] = true
			a.adds++
		}
	}

	return nil
}

func (a *fakeArchive) Retain(keep map[game.PhotoID]bool) error {
	for id := range a.stored {
		if !keep[id] {
			delete(a.stored, id)
		}
	}

	return nil
}

func (a *fakeArchive) Size() (int64, error) { return int64(len(a.stored)) * 100, nil }

func photoID(n int) game.PhotoID { return game.PhotoID(fmt.Sprintf("%064x", n)) }

func TestBackups_photos(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a catalog with two photos and room for two backups", func(t *testing.T) {
		dir := t.TempDir()
		games := &photoGames{ids: []game.PhotoID{photoID(1), photoID(2)}}
		archive := &fakeArchive{stored: map[game.PhotoID]bool{}}
		clock := time.Now()
		svc := system.NewService(games, fileBackup{}, nil, archive, func() time.Time { clock = clock.Add(time.Second); return clock },
			slog.New(slog.NewTextHandler(io.Discard, nil)), system.Status{}, dir, 2)

		t.Run("WHEN a backup is made", func(t *testing.T) {
			b, err := svc.CreateBackup(ctx)
			require.NoError(t, err)

			t.Run("THEN its photo list sits next to it and the photos are in the shared store", func(t *testing.T) {
				list, err := os.ReadFile(filepath.Join(dir, strings.TrimSuffix(b.Name, ".db")+".photos"))
				require.NoError(t, err)
				assert.Equal(t, string(photoID(1))+"\n"+string(photoID(2))+"\n", string(list))
				assert.Len(t, archive.stored, 2)
				assert.Equal(t, 2, b.Photos)
			})
		})

		t.Run("WHEN a second backup is made with the same photos", func(t *testing.T) {
			_, err := svc.CreateBackup(ctx)
			require.NoError(t, err)

			t.Run("THEN nothing new is stored", func(t *testing.T) {
				assert.Equal(t, 2, archive.adds)
			})
		})

		t.Run("WHEN one photo is gone from the catalog and two more backups rotate the first two out", func(t *testing.T) {
			games.ids = []game.PhotoID{photoID(2)}

			for range 2 {
				_, err := svc.CreateBackup(ctx)
				require.NoError(t, err)
			}

			t.Run("THEN the old lists went with their backups, and the photo no backup lists is removed", func(t *testing.T) {
				lists, err := filepath.Glob(filepath.Join(dir, "*.photos"))
				require.NoError(t, err)
				assert.Len(t, lists, 2)
				assert.Equal(t, map[game.PhotoID]bool{photoID(2): true}, archive.stored)
			})

			t.Run("AND the list of backups says how many photos each covers, and the store's size", func(t *testing.T) {
				list, err := svc.ListBackups(ctx)
				require.NoError(t, err)
				require.Len(t, list, 2)
				assert.Equal(t, 1, list[0].Photos)

				size, err := svc.PhotoStoreSize(ctx)
				require.NoError(t, err)
				assert.Equal(t, int64(100), size)
			})
		})
	})
}

// racingBackup changes the catalog's photos while the database is being copied, like a user
// editing during a backup.
type racingBackup struct {
	games *photoGames
	next  []game.PhotoID
}

func (b racingBackup) BackupTo(_ context.Context, path string) error {
	b.games.ids = b.next

	return os.WriteFile(path, []byte("db"), 0o600)
}

// failingArchive cannot store photos.
type failingArchive struct{ fakeArchive }

func (failingArchive) Add([]game.PhotoID) error { return errors.New("disk full") }

func TestBackups_photosEdgeCases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("GIVEN photos that change while the database is being copied", func(t *testing.T) {
		dir := t.TempDir()
		games := &photoGames{ids: []game.PhotoID{photoID(1), photoID(2)}}
		archive := &fakeArchive{stored: map[game.PhotoID]bool{}}
		db := racingBackup{
			games: games,
			next:  []game.PhotoID{photoID(2), photoID(3)},
		}
		svc := system.NewService(games, db, nil, archive, time.Now, logger, system.Status{}, dir, 3)

		t.Run("WHEN a backup is made", func(t *testing.T) {
			b, err := svc.CreateBackup(ctx)
			require.NoError(t, err)

			t.Run("THEN it keeps every photo the copied database may reference, before and after", func(t *testing.T) {
				assert.Equal(t, 3, b.Photos)
				assert.Equal(t, map[game.PhotoID]bool{photoID(1): true, photoID(2): true, photoID(3): true}, archive.stored)
			})
		})
	})

	t.Run("GIVEN more backups than are kept and a photo store that fails", func(t *testing.T) {
		dir := t.TempDir()
		old := time.Now().Add(-48 * time.Hour)
		writeBackup(t, dir, "gamevault-20261001-120000.db", old)
		writeBackup(t, dir, "gamevault-20261002-120000.db", old.Add(time.Hour))

		games := &photoGames{ids: []game.PhotoID{photoID(1)}}
		svc := system.NewService(games, fileBackup{}, nil, &failingArchive{}, time.Now, logger, system.Status{}, dir, 2)

		t.Run("WHEN a backup is made", func(t *testing.T) {
			_, err := svc.CreateBackup(ctx)

			t.Run("THEN it reports the photos failed, keeps the database copy and still rotates", func(t *testing.T) {
				require.ErrorContains(t, err, "disk full")

				list, err := svc.ListBackups(ctx)
				require.NoError(t, err)
				assert.Len(t, list, 2)
				assert.NoFileExists(t, filepath.Join(dir, "gamevault-20261001-120000.db"))
			})
		})
	})
}
