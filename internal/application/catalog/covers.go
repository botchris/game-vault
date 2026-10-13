package catalog

import (
	"context"

	"gamevault/internal/domain/game"
)

// CoverCache is the port used to drop cached covers when they may have changed. The catalog, the
// source scans and the file imports share it.
type CoverCache interface {
	// Invalidate drops the game's cached covers and details, so the next request resolves them again.
	Invalidate(ctx context.Context, id game.ID) error

	// InvalidateEdition drops the cached cover of the game's edition on system.
	InvalidateEdition(ctx context.Context, id game.ID, system string) error

	// DropLegacyCover drops the game's cover cached before editions, which the main edition would
	// otherwise adopt.
	DropLegacyCover(ctx context.Context, id game.ID) error
}

// NoCoverCache is a CoverCache for when no covers are cached: there is never anything to drop.
type NoCoverCache struct{}

// Invalidate implements CoverCache.
func (NoCoverCache) Invalidate(context.Context, game.ID) error { return nil }

// InvalidateEdition implements CoverCache.
func (NoCoverCache) InvalidateEdition(context.Context, game.ID, string) error { return nil }

// DropLegacyCover implements CoverCache.
func (NoCoverCache) DropLegacyCover(context.Context, game.ID) error { return nil }

// StaleCovers collects the cached covers a change makes stale, to drop once the change is saved.
// The zero value is ready to use.
type StaleCovers struct {
	editions map[game.ID][]string
	games    []game.ID
}

// AddEditions records the editions of a game whose cover fingerprints differ between before and
// after (see game.Game.CoverFingerprints). A new game has no fingerprints before, so all its
// editions count.
func (st *StaleCovers) AddEditions(id game.ID, before, after map[string]string) {
	changed := game.ChangedSystems(before, after)
	if len(changed) == 0 {
		return
	}

	if st.editions == nil {
		st.editions = map[game.ID][]string{}
	}

	st.editions[id] = append(st.editions[id], changed...)
}

// AddGame records a game whose every cached cover and its details are stale: it was deleted (its
// cached files would otherwise stay behind), or its links changed.
func (st *StaleCovers) AddGame(id game.ID) { st.games = append(st.games, id) }

// Drop drops the recorded covers from the cache, editions first. Best effort: a stale cached image
// is not worth failing the use case that saved the change.
func (st *StaleCovers) Drop(ctx context.Context, covers CoverCache) {
	for id, systems := range st.editions {
		for _, system := range systems {
			_ = covers.InvalidateEdition(ctx, id, system)
		}
	}

	for _, id := range st.games {
		_ = covers.Invalidate(ctx, id)
	}
}
