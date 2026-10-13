package media

import (
	"context"

	"gamevault/internal/domain/game"
)

// RefreshCovers drops cached covers so they are resolved again on next view. With missingOnly, only
// the editions' "not found" markers are dropped (cheap: real covers stay cached). It returns how many
// games were affected.
func (c *Covers) RefreshCovers(ctx context.Context, missingOnly bool) (int, error) {
	c.misses.forgetAll()

	games, err := c.games.List(ctx)
	if err != nil {
		return 0, err
	}

	n := 0

	for _, g := range games {
		if !missingOnly {
			if err := c.store.DeleteCovers(g.ID()); err != nil {
				return n, err
			}

			n++

			continue
		}

		systems := editionSystems(g)

		// Markers of systems the game no longer has are leftovers.
		pruned, err := c.store.PruneCoverMarkers(g.ID(), systems)
		if err != nil {
			return n, err
		}

		dropped := pruned > 0

		for _, system := range systems {
			if _, missing := c.store.CoverMissingSince(g.ID(), system); !missing {
				continue
			}

			// Only the marker goes: this is the cheap refresh, which keeps every stored cover.
			if err := c.store.ClearCoverMissing(g.ID(), system); err != nil {
				return n, err
			}

			dropped = true
		}

		if dropped {
			n++
		}
	}

	return n, nil
}

// InvalidateEdition drops the cached cover of a game's edition (implements catalog.CoverCache and
// sync.CoverCache): its chosen cover or its copies changed.
func (c *Covers) InvalidateEdition(_ context.Context, id game.ID, system string) error {
	c.misses.forgetEdition(id, system)

	return c.store.DeleteCover(id, system)
}

// Invalidate drops the game's cover, details and downloaded images (implements catalog.CoverCache):
// they depend on the custom cover URL and the game's links, and must go when the game is deleted.
// It deletes all of the game's media, not only its covers (the sheet and its images too), and lives
// here because the catalog's one CoverCache port is the only call it gets for that: the game's
// folder holds covers and sheet images together.
func (c *Covers) Invalidate(ctx context.Context, id game.ID) error {
	c.misses.forgetGame(id)

	if err := c.details.Delete(ctx, id); err != nil {
		return err
	}

	return c.store.DeleteAll(id)
}

// forgetMissing forgets the editions' "no cover found" results, so they are looked up again: the
// cover chain changed (see providers.Registry.OnCoversChanged).
func (c *Covers) forgetMissing(ctx context.Context) error {
	_, err := c.RefreshCovers(ctx, true)
	return err
}
