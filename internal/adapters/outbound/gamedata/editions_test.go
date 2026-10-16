package gamedata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
)

// filesLike returns the names of the files in dir that match pattern. filepath.Glob cannot be used:
// the "[id]" of a game folder's name is a character class to it.
func filesLike(t *testing.T, dir, pattern string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var out []string

	for _, e := range entries {
		if ok, _ := filepath.Match(pattern, e.Name()); ok {
			out = append(out, e.Name())
		}
	}

	return out
}

func TestEditionCoverFiles(t *testing.T) {
	png := media.Image{
		Data:        []byte("png"),
		ContentType: "image/png",
	}
	g := media.GameRef{
		ID:    "019b11c2-bbbb",
		Title: "Halo 3",
	}

	t.Run("GIVEN covers stored for two systems, one with a slash and spaces", func(t *testing.T) {
		root := t.TempDir()
		s, err := Open(root)
		require.NoError(t, err)
		require.NoError(t, s.PutCover(g, "PS3", jpg))
		require.NoError(t, s.PutCover(g, "Xbox 360/S Slim", png))

		t.Run("THEN each reads back on its own", func(t *testing.T) {
			img, ok, err := s.GetCover(g.ID, "PS3")
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, jpg.Data, img.Data)

			img, ok, _ = s.GetCover(g.ID, "Xbox 360/S Slim")
			assert.True(t, ok)
			assert.Equal(t, png.Data, img.Data)

			_, ok, _ = s.GetCover(g.ID, "Wii")
			assert.False(t, ok)
		})

		t.Run("AND the files stay in the game's folder, named after the system", func(t *testing.T) {
			dir := filepath.Join(root, "Halo 3 [019b11c2-bbbb]")
			assert.Len(t, filesLike(t, dir, "cover-ps3-*.jpg"), 1)
			assert.Len(t, filesLike(t, dir, "cover-xbox-360-s-slim-*.png"), 1)
		})

		t.Run("AND a missing marker is per system", func(t *testing.T) {
			at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
			require.NoError(t, s.MarkCoverMissing(g, "Wii", at))

			since, ok := s.CoverMissingSince(g.ID, "Wii")
			assert.True(t, ok)
			assert.True(t, since.Equal(at))

			_, ok = s.CoverMissingSince(g.ID, "PS3")
			assert.False(t, ok)
		})

		t.Run("AND pruning sheet images never touches covers", func(t *testing.T) {
			require.NoError(t, s.SetAssetSources(g, map[string]string{"screenshot-1": "https://x.test/1.jpg"}))

			_, ok, _ := s.GetCover(g.ID, "PS3")
			assert.True(t, ok)
		})

		t.Run("AND a sheet image cannot take a cover's name", func(t *testing.T) {
			assert.Error(t, s.PutAsset(g, "cover-ps3", jpg))
		})

		t.Run("WHEN one system's cover is deleted THEN the others stay", func(t *testing.T) {
			require.NoError(t, s.DeleteCover(g.ID, "PS3"))

			_, ok, _ := s.GetCover(g.ID, "PS3")
			assert.False(t, ok)

			_, ok, _ = s.GetCover(g.ID, "Xbox 360/S Slim")
			assert.True(t, ok)
		})

		t.Run("WHEN every cover is deleted THEN no system has one, nor a missing marker", func(t *testing.T) {
			require.NoError(t, s.DeleteCovers(g.ID))

			_, ok, _ := s.GetCover(g.ID, "Xbox 360/S Slim")
			assert.False(t, ok)

			_, ok = s.CoverMissingSince(g.ID, "Wii")
			assert.False(t, ok)
		})
	})
}

func TestEditionCoverFiles_catalogNameWithSlash(t *testing.T) {
	g := media.GameRef{
		ID:    "019b11c2-cccc",
		Title: "Sonic",
	}

	t.Run("GIVEN a cover saved for a catalog system whose name has a slash", func(t *testing.T) {
		root := t.TempDir()
		s, err := Open(root)
		require.NoError(t, err)
		require.NoError(t, s.PutCover(g, "Mega Drive / Genesis", jpg))

		t.Run("THEN it reads back and the file name has no slash", func(t *testing.T) {
			img, ok, err := s.GetCover(g.ID, "Mega Drive / Genesis")
			require.NoError(t, err)
			assert.True(t, ok)
			assert.Equal(t, jpg.Data, img.Data)

			assert.NotContains(t, coverFile("Mega Drive / Genesis"), "/")
			assert.Len(t, filesLike(t, filepath.Join(root, "Sonic [019b11c2-cccc]"), "cover-mega-drive-genesis-*.jpg"), 1)
		})
	})
}

func TestPruneCoverMarkers(t *testing.T) {
	g := media.GameRef{
		ID:    "019b11c2-cccc",
		Title: "Halo 3",
	}
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

	t.Run("GIVEN misses remembered for PS3 and Xbox 360, and a Wii cover", func(t *testing.T) {
		root := t.TempDir()
		s, err := Open(root)
		require.NoError(t, err)
		require.NoError(t, s.MarkCoverMissing(g, "PS3", at))
		require.NoError(t, s.MarkCoverMissing(g, "Xbox 360", at))
		require.NoError(t, s.PutCover(g, "Wii", jpg))

		t.Run("WHEN the markers of systems other than PS3 and Wii are pruned", func(t *testing.T) {
			n, err := s.PruneCoverMarkers(g.ID, []string{"PS3", "Wii"})
			require.NoError(t, err)

			t.Run("THEN the Xbox 360 marker goes", func(t *testing.T) {
				assert.Equal(t, 1, n)

				_, ok := s.CoverMissingSince(g.ID, "Xbox 360")
				assert.False(t, ok)
			})

			t.Run("AND the PS3 marker and every cover stay", func(t *testing.T) {
				_, ok := s.CoverMissingSince(g.ID, "PS3")
				assert.True(t, ok)

				_, ok, err = s.GetCover(g.ID, "Wii")
				require.NoError(t, err)
				assert.True(t, ok)
			})
		})
	})

	t.Run("GIVEN a game with nothing stored", func(t *testing.T) {
		s, err := Open(t.TempDir())
		require.NoError(t, err)

		t.Run("THEN pruning finds nothing to do", func(t *testing.T) {
			n, err := s.PruneCoverMarkers(g.ID, nil)
			require.NoError(t, err)
			assert.Zero(t, n)
		})
	})
}
