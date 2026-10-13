package system_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/system"
	"gamevault/internal/domain/game"
)

// memBackups is an in-memory system.BackupStore. A backup exists once its database copy is written
// to the path Create returned (see copier), as on disk.
type memBackups struct {
	backups map[string]system.Backup
	lists   map[string][]game.PhotoID
}

func newBackups() *memBackups {
	return &memBackups{
		backups: map[string]system.Backup{},
		lists:   map[string][]game.PhotoID{},
	}
}

// put adds the backup called name, made at at.
func (m *memBackups) put(name string, at time.Time) {
	m.backups[name] = system.Backup{
		Name:      name,
		SizeBytes: 2,
		CreatedAt: at,
	}
}

func (m *memBackups) Create(name string) (string, error) { return name, nil }

func (m *memBackups) Stat(name string) (system.Backup, error) {
	b, ok := m.backups[name]
	if !ok {
		return system.Backup{}, fs.ErrNotExist
	}

	b.Photos = len(m.lists[name])

	return b, nil
}

func (m *memBackups) List() ([]system.Backup, error) {
	out := make([]system.Backup, 0, len(m.backups))

	for name := range m.backups {
		b, _ := m.Stat(name)
		out = append(out, b)
	}

	return out, nil
}

func (m *memBackups) Remove(name string) error {
	if _, ok := m.backups[name]; !ok {
		return fs.ErrNotExist
	}

	delete(m.backups, name)
	delete(m.lists, name)

	return nil
}

func (m *memBackups) WritePhotoList(name string, ids []game.PhotoID) error {
	m.lists[name] = slices.Clone(ids)
	return nil
}

func (m *memBackups) PhotoList(name string) ([]game.PhotoID, error) { return m.lists[name], nil }

// copier is the database: it writes its copy into the store, dated by the real clock.
type copier struct{ store *memBackups }

func (c copier) BackupTo(_ context.Context, path string) error {
	c.store.put(path, time.Now())
	return nil
}

func TestBackups_rotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	// The backups' dates come from the real clock, like the backups the service writes.
	now := time.Now()

	t.Run("GIVEN a pre-migration copy from an upgrade, older than the scheduled backups after it", func(t *testing.T) {
		store := newBackups()
		store.put("pre-migration-0009.db", now.Add(-72*time.Hour))
		store.put("gamevault-20261018-120000.db", now.Add(-48*time.Hour))
		store.put("gamevault-20261019-120000.db", now.Add(-24*time.Hour))

		svc := system.NewService(nil, copier{store}, store, nil,
			system.WithClock(func() time.Time { return now }), system.WithKeep(3))

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
				assert.Contains(t, store.backups, b.Name)
				assert.NotContains(t, store.backups, "pre-migration-0009.db")
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

	return []*game.Game{game.Rehydrate(game.Snapshot{
		ID:   "g1",
		Info: game.Info{Title: "Halo 3"},
		Copies: []game.Copy{{
			ID: "c1",
			CopyDetails: game.CopyDetails{
				Kind: game.KindPhysical,
			},
			Photos: photos,
		}},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})}, nil
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
		store := newBackups()
		games := &photoGames{ids: []game.PhotoID{photoID(1), photoID(2)}}
		archive := &fakeArchive{stored: map[game.PhotoID]bool{}}
		clock := time.Now()
		svc := system.NewService(games, copier{store}, store, nil, system.WithPhotoArchive(archive),
			system.WithClock(func() time.Time { clock = clock.Add(time.Second); return clock }), system.WithKeep(2))

		t.Run("WHEN a backup is made", func(t *testing.T) {
			b, err := svc.CreateBackup(ctx)
			require.NoError(t, err)

			t.Run("THEN its photo list is recorded and the photos are in the shared store", func(t *testing.T) {
				assert.Equal(t, []game.PhotoID{photoID(1), photoID(2)}, store.lists[b.Name])
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
				assert.Len(t, store.lists, 2)
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
	copier

	games *photoGames
	next  []game.PhotoID
}

func (b racingBackup) BackupTo(ctx context.Context, path string) error {
	b.games.ids = b.next

	return b.copier.BackupTo(ctx, path)
}

// failingArchive cannot store photos.
type failingArchive struct{ fakeArchive }

func (failingArchive) Add([]game.PhotoID) error { return errors.New("disk full") }

// unreadableLists is a store whose photo lists cannot be read.
type unreadableLists struct{ *memBackups }

func (unreadableLists) PhotoList(string) ([]game.PhotoID, error) {
	return nil, errors.New("permission denied")
}

func TestBackups_photosEdgeCases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN photos that change while the database is being copied", func(t *testing.T) {
		store := newBackups()
		games := &photoGames{ids: []game.PhotoID{photoID(1), photoID(2)}}
		archive := &fakeArchive{stored: map[game.PhotoID]bool{}}
		db := racingBackup{
			copier: copier{store},
			games:  games,
			next:   []game.PhotoID{photoID(2), photoID(3)},
		}
		svc := system.NewService(games, db, store, nil, system.WithPhotoArchive(archive), system.WithKeep(3))

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
		store := newBackups()
		old := time.Now().Add(-48 * time.Hour)
		store.put("gamevault-20261001-120000.db", old)
		store.put("gamevault-20261002-120000.db", old.Add(time.Hour))

		games := &photoGames{ids: []game.PhotoID{photoID(1)}}
		svc := system.NewService(games, copier{store}, store, nil, system.WithPhotoArchive(&failingArchive{}),
			system.WithKeep(2))

		t.Run("WHEN a backup is made", func(t *testing.T) {
			_, err := svc.CreateBackup(ctx)

			t.Run("THEN it reports the photos failed, keeps the database copy and still rotates", func(t *testing.T) {
				require.ErrorContains(t, err, "disk full")

				list, err := svc.ListBackups(ctx)
				require.NoError(t, err)
				assert.Len(t, list, 2)
				assert.NotContains(t, store.backups, "gamevault-20261001-120000.db")
			})
		})
	})

	t.Run("GIVEN more backups than are kept, whose photo lists cannot be read", func(t *testing.T) {
		store := newBackups()
		store.put("gamevault-20261001-120000.db", time.Now().Add(-48*time.Hour))

		archive := &fakeArchive{stored: map[game.PhotoID]bool{photoID(1): true}}
		svc := system.NewService(&photoGames{}, copier{store}, unreadableLists{store}, nil,
			system.WithPhotoArchive(archive), system.WithKeep(1))

		t.Run("WHEN a backup rotates the old one out", func(t *testing.T) {
			_, err := svc.CreateBackup(ctx)
			require.NoError(t, err)

			t.Run("THEN the shared photos stay: the kept backup may need them", func(t *testing.T) {
				assert.Equal(t, map[game.PhotoID]bool{photoID(1): true}, archive.stored)
			})
		})
	})
}
