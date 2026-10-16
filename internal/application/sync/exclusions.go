package sync

import (
	"context"
	"errors"
	"slices"

	"gamevault/internal/application/catalog"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

var (
	// ErrNotImported means the copy was added by hand: there is nothing to stop importing.
	ErrNotImported = errkind.New(errkind.Precondition, "this copy was added by hand: delete it instead")

	// ErrNotExcluded means the item is not on the source's removed list.
	ErrNotExcluded = errkind.New(errkind.NotFound, "this item is not on the source's removed list")
)

// ExcludeCopy removes a copy a source imported and records it on the source, so later scans skip
// it. A game left without copies is deleted; the result is then nil.
func (s *Service) ExcludeCopy(ctx context.Context, gameID, copyID game.ID) (*game.Game, error) {
	var (
		out    *game.Game
		before map[string]string
		gone   bool
	)

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		g, err := s.games.Get(ctx, gameID)
		if err != nil {
			return err
		}

		i := slices.IndexFunc(g.Copies(), func(c game.Copy) bool { return c.ID == copyID })
		if i < 0 {
			return game.ErrCopyNotFound
		}

		c := g.Copies()[i]
		if c.SourceID == "" || c.ExternalID == "" {
			return ErrNotImported
		}

		src, err := s.sources.Get(ctx, source.ID(c.SourceID))
		if err != nil {
			return err
		}

		now := s.now()
		if err := src.Exclude(source.Exclusion{
			ExternalID: c.ExternalID,
			Title:      g.Title(),
			At:         now,
		}); err != nil {
			return err
		}

		if err := s.sources.Save(ctx, src); err != nil {
			return err
		}

		before = g.CoverFingerprints()
		if _, err := g.RemoveCopy(copyID, now); err != nil {
			return err
		}

		if len(g.Copies()) == 0 {
			gone = true
			return s.games.Delete(ctx, gameID)
		}

		out = g

		return s.games.Save(ctx, g)
	})
	if err != nil {
		return nil, err
	}

	var stale catalog.StaleCovers
	if gone {
		stale.AddGame(gameID)
	} else {
		stale.AddEditions(gameID, before, out.CoverFingerprints())
	}

	s.catalog.DropCovers(ctx, &stale)

	return out, nil
}

// DeleteGame deletes a game and records each copy a source imported on that source's removed list,
// so later scans do not bring the game back. Copies added by hand are simply deleted with it, and so
// is a copy whose source no longer exists.
func (s *Service) DeleteGame(ctx context.Context, id game.ID) error {
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		g, err := s.games.Get(ctx, id)
		if err != nil {
			return err
		}

		now := s.now()
		changed := map[source.ID]*source.Source{}

		for _, c := range g.Copies() {
			if c.SourceID == "" || c.ExternalID == "" {
				continue
			}

			sid := source.ID(c.SourceID)

			src, ok := changed[sid]
			if !ok {
				if src, err = s.sources.Get(ctx, sid); errors.Is(err, source.ErrNotFound) {
					continue
				} else if err != nil {
					return err
				}

				changed[sid] = src
			}

			if err := src.Exclude(source.Exclusion{
				ExternalID: c.ExternalID,
				Title:      g.Title(),
				At:         now,
			}); err != nil {
				return err
			}
		}

		for _, src := range changed {
			if err := s.sources.Save(ctx, src); err != nil {
				return err
			}
		}

		return s.games.Delete(ctx, id)
	})
	if err != nil {
		return err
	}

	var stale catalog.StaleCovers
	stale.AddGame(id)
	s.catalog.DropCovers(ctx, &stale)

	return nil
}

// IncludeCopy takes an item off a source's removed list: the next scan imports it again.
func (s *Service) IncludeCopy(ctx context.Context, id source.ID, externalID string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		src, err := s.sources.Get(ctx, id)
		if err != nil {
			return err
		}

		if !src.Include(externalID) {
			return ErrNotExcluded
		}

		return s.sources.Save(ctx, src)
	})
}

// skipExcluded turns the imported items the user removed from src into withdrawn copies: the
// consolidation then removes an old copy of them and never adds a new one. It returns how many
// items it skipped.
func skipExcluded(src *source.Source, copies []game.ImportedCopy) ([]game.ImportedCopy, int) {
	out := make([]game.ImportedCopy, len(copies))
	skipped := 0

	for i, c := range copies {
		out[i] = c

		if c.Withdrawn || !src.Excludes(c.ExternalID) {
			continue
		}

		out[i].Withdrawn = true
		skipped++
	}

	return out, skipped
}
