// Package catalog implements the use cases to browse and edit games and their copies.
package catalog

import (
	"context"
	"errors"
	"slices"

	"gamevault/internal/application/port"
	"gamevault/internal/domain/game"
)

// CoverCache is the port used to drop a game's cached cover image when it may have changed.
type CoverCache interface {
	Invalidate(ctx context.Context, id game.ID) error
}

// Service exposes the catalog use cases.
type Service struct {
	games  game.Repository
	tx     port.TxManager
	now    port.Clock
	covers CoverCache
}

func NewService(games game.Repository, tx port.TxManager, now port.Clock, covers CoverCache) *Service {
	return &Service{games: games, tx: tx, now: now, covers: covers}
}

// invalidateCover is best effort: a stale cached image is not worth failing the use case.
func (s *Service) invalidateCover(ctx context.Context, id game.ID) {
	if s.covers != nil {
		_ = s.covers.Invalidate(ctx, id)
	}
}

func (s *Service) ListGames(ctx context.Context) ([]*game.Game, error) {
	return s.games.List(ctx)
}

func (s *Service) GetGame(ctx context.Context, id game.ID) (*game.Game, error) {
	return s.games.Get(ctx, id)
}

// CreateGame registers a game, optionally with some copies.
func (s *Service) CreateGame(ctx context.Context, info game.Info, copies []game.CopyDetails) (*game.Game, error) {
	now := s.now()
	g, err := game.New(info.Title, now)
	if err != nil {
		return nil, err
	}
	if _, err := g.UpdateInfo(info, now); err != nil {
		return nil, err
	}
	for _, d := range copies {
		if _, err := g.AddCopy(d, now); err != nil {
			return nil, err
		}
	}
	return g, s.games.Save(ctx, g)
}

func (s *Service) UpdateGame(ctx context.Context, id game.ID, info game.Info) (*game.Game, error) {
	var coverChanged bool
	g, err := s.mutate(ctx, id, func(g *game.Game) error {
		var err error
		coverChanged, err = g.UpdateInfo(info, s.now())
		return err
	})
	if err == nil && coverChanged {
		s.invalidateCover(ctx, id)
	}
	return g, err
}

func (s *Service) DeleteGame(ctx context.Context, id game.ID) error {
	if err := s.games.Delete(ctx, id); err != nil {
		return err
	}
	s.invalidateCover(ctx, id)
	return nil
}

func (s *Service) AddCopy(ctx context.Context, id game.ID, d game.CopyDetails) (*game.Game, error) {
	return s.mutate(ctx, id, func(g *game.Game) error {
		_, err := g.AddCopy(d, s.now())
		return err
	})
}

func (s *Service) UpdateCopy(ctx context.Context, id, copyID game.ID, d game.CopyDetails) (*game.Game, error) {
	return s.mutate(ctx, id, func(g *game.Game) error {
		_, err := g.UpdateCopy(copyID, d, s.now())
		return err
	})
}

func (s *Service) DeleteCopy(ctx context.Context, id, copyID game.ID) (*game.Game, error) {
	return s.mutate(ctx, id, func(g *game.Game) error {
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
		s.invalidateCover(ctx, target) // the target may have adopted a Steam AppID or cover
		for _, id := range sources {
			s.invalidateCover(ctx, id)
		}
	}
	return merged, err
}

// MoveCopy moves a copy to another game, or to a new game titled newTitle when target is empty.
// The source game is deleted if it ends up without copies; in that case the first result is nil.
func (s *Service) MoveCopy(ctx context.Context, from, copyID, target game.ID, newTitle string) (*game.Game, *game.Game, error) {
	var src, dst *game.Game
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if src, err = s.games.Get(ctx, from); err != nil {
			return err
		}
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
	return src, dst, nil
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

func (s *Service) mutate(ctx context.Context, id game.ID, fn func(*game.Game) error) (*game.Game, error) {
	var g *game.Game
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if g, err = s.games.Get(ctx, id); err != nil {
			return err
		}
		if err := fn(g); err != nil {
			return err
		}
		return s.games.Save(ctx, g)
	})
	return g, err
}
