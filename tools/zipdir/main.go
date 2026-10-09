// Command zipdir packs a folder into a zip, leaving out the given top-level entries. It packs the
// browser extension for the extension stores (task extension:pack) without needing zip on the
// host or in the toolchain image. Usage: zipdir DIR OUT.zip [EXCLUDE...].
package main

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: zipdir DIR OUT.zip [EXCLUDE...]")
		os.Exit(2)
	}

	if err := zipDir(os.Args[1], os.Args[2], os.Args[3:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// zipDir writes every file under dir into out, with paths relative to dir; top-level entries named
// in exclude (files or folders) are left out.
func zipDir(dir, out string, exclude []string) (err error) {
	f, err := os.Create(out)
	if err != nil {
		return err
	}

	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	w := zip.NewWriter(f)

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil || rel == "." {
			return err
		}

		rel = filepath.ToSlash(rel)
		if slices.Contains(exclude, strings.SplitN(rel, "/", 2)[0]) {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			return nil
		}

		return addFile(w, path, rel)
	})
	if walkErr != nil {
		_ = w.Close() // the walk error is the one worth reporting

		return walkErr
	}

	return w.Close()
}

func addFile(w *zip.Writer, path, name string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }() // only read

	dst, err := w.Create(name)
	if err != nil {
		return err
	}

	_, err = io.Copy(dst, src)

	return err
}
