// Package photostore keeps the photos users upload of their copies, named after their content, and
// implements media.PhotoStore:
//
//	config/photos/
//	  3f/
//	    3fa9…e1.jpg        the photo (at most 2560 px), named after its SHA-256
//	    3fa9…e1-thumb.jpg  its thumbnail (at most 400 px)
//
// A stored file never changes (its name is its content's hash), so the same layout also serves as
// the backups' shared store, filled with hard links (see Archive).
package photostore

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

var reFile = regexp.MustCompile(`^([0-9a-f]{64})(-thumb)?\.jpg$`)

// Store implements media.PhotoStore on a directory.
type Store struct {
	root string
}

// Open returns the store in root, creating the directory if needed.
func Open(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}

	return &Store{root: root}, nil
}

func (s *Store) path(id game.PhotoID, thumb bool) string {
	name := string(id) + ".jpg"
	if thumb {
		name = string(id) + "-thumb.jpg"
	}

	return filepath.Join(s.root, string(id)[:2], name)
}

func valid(id game.PhotoID) bool {
	_, err := game.ParsePhotoID(string(id))

	return err == nil
}

// Put implements media.PhotoStore.
func (s *Store) Put(id game.PhotoID, photo, thumb []byte) error {
	if !valid(id) {
		return media.ErrNoPhoto
	}

	now := time.Now()

	for _, f := range []struct {
		thumb bool
		data  []byte
	}{{false, photo}, {true, thumb}} {
		p := s.path(id, f.thumb)
		if _, err := os.Stat(p); err == nil {
			// Already stored: mark it as just written so a prune does not delete it before it is
			// attached (it may be an old photo that nothing referenced any more).
			if err := os.Chtimes(p, now, now); err != nil {
				return err
			}

			continue
		}

		if err := writeAtomic(p, f.data); err != nil {
			return err
		}
	}

	return nil
}

// writeAtomic writes data to a temporary file next to path and renames it, so a reader never sees
// a half-written photo.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // the write error is the one worth reporting
		_ = os.Remove(tmp.Name())

		return err
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())

		return err
	}

	return os.Rename(tmp.Name(), path)
}

// Has implements media.PhotoStore.
func (s *Store) Has(id game.PhotoID) bool {
	if !valid(id) {
		return false
	}

	for _, thumb := range []bool{false, true} {
		if _, err := os.Stat(s.path(id, thumb)); err != nil {
			return false
		}
	}

	return true
}

// Open implements media.PhotoStore.
func (s *Store) Open(id game.PhotoID, thumb bool) (media.Image, error) {
	if !valid(id) {
		return media.Image{}, media.ErrNoPhoto
	}

	data, err := os.ReadFile(s.path(id, thumb))
	if os.IsNotExist(err) {
		return media.Image{}, media.ErrNoPhoto
	}

	if err != nil {
		return media.Image{}, err
	}

	return media.Image{
		Data:        data,
		ContentType: "image/jpeg",
	}, nil
}

// Prune implements media.PhotoStore. Stray temporary files older than before go too.
func (s *Store) Prune(keep map[game.PhotoID]bool, before time.Time) (int, error) {
	n := 0
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		m := reFile.FindStringSubmatch(d.Name())
		temp := strings.HasPrefix(d.Name(), ".tmp-")

		if (m == nil && !temp) || (m != nil && keep[game.PhotoID(m[1])]) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if !before.IsZero() && !info.ModTime().Before(before) {
			return nil
		}

		if err := os.Remove(path); err != nil {
			return err
		}

		if m != nil && m[2] == "" {
			n++
		}

		return nil
	})

	return n, err
}

// Size returns the total size of the stored files in bytes.
func (s *Store) Size() (int64, error) {
	var total int64

	err := filepath.WalkDir(s.root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		total += info.Size()

		return nil
	})

	return total, err
}
