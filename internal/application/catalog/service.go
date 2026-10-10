// Package catalog implements the use cases to browse and edit games and their copies.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
)

// CoverCache is the port used to drop a game's cached cover image when it may have changed.
type CoverCache interface {
	// Invalidate drops the cached cover, so the next request resolves it again.
	Invalidate(ctx context.Context, id game.ID) error
}

// PhotoFiles is the port that tells whether an uploaded photo's files are stored.
type PhotoFiles interface {
	// Has reports whether the photo and its thumbnail are stored.
	Has(id game.PhotoID) bool
}

// ErrPhotoNotUploaded means a photo to attach is not stored: it was never uploaded, or it was
// pruned before being attached.
var ErrPhotoNotUploaded = errors.New("photo not uploaded: upload it again")

// Service exposes the catalog use cases.
type Service struct {
	games  game.Repository
	tx     port.TxManager
	now    port.Clock
	covers CoverCache
	photos PhotoFiles
	fields field.Repository
}

// NewService builds the service. covers may be nil when no cover cache is wired; photos may be nil,
// and attached photos are then not checked; fields may be nil: no custom fields are defined.
func NewService(games game.Repository, tx port.TxManager, now port.Clock, covers CoverCache, photos PhotoFiles, fields field.Repository) *Service {
	return &Service{
		games:  games,
		tx:     tx,
		now:    now,
		covers: covers,
		photos: photos,
		fields: fields,
	}
}

// fieldSet returns the custom field definitions (an empty set when none are wired).
func (s *Service) fieldSet(ctx context.Context) (*field.Set, error) {
	if s.fields == nil {
		return field.NewSet(nil), nil
	}

	return s.fields.Fields(ctx)
}

// invalidateCover is best effort: a stale cached image is not worth failing the use case.
func (s *Service) invalidateCover(ctx context.Context, id game.ID) {
	if s.covers != nil {
		_ = s.covers.Invalidate(ctx, id)
	}
}

// ListGames returns every game in the catalog.
func (s *Service) ListGames(ctx context.Context) ([]*game.Game, error) {
	return s.games.List(ctx)
}

// GetGame returns one game with its copies.
func (s *Service) GetGame(ctx context.Context, id game.ID) (*game.Game, error) {
	return s.games.Get(ctx, id)
}

// CreateGame registers a game, optionally with some copies. copyFields[i] holds the custom field
// values of copies[i]; copies without an entry get none.
func (s *Service) CreateGame(ctx context.Context, info game.Info, copies []game.CopyDetails, copyFields ...game.FieldValues) (*game.Game, error) {
	var g *game.Game

	// The field definitions are read in the write's transaction, so a field deleted meanwhile is
	// refused instead of stored (SQLite has a single writer: the two cannot interleave).
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		now := s.now()

		set, err := s.fieldSet(ctx)
		if err != nil {
			return err
		}

		if info.Fields, err = set.Validate(field.ScopeGame, "", info.Fields); err != nil {
			return err
		}

		if g, err = game.New(info.Title, now); err != nil {
			return err
		}

		if _, err := g.UpdateInfo(info, now); err != nil {
			return err
		}

		for i, d := range copies {
			c, err := g.AddCopy(d, now)
			if err != nil {
				return err
			}

			if i >= len(copyFields) {
				continue
			}

			values, err := set.Validate(field.ScopeCopy, c.Kind, copyFields[i])
			if err != nil {
				return err
			}

			if err := g.SetCopyFields(c.ID, values, now); err != nil {
				return err
			}
		}

		return s.games.Save(ctx, g)
	})
	if err != nil {
		return nil, err
	}

	return g, nil
}

// UpdateGame changes a game's own attributes and drops its cached cover when the cover changed.
func (s *Service) UpdateGame(ctx context.Context, id game.ID, info game.Info) (*game.Game, error) {
	var coverChanged bool

	g, err := s.mutate(ctx, id, func(ctx context.Context, g *game.Game) error {
		set, err := s.fieldSet(ctx)
		if err != nil {
			return err
		}

		// Editing the game keeps its cover photo (SetCoverPhoto changes it), unless the user chose
		// another custom cover.
		info.CoverPhoto = g.CoverPhoto()
		if info.CoverURL != g.CoverURL() {
			info.CoverPhoto = ""
		}

		if info.Fields, err = set.Validate(field.ScopeGame, "", info.Fields); err != nil {
			return err
		}

		coverChanged, err = g.UpdateInfo(info, s.now())

		return err
	})
	if err == nil && coverChanged {
		s.invalidateCover(ctx, id)
	}

	return g, err
}

