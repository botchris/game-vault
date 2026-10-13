package media

import (
	"context"
	"errors"

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

		// Markers of systems the game no longer has, and the one from before editions, are leftovers.
		pruned, err := c.store.PruneCoverMarkers(g.ID(), systems)
		if err != nil {
			return n, err
		}

		dropped := pruned > 0

		for _, system := range systems {
			if _, missing := c.store.CoverMissingSince(g.ID(), system); !missing {
				continue
			}

			// Only the marker goes: deleting the cover would also delete the one from before
			// editions, which the main edition has not adopted yet.
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
// sync.CoverCache): its chosen cover or its copies changed. For the main edition (or a game without
// copies) it also drops the cover cached before editions, which the main edition would otherwise
// adopt instead of resolving the new one; other editions leave it for the main edition to adopt.
func (c *Covers) InvalidateEdition(ctx context.Context, id game.ID, system string) error {
	c.misses.forgetEdition(id, system)

	if err := c.store.DeleteCover(id, system); err != nil {
		return err
	}
	// Without a cover from before editions there is nothing more to drop: every new game's first
	// scan ends here, without reading the game.
	if !c.store.HasLegacyCover(id) {
		return nil
	}

	g, err := c.games.Get(ctx, id)
	if errors.Is(err, game.ErrGameNotFound) {
		return c.store.DeleteLegacyCover(id) // nothing left to adopt it
	}

	if err != nil {
		return err
	}

	if system == "" || system == g.MainEdition().System {
		return c.store.DeleteLegacyCover(id)
	}

	return nil
}

// DropLegacyCover drops the game's cover cached before editions (implements catalog.CoverCache): the
// main edition changed, and the new one must not adopt the old one's image.
func (c *Covers) DropLegacyCover(_ context.Context, id game.ID) error {
	return c.store.DeleteLegacyCover(id)
}

// LegacyCoverIDs returns the games that still have a cover cached before editions, or its "no cover
// found" marker, for the one-time start-up upgrade (SettleLegacyCover).
func (c *Covers) LegacyCoverIDs() []game.ID {
	return c.store.LegacyCoverIDs()
}

// SettleLegacyCover does now, for the one-time start-up upgrade, what the main edition's next
// resolution would do with the game's cover cached before editions: it adopts it (true) or removes
// it (false). It removes it as well when nothing would ever read it again: the game is gone, the
// main edition already has a cover of its own (the cache answers before the file is looked at),
// or only the "no cover found" marker is left (resolving the main edition prunes it).
func (c *Covers) SettleLegacyCover(ctx context.Context, id game.ID) (adopted bool, err error) {
	g, err := c.games.Get(ctx, id)
	if errors.Is(err, game.ErrGameNotFound) {
		return false, c.store.DeleteLegacyCover(id)
	}

	if err != nil {
		return false, err
	}

	main := g.MainEdition()

	_, cached, err := c.store.GetCover(id, main.System)
	if err != nil {
		return false, err
	}

	if !cached {
		_, adopted, err = c.adoptLegacyCover(g, GameRef{g.ID(), g.Title()}, main)
		if err != nil || adopted {
			return adopted, err
		}
	}

	return false, c.store.DeleteLegacyCover(id)
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
