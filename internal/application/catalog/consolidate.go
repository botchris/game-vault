package catalog

import (
	"context"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// Consolidation is what merging imported copies into the catalog changed (see Service.Consolidate).
type Consolidation struct {
	game.ConsolidationResult
}

// AddTo adds the consolidation's counts and warnings to a scan or import report. The invalid
// systems are left to the caller: a source sends the same ones on every scan, a file only once.
func (c Consolidation) AddTo(r *source.SyncReport) {
	r.CopiesAdded = c.CopiesAdded
	r.CopiesUpdated = c.CopiesUpdated
	r.CopiesUnchanged = c.CopiesUnchanged
	r.GamesCreated = c.GamesCreated
	r.Warnings = append(r.Warnings, c.Warnings...)
}

// Consolidate merges imported copies into the catalog on behalf of sourceID (empty for a file),
// in one transaction: the games that changed are saved and the games left without copies by a
// withdrawal are deleted. copies runs first inside that transaction, so what it reads is current
// when the copies are merged (a source's exclusions, which the user may change during a scan).
// Once the transaction is committed, the cached covers of the editions that changed and of the
// deleted games are dropped.
func (s *Service) Consolidate(ctx context.Context, sourceID string,
	copies func(ctx context.Context) ([]game.ImportedCopy, error)) (Consolidation, error) {
	var (
		res   game.ConsolidationResult
		stale StaleCovers
	)

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		games, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		prints := make(map[game.ID]map[string]string, len(games))
		for _, g := range games {
			prints[g.ID()] = g.CoverFingerprints()
		}

		imported, err := copies(ctx)
		if err != nil {
			return err
		}

		res = game.NewConsolidator(games).Apply(sourceID, imported, s.now())
		stale = StaleCovers{} // what an earlier attempt of this function recorded was not saved

		for _, g := range res.Changed {
			if err := s.games.Save(ctx, g); err != nil {
				return err
			}

			stale.AddEditions(g.ID(), prints[g.ID()], g.CoverFingerprints())
		}

		// Games left without copies after the source withdrew them (they were never games).
		for _, id := range res.Emptied {
			if err := s.games.Delete(ctx, id); err != nil {
				return err
			}

			stale.AddGame(id)
		}

		return nil
	})
	if err != nil {
		return Consolidation{}, err
	}

	stale.Drop(ctx, s.covers)

	return Consolidation{res}, nil
}