// DeleteGame removes a game with all its copies and its cached cover.
func (s *Service) DeleteGame(ctx context.Context, id game.ID) error {
	if err := s.games.Delete(ctx, id); err != nil {
		return err
	}

	s.invalidateCover(ctx, id)

	return nil
}

// AddCopy adds a copy with its custom field values to a game and returns the updated game.
func (s *Service) AddCopy(ctx context.Context, id game.ID, d game.CopyDetails, fields game.FieldValues) (*game.Game, error) {
	return s.mutate(ctx, id, func(ctx context.Context, g *game.Game) error {
		set, err := s.fieldSet(ctx)
		if err != nil {
			return err
		}

		now := s.now()

		c, err := g.AddCopy(d, now)
		if err != nil {
			return err
		}

		values, err := set.Validate(field.ScopeCopy, c.Kind, fields)
		if err != nil {
			return err
		}

		return g.SetCopyFields(c.ID, values, now)
	})
}

// UpdateCopy changes one copy of a game and returns the updated game. The custom field values
// replace the copy's: nil clears them, because the edit form always sends all of them.
func (s *Service) UpdateCopy(ctx context.Context, id, copyID game.ID, d game.CopyDetails, fields game.FieldValues) (*game.Game, error) {
	return s.mutate(ctx, id, func(ctx context.Context, g *game.Game) error {
		set, err := s.fieldSet(ctx)
		if err != nil {
			return err
		}

		now := s.now()

		c, err := g.UpdateCopy(copyID, d, now)
		if err != nil {
			return err
		}

		values, err := set.Validate(field.ScopeCopy, c.Kind, fields)
		if err != nil {
			return err
		}

		return g.SetCopyFields(copyID, values, now)
	})
}

// DeleteCopy removes one copy from a game and returns the updated game.
func (s *Service) DeleteCopy(ctx context.Context, id, copyID game.ID) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.RemoveCopy(copyID, s.now())
		return err
	})
}

// MergeGames moves the copies of every source game into target and deletes the sources.
func (s *Service) MergeGames(ctx context.Context, target game.ID, sources []game.ID) (*game.Game, error) {
	if slices.Contains(sources, target) {
		return nil, errors.New("a game cannot be merged into itself")
	}

	var merged *game.Game

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		t, err := s.games.Get(ctx, target)
		if err != nil {
			return err
		}

		now := s.now()
		for _, id := range sources {
			src, err := s.games.Get(ctx, id)
			if err != nil {
				return err
			}

			t.Absorb(src, now)
			// Save the target first so moved copies are re-parented before the source is deleted.
			if err := s.games.Save(ctx, t); err != nil {
				return err
			}

			if err := s.games.Delete(ctx, id); err != nil {
				return err
			}
		}

		merged = t

		return nil
	})
	if err == nil {
		s.invalidateCover(ctx, target) // the target may have adopted links or a cover

		for _, id := range sources {
			s.invalidateCover(ctx, id)
		}
	}

	return merged, err
}

