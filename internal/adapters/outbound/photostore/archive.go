package photostore

import (
	"os"
	"path/filepath"
	"time"

	"gamevault/internal/domain/game"
)

// Archive fills the backups' shared photo store from the live one and implements
// system.PhotoArchive. Each photo is stored there once, however many backups list it.
type Archive struct {
	live   *Store
	backup *Store

	// link makes dst another name of src; os.Link, replaced in tests.
	link func(src, dst string) error
}

// NewArchive returns the archive that copies photos from live into backup.
func NewArchive(live, backup *Store) *Archive {
	return &Archive{
		live:   live,
		backup: backup,
		link:   os.Link,
	}
}

// Add implements system.PhotoArchive. Photos are hard-linked (they never change, so the backup
// costs no space while the live photo exists) and copied when linking is not possible. A photo
// missing from the live store is skipped: there is nothing to keep.
func (a *Archive) Add(ids []game.PhotoID) error {
	for _, id := range ids {
		if !valid(id) {
			continue
		}

		for _, thumb := range []bool{false, true} {
			src, dst := a.live.path(id, thumb), a.backup.path(id, thumb)
			if _, err := os.Stat(dst); err == nil {
				continue
			}

			if _, err := os.Stat(src); os.IsNotExist(err) {
				continue
			}

			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}

			if err := a.link(src, dst); err == nil {
				continue
			}

			data, err := os.ReadFile(src)
			if err != nil {
				return err
			}

			if err := writeAtomic(dst, data); err != nil {
				return err
			}
		}
	}

	return nil
}

// Retain implements system.PhotoArchive.
func (a *Archive) Retain(keep map[game.PhotoID]bool) error {
	_, err := a.backup.Prune(keep, time.Time{})

	return err
}

// Size implements system.PhotoArchive.
func (a *Archive) Size() (int64, error) { return a.backup.Size() }
