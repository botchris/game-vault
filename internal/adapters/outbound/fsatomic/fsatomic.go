// Package fsatomic writes files so a reader never sees half of one: the data goes to a temporary
// file next to the target, which is then renamed over it.
package fsatomic

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to path atomically with the given permissions, creating the folder if
// needed. Each call gets its own temporary file, so two writers of the same path never mix their
// data (the last rename wins whole), and a failed write removes its temporary file.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}

	if err := write(tmp, data, perm); err != nil {
		_ = os.Remove(tmp.Name())

		return err
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())

		return err
	}

	return nil
}

// write fills and closes the temporary file. os.CreateTemp makes it readable by its owner only, so
// it gets the asked permissions before it takes the target's place.
func write(tmp *os.File, data []byte, perm os.FileMode) error {
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // the write error is the one worth reporting

		return err
	}

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close() // the chmod error is the one worth reporting

		return err
	}

	return tmp.Close()
}
