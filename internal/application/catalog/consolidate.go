package catalog

import (
	"context"

	"gamevault/internal/domain/event"
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

// Added returns what a collection.added event tells about the consolidation: the games it created,
// in order, and the copies it added to games that were already there. It reports false when no game
// was created: copies joining known games alone are not news.
func (c Consolidation) Added(origin event.Origin, originName string) (event.CollectionAdded, bool) {
	saved := make(map[game.ID]*game.Game, len(c.Changed))
	for _, g := range c.Changed {
		saved[g.ID()] = g
	}

	added := event.CollectionAdded{
		Origin:      origin,
		OriginName:  originName,
		Games:       make([]event.GameRef, 0, len(c.Created)),
		CopiesAdded: c.CopiesAdded,
	}

	for _, id := range c.Created {
		g, ok := saved[id]
		if !ok {
			continue // a withdrawal later in the same import emptied it, and it was deleted
		}

		added.Games = append(added.Games, event.RefOf(g))
		added.CopiesAdded -= len(g.Copies()) // a new game's copies all came with it
	}

	added.CopiesAdded = max(added.CopiesAdded, 0)

	return added, len(added.Games) > 0
}

// Consolidate merges imported copies into the catalog on behalf of sourceID (empty for a file),
// in one transaction. It lists the games, then calls copies, inside that transaction, so what
// copies reads is current when they are merged (a source's exclusions, which the user may change
// during a scan); then it saves the games that changed and deletes the games a withdrawal left
// without copies. On success the cached covers of the editions that changed and of the deleted
// games are dropped: after the commit, unless ctx already carries a transaction (WithinTx then
// joins it), in which case they are dropped before the outer transaction commits. On failure
// nothing is saved, no cover is dropped and the Consolidation is zero.
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