// MoveCopy moves a copy to another game, or to a new game titled newTitle when target is empty.
// The source game is deleted if it ends up without copies; in that case the first result is nil.
func (s *Service) MoveCopy(ctx context.Context, from, copyID, target game.ID, newTitle string) (*game.Game, *game.Game, error) {
	var (
		src, dst *game.Game
		srcCover game.PhotoID
	)

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if src, err = s.games.Get(ctx, from); err != nil {
			return err
		}

		srcCover = src.CoverPhoto()

		now := s.now()
		if target != "" {
			if target == from {
				return errors.New("the copy is already in that game")
			}

			if dst, err = s.games.Get(ctx, target); err != nil {
				return err
			}
		} else if dst, err = game.New(newTitle, now); err != nil {
			return err
		}

		c, err := src.RemoveCopy(copyID, now)
		if err != nil {
			return err
		}

		dst.AttachCopy(c, now)

		if err := s.games.Save(ctx, dst); err != nil {
			return err
		}

		if len(src.Copies()) == 0 {
			src = nil
			return s.games.Delete(ctx, from)
		}

		return s.games.Save(ctx, src)
	})
	if err != nil {
		return nil, nil, err
	}

	if src != nil && src.CoverPhoto() != srcCover {
		s.invalidateCover(ctx, from) // the moved copy took the cover photo with it
	}

	return src, dst, nil
}

// AddCopyPhotos attaches uploaded photos to a copy and returns the updated game.
func (s *Service) AddCopyPhotos(ctx context.Context, id, copyID game.ID, photos []game.Photo) (*game.Game, error) {
	for _, p := range photos {
		if s.photos != nil && !s.photos.Has(p.ID) {
			return nil, fmt.Errorf("%w (%s)", ErrPhotoNotUploaded, p.ID)
		}
	}

	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.AddPhotos(copyID, photos, s.now())
		return err
	})
}

// UpdateCopyPhoto changes a photo's caption and returns the updated game.
func (s *Service) UpdateCopyPhoto(ctx context.Context, id, copyID game.ID, photoID game.PhotoID, caption string) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.UpdatePhoto(copyID, photoID, caption, s.now())
		return err
	})
}

// RemoveCopyPhoto removes a photo from a copy and returns the updated game. The file stays until
// the daily cleanup, so a mistake can be undone by uploading it again.
func (s *Service) RemoveCopyPhoto(ctx context.Context, id, copyID game.ID, photoID game.PhotoID) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.RemovePhoto(copyID, photoID, s.now())
		return err
	})
}

// ReorderCopyPhotos puts a copy's photos in a new order and returns the updated game.
func (s *Service) ReorderCopyPhotos(ctx context.Context, id, copyID game.ID, ids []game.PhotoID) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		_, err := g.ReorderPhotos(copyID, ids, s.now())
		return err
	})
}

// SetCoverPhoto makes one of the copies' photos the cover; an empty id stops using a photo.
func (s *Service) SetCoverPhoto(ctx context.Context, id game.ID, photoID game.PhotoID) (*game.Game, error) {
	return s.mutatePhotos(ctx, id, func(g *game.Game) error {
		info := g.Info()
		info.CoverPhoto = photoID
		_, err := g.UpdateInfo(info, s.now())

		return err
	})
}

// mutatePhotos is mutate, dropping the cached cover when the change touched the cover photo.
func (s *Service) mutatePhotos(ctx context.Context, id game.ID, fn func(*game.Game) error) (*game.Game, error) {
	var before game.PhotoID

	g, err := s.mutate(ctx, id, func(_ context.Context, g *game.Game) error {
		before = g.CoverPhoto()
		return fn(g)
	})
	if err == nil && g.CoverPhoto() != before {
		s.invalidateCover(ctx, id)
	}

	return g, err
}

// MarkRedeemedKeys marks revealed keys as redeemed when the game is already in the platform's library.
func (s *Service) MarkRedeemedKeys(ctx context.Context) (int, error) {
	total := 0
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		games, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		now := s.now()
		for _, g := range games {
			if n := g.MarkRedundantKeysRedeemed(now); n > 0 {
				total += n

				if err := s.games.Save(ctx, g); err != nil {
					return err
				}
			}
		}

		return nil
	})

	return total, err
}

// mutate loads a game, applies fn and saves it, in one transaction. fn gets the transaction's
// context, so what it reads (the field definitions) is read in the same transaction.
func (s *Service) mutate(ctx context.Context, id game.ID, fn func(context.Context, *game.Game) error) (*game.Game, error) {
	var g *game.Game

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if g, err = s.games.Get(ctx, id); err != nil {
			return err
		}

		if err := fn(ctx, g); err != nil {
			return err
		}

		return s.games.Save(ctx, g)
	})

	return g, err
}
