package photostore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

var _ media.PhotoStore = (*Store)(nil)

func put(t *testing.T, s *Store, data string) game.PhotoID {
	t.Helper()

	id := game.PhotoIDOf([]byte(data))
	require.NoError(t, s.Put(id, []byte(data), []byte("thumb of "+data)))

	return id
}

func age(t *testing.T, s *Store, id game.PhotoID, at time.Time) {
	t.Helper()

	for _, thumb := range []bool{false, true} {
		require.NoError(t, os.Chtimes(s.path(id, thumb), at, at))
	}
}

func TestStore(t *testing.T) {
	t.Run("GIVEN an empty store", func(t *testing.T) {
		s, err := Open(t.TempDir())
		require.NoError(t, err)

		t.Run("WHEN a photo is put", func(t *testing.T) {
			id := put(t, s, "photo one")

			t.Run("THEN it is stored under its id's first two characters, with its thumbnail", func(t *testing.T) {
				assert.FileExists(t, filepath.Join(s.root, string(id)[:2], string(id)+".jpg"))
				assert.FileExists(t, filepath.Join(s.root, string(id)[:2], string(id)+"-thumb.jpg"))
				assert.True(t, s.Has(id))

				img, err := s.Open(id, true)
				require.NoError(t, err)
				assert.Equal(t, "thumb of photo one", string(img.Data))
				assert.Equal(t, "image/jpeg", img.ContentType)
			})

			t.Run("AND no temporary file is left behind", func(t *testing.T) {
				entries, err := os.ReadDir(filepath.Join(s.root, string(id)[:2]))
				require.NoError(t, err)
				assert.Len(t, entries, 2)
			})
		})

		t.Run("WHEN an unknown or malformed id is opened", func(t *testing.T) {
			_, errUnknown := s.Open(game.PhotoIDOf([]byte("nothing")), false)
			_, errBad := s.Open("../../etc/passwd", false)

			t.Run("THEN there is no photo", func(t *testing.T) {
				assert.ErrorIs(t, errUnknown, media.ErrNoPhoto)
				assert.ErrorIs(t, errBad, media.ErrNoPhoto)
				assert.False(t, s.Has("../../etc/passwd"))
			})
		})
	})
}

func TestStore_prune(t *testing.T) {
	t.Run("GIVEN a kept photo, an old unreferenced one, a recent unreferenced one, and an old one uploaded again", func(t *testing.T) {
		s, err := Open(t.TempDir())
		require.NoError(t, err)

		old := time.Now().Add(-48 * time.Hour)
		kept, stale, fresh, again := put(t, s, "kept"), put(t, s, "stale"), put(t, s, "fresh"), put(t, s, "again")
		age(t, s, kept, old)
		age(t, s, stale, old)
		age(t, s, again, old)
		put(t, s, "again") // uploaded again: about to be attached

		t.Run("WHEN files older than a day that nothing references are pruned", func(t *testing.T) {
			n, err := s.Prune(map[game.PhotoID]bool{kept: true}, time.Now().Add(-24*time.Hour))
			require.NoError(t, err)

			t.Run("THEN only the old unreferenced photo goes, thumbnail included", func(t *testing.T) {
				assert.Equal(t, 1, n)
				assert.False(t, s.Has(stale))
				assert.NoFileExists(t, s.path(stale, true))
				assert.True(t, s.Has(kept))
				assert.True(t, s.Has(fresh))
				assert.True(t, s.Has(again))
			})
		})

		t.Run("WHEN pruning with no age limit", func(t *testing.T) {
			_, err := s.Prune(map[game.PhotoID]bool{kept: true}, time.Time{})
			require.NoError(t, err)

			t.Run("THEN every unreferenced photo goes", func(t *testing.T) {
				assert.False(t, s.Has(fresh))
				assert.True(t, s.Has(kept))
			})
		})
	})
}

func TestArchive(t *testing.T) {
	t.Run("GIVEN a live store with two photos and an empty backup store", func(t *testing.T) {
		live, err := Open(t.TempDir())
		require.NoError(t, err)

		backup, err := Open(t.TempDir())
		require.NoError(t, err)

		a, b := put(t, live, "a"), put(t, live, "b")
		arch := NewArchive(live, backup)

		t.Run("WHEN both are archived, and then again", func(t *testing.T) {
			require.NoError(t, arch.Add([]game.PhotoID{a, b}))
			require.NoError(t, arch.Add([]game.PhotoID{a, b}))

			t.Run("THEN the backup store has each once, as hard links of the live files", func(t *testing.T) {
				assert.True(t, backup.Has(a))

				li, err := os.Stat(live.path(a, false))
				require.NoError(t, err)

				bi, err := os.Stat(backup.path(a, false))
				require.NoError(t, err)
				assert.True(t, os.SameFile(li, bi))
			})

			t.Run("AND a photo no backup lists any more is removed from the backup store only", func(t *testing.T) {
				require.NoError(t, arch.Retain(map[game.PhotoID]bool{a: true}))
				assert.False(t, backup.Has(b))
				assert.True(t, live.Has(b))
			})
		})

		t.Run("WHEN hard links are not possible (another file system)", func(t *testing.T) {
			c := put(t, live, "c")
			arch.link = func(string, string) error { return errors.New("cross-device link") }
			require.NoError(t, arch.Add([]game.PhotoID{c}))

			t.Run("THEN the photo is copied", func(t *testing.T) {
				img, err := backup.Open(c, false)
				require.NoError(t, err)
				assert.Equal(t, "c", string(img.Data))
			})
		})

		t.Run("WHEN a listed photo is missing from the live store", func(t *testing.T) {
			err := arch.Add([]game.PhotoID{game.PhotoIDOf([]byte("gone"))})

			t.Run("THEN it is skipped without failing the backup", func(t *testing.T) {
				assert.NoError(t, err)
			})
		})

		t.Run("WHEN the size is asked", func(t *testing.T) {
			n, err := arch.Size()
			require.NoError(t, err)

			t.Run("THEN it counts the backup store's files", func(t *testing.T) {
				assert.Positive(t, n)
			})
		})
	})
}
