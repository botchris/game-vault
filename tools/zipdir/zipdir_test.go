package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZipDir(t *testing.T) {
	t.Run("GIVEN a folder with files, a subfolder and things to leave out", func(t *testing.T) {
		dir := t.TempDir()
		for name, body := range map[string]string{
			"manifest.json":         "{}",
			"lib/engine.js":         "export {}",
			"tests/engine.test.mjs": "test",
			"package.json":          "{}",
		} {
			p := filepath.Join(dir, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
			require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		}

		out := filepath.Join(t.TempDir(), "x.zip")

		t.Run("WHEN it is zipped leaving out tests/ and package.json", func(t *testing.T) {
			require.NoError(t, zipDir(dir, out, []string{"tests", "package.json"}))

			t.Run("THEN the zip holds the rest, with forward-slash paths", func(t *testing.T) {
				r, err := zip.OpenReader(out)
				require.NoError(t, err)

				defer r.Close()

				names := make([]string, 0, len(r.File))
				for _, f := range r.File {
					names = append(names, f.Name)
				}

				sort.Strings(names)
				assert.Equal(t, []string{"lib/engine.js", "manifest.json"}, names)
			})
		})
	})
}
