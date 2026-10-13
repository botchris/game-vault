// Package backupdir keeps the database backups in a directory and implements system.BackupStore.
// A backup is a file named as the service named it (gamevault-20261013-120000.db); the photos it
// needs are listed next to it in a file with the same name ending in .photos, one id per line. A
// backup's date is its file's modification time, so copies made outside the service (the
// pre-migration copies the database writes before an upgrade) are listed and rotated too.
package backupdir

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gamevault/internal/application/system"
	"gamevault/internal/domain/game"
)

// Dir is a backups directory. It is created with the first backup.
type Dir struct {
	path string
}

// New returns the backups directory at path.
func New(path string) *Dir { return &Dir{path: path} }

// Create implements system.BackupStore.
func (d *Dir) Create(name string) (string, error) {
	path, err := d.file(name)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(d.path, 0o755); err != nil {
		return "", err
	}

	return path, nil
}

// Stat implements system.BackupStore.
func (d *Dir) Stat(name string) (system.Backup, error) {
	path, err := d.file(name)
	if err != nil {
		return system.Backup{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return system.Backup{}, err
	}

	return d.backup(name, info), nil
}

// List implements system.BackupStore. Every file ending in .db is a backup.
func (d *Dir) List() ([]system.Backup, error) {
	entries, err := os.ReadDir(d.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var out []system.Backup

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".db") {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue // removed since the directory was read
		}

		out = append(out, d.backup(e.Name(), info))
	}

	return out, nil
}

// Remove implements system.BackupStore.
func (d *Dir) Remove(name string) error {
	path, err := d.file(name)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		return err
	}

	if err := os.Remove(photoList(path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	return nil
}

// WritePhotoList implements system.BackupStore.
func (d *Dir) WritePhotoList(name string, ids []game.PhotoID) error {
	path, err := d.file(name)
	if err != nil {
		return err
	}

	var list strings.Builder
	for _, id := range ids {
		list.WriteString(string(id) + "\n")
	}

	return os.WriteFile(photoList(path), []byte(list.String()), 0o600)
}

// PhotoList implements system.BackupStore.
func (d *Dir) PhotoList(name string) ([]game.PhotoID, error) {
	path, err := d.file(name)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(photoList(path))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	var ids []game.PhotoID

	for line := range strings.SplitSeq(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, game.PhotoID(line))
		}
	}

	return ids, nil
}

// file returns the path of the backup called name, refusing a name that would leave the directory.
func (d *Dir) file(name string) (string, error) {
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		return "", fmt.Errorf("backup name %q is not a file name", name)
	}

	return filepath.Join(d.path, name), nil
}

// backup describes the backup file called name. A photo list that cannot be read counts as none:
// the number is only shown.
func (d *Dir) backup(name string, info fs.FileInfo) system.Backup {
	ids, _ := d.PhotoList(name) // see above: an unreadable list shows as no photos

	return system.Backup{
		Name:      name,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime(),
		Photos:    len(ids),
	}
}

// photoList is the file next to a backup that names the photos it needs.
func photoList(path string) string { return strings.TrimSuffix(path, ".db") + ".photos" }
