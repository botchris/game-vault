package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
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

// ErrInvalidPhoto wraps the reasons an uploaded photo is refused; the message says what is wrong.
var ErrInvalidPhoto = errors.New("invalid photo")

// Limits of an upload. The browser sends at most 2560 px and 400 px; the margins allow other clients.
const (
	MaxPhotoBytes = 15 << 20
	MaxThumbBytes = 1 << 20
	maxPhotoSide  = 8000
	maxThumbSide  = 512

	// unattachedGrace is how long an unreferenced photo is kept: it may be about to be attached, or
	// have been removed by mistake and be added back.
	unattachedGrace = 24 * time.Hour
)

// PhotoUpload is a stored photo, ready to be attached to copies.
type PhotoUpload struct {
	ID game.PhotoID

	// TakenAt comes from the photo's metadata; zero when unknown.
	TakenAt time.Time
}

// UploadPhoto checks and stores a photo and its thumbnail, both JPEG, and reads when the photo was
// taken. The id is the photo's SHA-256, so uploading the same photo again gives the same id.
func (s *Service) UploadPhoto(photo, thumb []byte) (PhotoUpload, error) {
	if s.photos == nil {
		return PhotoUpload{}, errors.New("photos are not available on this server")
	}

	if err := checkJPEG("photo", photo, MaxPhotoBytes, maxPhotoSide); err != nil {
		return PhotoUpload{}, err
	}

	if err := checkJPEG("thumbnail", thumb, MaxThumbBytes, maxThumbSide); err != nil {
		return PhotoUpload{}, err
	}

	id := game.PhotoIDOf(photo)
	if err := s.photos.Put(id, photo, thumb); err != nil {
		return PhotoUpload{}, fmt.Errorf("storing the photo: %w", err)
	}

	taken, _ := exifTakenAt(photo)

	return PhotoUpload{
		ID:      id,
		TakenAt: taken,
	}, nil
}

func checkJPEG(what string, data []byte, maxBytes, maxSide int) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: the %s is missing", ErrInvalidPhoto, what)
	}

	if len(data) > maxBytes {
		return fmt.Errorf("%w: the %s is larger than %d MB", ErrInvalidPhoto, what, maxBytes>>20)
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" {
		return fmt.Errorf("%w: the %s is not a JPEG image", ErrInvalidPhoto, what)
	}

	if cfg.Width > maxSide || cfg.Height > maxSide {
		return fmt.Errorf("%w: the %s is larger than %d pixels", ErrInvalidPhoto, what, maxSide)
	}

	return nil
}

// Photo returns a stored photo or its thumbnail, or ErrNoPhoto.
func (s *Service) Photo(id game.PhotoID, thumb bool) (Image, error) {
	if s.photos == nil {
		return Image{}, ErrNoPhoto
	}

	return s.photos.Open(id, thumb)
}

// PrunePhotos deletes the stored photos no copy shows any more, once they are a day old.
func (s *Service) PrunePhotos(ctx context.Context) (int, error) {
	if s.photos == nil {
		return 0, nil
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return 0, err
	}

	keep := map[game.PhotoID]bool{}

	for _, g := range games {
		for _, id := range g.PhotoIDs() {
			keep[id] = true
		}
	}

	return s.photos.Prune(keep, s.now().Add(-unattachedGrace))
}

// RunPhotoCleanup prunes unused photos after first and then every interval until ctx ends.
func (s *Service) RunPhotoCleanup(ctx context.Context, first, every time.Duration) {
	timer := time.NewTimer(first)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if n, err := s.PrunePhotos(ctx); err != nil {
				s.log.Warn("pruning unused photos", "error", err)
			} else if n > 0 {
				s.log.Info("unused photos deleted", "count", n)
			}

			timer.Reset(every)
		}
	}
}
