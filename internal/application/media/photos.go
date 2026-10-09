package media

import (
	"errors"
	"time"

	"gamevault/internal/domain/game"
)

// ErrNoPhoto means there is no stored photo with that id.
var ErrNoPhoto = errors.New("no such photo")

// PhotoStore is the port that keeps the photos users upload of their copies, named after their
// content so each image is stored once.
type PhotoStore interface {
	// Put stores a photo and its thumbnail under id. When they are already stored, it keeps them and
	// marks them as just written, so Prune does not delete them before they are attached.
	Put(id game.PhotoID, photo, thumb []byte) error

	// Has reports whether the photo and its thumbnail are stored.
	Has(id game.PhotoID) bool

	// Open returns the photo, or its thumbnail, or ErrNoPhoto.
	Open(id game.PhotoID, thumb bool) (Image, error)

	// Prune deletes the photos not in keep whose files were written before before (any age when
	// before is zero), and returns how many photos it deleted.
	Prune(keep map[game.PhotoID]bool, before time.Time) (int, error)
}
