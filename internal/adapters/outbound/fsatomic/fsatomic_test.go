package fsatomic

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteFile(t *testing.T) {
	t.Run("GIVEN a folder that does not exist yet", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "game", "cover.jpg")

		t.Run("WHEN a file is written into it", func(t *testing.T) {
			require.NoError(t, WriteFile(path, []byte("image"), 0o644))

			t.Run("THEN the folder is created and the file holds the data", func(t *testing.T) {
				got, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, "image", string(got))
			})

			t.Run("AND it has the asked permissions", func(t *testing.T) {
				info, err := os.Stat(path)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
			})

			t.Run("AND no temporary file is left next to it", func(t *testing.T) {
				assert.Equal(t, []string{"cover.jpg"}, names(t, filepath.Dir(path)))
			})
		})
	})

	t.Run("GIVEN a folder where the file's name is taken by a folder", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "cover.jpg", "inside"), 0o755))

		t.Run("WHEN the file is written", func(t *testing.T) {
			err := WriteFile(filepath.Join(dir, "cover.jpg"), []byte("image"), 0o644)

			t.Run("THEN it fails and leaves no temporary file behind", func(t *testing.T) {
				require.Error(t, err)
				assert.Equal(t, []string{"cover.jpg"}, names(t, dir))
			})
		})
	})

	t.Run("GIVEN many writers of the same file at once", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cover.jpg")
		contents := []string{"aaaaaaaaaaaaaaaa", "bbbb", "cccccccccccccccccccccccccccccccc"}

		var wg sync.WaitGroup
		for i := range 30 {
			wg.Go(func() {
				assert.NoError(t, WriteFile(path, []byte(contents[i%len(contents)]), 0o644))
			})
		}

		wg.Wait()

		t.Run("THEN the file holds one writer's data whole", func(t *testing.T) {
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, contents, string(got))
		})
	})
}

func names(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}

	return out
}
