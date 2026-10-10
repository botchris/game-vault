package game

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxPhotosPerCopy is how many photos one copy can have.
const MaxPhotosPerCopy = 50

// maxCaptionLen is the longest photo caption, in characters.
const maxCaptionLen = 200

var rePhotoID = regexp.MustCompile(`^[0-9a-f]{64}$`)

// PhotoID identifies a photo by the SHA-256 of its stored JPEG, in lowercase hex. The photo's files
// are named after it, so the same image is stored once however many copies show it.
type PhotoID string

// PhotoIDOf returns the id of a stored JPEG.
func PhotoIDOf(jpeg []byte) PhotoID {
	sum := sha256.Sum256(jpeg)

	return PhotoID(hex.EncodeToString(sum[:]))
}

// ParsePhotoID validates a photo id received from outside.
func ParsePhotoID(s string) (PhotoID, error) {
	if !rePhotoID.MatchString(s) {
		return "", invalid("photo id %q is not valid", s)
	}

	return PhotoID(s), nil
}

// Photo is a picture of a copy the user uploaded.
type Photo struct {
	ID PhotoID

	// Caption is optional, at most 200 characters.
	Caption string

	// TakenAt is the camera's clock reading from the photo's metadata (EXIF has no zone), stored as
	// UTC; zero when unknown.
	TakenAt time.Time
	AddedAt time.Time
}

func normalizeCaption(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxCaptionLen {
		return "", invalid("a caption has at most %d characters", maxCaptionLen)
	}

	return s, nil
}

func photoIndex(photos []Photo, id PhotoID) int {
	return slices.IndexFunc(photos, func(p Photo) bool { return p.ID == id })
}

// AddPhotos appends photos to a copy. A photo the copy already has is not added again; a copy has
// at most MaxPhotosPerCopy photos.
func (g *Game) AddPhotos(copyID ID, photos []Photo, now time.Time) (Copy, error) {
	i := g.indexOf(copyID)
	if i < 0 {
		return Copy{}, ErrCopyNotFound
	}

	all := slices.Clone(g.copies[i].Photos)

	for _, p := range photos {
		if _, err := ParsePhotoID(string(p.ID)); err != nil {
			return Copy{}, err
		}

		if photoIndex(all, p.ID) >= 0 {
			continue
		}

		caption, err := normalizeCaption(p.Caption)
		if err != nil {
			return Copy{}, err
		}

		all = append(all, Photo{
			ID:      p.ID,
			Caption: caption,
			TakenAt: p.TakenAt.UTC(),
			AddedAt: now,
		})
	}

	if len(all) > MaxPhotosPerCopy {
		return Copy{}, invalid("a copy can have at most %d photos", MaxPhotosPerCopy)
	}

	g.copies[i].Photos = all

	return g.touchCopy(i, now), nil
}

// UpdatePhoto changes the caption of one of a copy's photos.
func (g *Game) UpdatePhoto(copyID ID, photoID PhotoID, caption string, now time.Time) (Copy, error) {
	i, j, err := g.photoAt(copyID, photoID)
	if err != nil {
		return Copy{}, err
	}

	caption, err = normalizeCaption(caption)
	if err != nil {
		return Copy{}, err
	}

	g.copies[i].Photos[j].Caption = caption

	return g.touchCopy(i, now), nil
}

// RemovePhoto removes a photo from a copy. An edition's cover photo is cleared when no copy of
// that edition has it any more.
func (g *Game) RemovePhoto(copyID ID, photoID PhotoID, now time.Time) (Copy, error) {
	i, j, err := g.photoAt(copyID, photoID)
	if err != nil {
		return Copy{}, err
	}

	g.copies[i].Photos = slices.Delete(slices.Clone(g.copies[i].Photos), j, j+1)
	g.reconcileEditions()

	return g.touchCopy(i, now), nil
}

// ReorderPhotos puts a copy's photos in the given order, which must name each of them exactly once.
func (g *Game) ReorderPhotos(copyID ID, ids []PhotoID, now time.Time) (Copy, error) {
	i := g.indexOf(copyID)
	if i < 0 {
		return Copy{}, ErrCopyNotFound
	}

	current := g.copies[i].Photos
	if len(ids) != len(current) {
		return Copy{}, invalid("the new order must list each of the copy's photos once")
	}

	ordered := make([]Photo, 0, len(ids))

	for _, id := range ids {
		j := photoIndex(current, id)
		if j < 0 || photoIndex(ordered, id) >= 0 {
			return Copy{}, invalid("the new order must list each of the copy's photos once")
		}

		ordered = append(ordered, current[j])
	}

	g.copies[i].Photos = ordered

	return g.touchCopy(i, now), nil
}

// PhotoIDs returns every photo of the game's copies, each once, in copy order.
func (g *Game) PhotoIDs() []PhotoID {
	var out []PhotoID

	for _, c := range g.copies {
		for _, p := range c.Photos {
			if !slices.Contains(out, p.ID) {
				out = append(out, p.ID)
			}
		}
	}

	return out
}

func (g *Game) photoAt(copyID ID, photoID PhotoID) (int, int, error) {
	i := g.indexOf(copyID)
	if i < 0 {
		return 0, 0, ErrCopyNotFound
	}

	j := photoIndex(g.copies[i].Photos, photoID)
	if j < 0 {
		return 0, 0, ErrPhotoNotFound
	}

	return i, j, nil
}

func (g *Game) touchCopy(i int, now time.Time) Copy {
	g.copies[i].UpdatedAt = now
	g.updatedAt = now

	return g.copies[i].clone()
}

// clone returns the copy with its own photo slice, so callers cannot change the aggregate.
func (c Copy) clone() Copy {
	c.Photos = slices.Clone(c.Photos)
	c.Estimates = slices.Clone(c.Estimates)
	c.Fields = c.Fields.compact()

	return c
}
