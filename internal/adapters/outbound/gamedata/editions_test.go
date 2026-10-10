package gamedata

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
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

	t.Run("GIVEN a cover stored before editions", func(t *testing.T) {
		root := t.TempDir()
		s, err := Open(root)
		require.NoError(t, err)

		legacy := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(legacy, string(g.ID)+".jpg"), jpg.Data, 0o600))
		_, err = s.MigrateLegacyCovers(legacy, map[game.ID]string{g.ID: g.Title})
		require.NoError(t, err)

		t.Run("WHEN one edition's missing marker is cleared", func(t *testing.T) {
			require.NoError(t, s.MarkCoverMissing(g, "Wii", time.Now()))
			require.NoError(t, s.ClearCoverMissing(g.ID, "Wii"))

			t.Run("THEN the marker is gone and the cover from before editions stays", func(t *testing.T) {
				_, ok := s.CoverMissingSince(g.ID, "Wii")
				assert.False(t, ok)

				_, err := os.Stat(filepath.Join(root, "Halo 3 [019b11c2-bbbb]", "cover.jpg"))
				assert.NoError(t, err)
			})
		})

		t.Run("WHEN another edition's cover is deleted THEN the cover from before editions stays", func(t *testing.T) {
			require.NoError(t, s.PutCover(g, "Wii", jpg))
			require.NoError(t, s.DeleteCover(g.ID, "Wii"))

			_, err := os.Stat(filepath.Join(root, "Halo 3 [019b11c2-bbbb]", "cover.jpg"))
			assert.NoError(t, err)
		})

		t.Run("WHEN the main edition adopts it", func(t *testing.T) {
			img, ok, err := s.AdoptLegacyCover(g, "PS3")
			require.NoError(t, err)

			t.Run("THEN it is that edition's cover, and it can only be adopted once", func(t *testing.T) {
				assert.True(t, ok)
				assert.Equal(t, jpg.Data, img.Data)

				_, ok, _ = s.GetCover(g.ID, "PS3")
				assert.True(t, ok)

				_, ok, _ = s.AdoptLegacyCover(g, "PC")
				assert.False(t, ok)
			})
		})
	})

	t.Run("GIVEN a cover stored before editions, and its missing marker", func(t *testing.T) {
		root := t.TempDir()
		s, err := Open(root)
		require.NoError(t, err)

		legacy := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(legacy, string(g.ID)+".jpg"), jpg.Data, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(legacy, string(g.ID)+".missing"), []byte("2026-10-01T00:00:00Z"), 0o600))
		_, err = s.MigrateLegacyCovers(legacy, map[game.ID]string{g.ID: g.Title})
		require.NoError(t, err)

		t.Run("WHEN it is deleted THEN no edition can adopt it, and the marker is gone", func(t *testing.T) {
			require.NoError(t, s.DeleteLegacyCover(g.ID))

			_, ok, _ := s.AdoptLegacyCover(g, "PS3")
			assert.False(t, ok)
			assert.Empty(t, filesLike(t, filepath.Join(root, "Halo 3 [019b11c2-bbbb]"), "cover*"))
		})
	})
}
