package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"

	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// coverLogicChanged is when the way covers are found last improved (e.g. add-ons borrowing their
// base game's cover). "No cover found" markers older than this are ignored, so games are retried
// with the new logic instead of waiting for the marker to expire. Bump it with such changes.
var coverLogicChanged = time.Date(2026, 10, 13, 10, 50, 0, 0, time.UTC) // TheGamesDB searches edition titles by the game's name

// missingCoverRetry is how long a "no cover found" marker is trusted: long enough not to spend
// providers' requests on every view, short enough that art published since is found.
const missingCoverRetry = 7 * 24 * time.Hour

// Covers resolves the covers of the games' editions through the cover chain, caches them with the
// game's other images, proposes candidates for the user to choose from, and drops cached covers when
// what they depend on changes (it implements catalog.CoverCache and sync.CoverCache).
type Covers struct {
	games     game.Repository
	reg       *providers.Registry
	chain     coverChain
	store     AssetStore
	photos    PhotoStore
	details   DetailsStore
	downloads *Downloads
	autoLinks linkQueue
	searchers []LinkSearcher // the stores' catalog searches, for add-ons' base games
	now       port.Clock
	log       *slog.Logger
	group     singleflight.Group

	// misses remembers providers' "no cover" answers that could not become a marker.
	misses *misses
}

// NewCovers builds the covers service over the cover providers in reg. Options: WithPhotoStore (a
// chosen photo cover), WithDetailsStore (dropped with a deleted game's media), WithClock,
// WithLogger. Built alone, it queues no game for automatic links: New wires the auto-linker.
func NewCovers(games game.Repository, reg *providers.Registry, store AssetStore, downloads *Downloads, opts ...CoverOption) *Covers {
	_, searchers := linkStoresOf(reg)

	c := &Covers{
		games:     games,
		reg:       reg,
		chain:     newCoverChain(reg),
		store:     store,
		photos:    noPhotos{},
		details:   noDetails{},
		downloads: downloads,
		autoLinks: noAutoLinks{},
		searchers: searchers,
		now:       defaultClock,
		log:       defaultLogger,
		misses:    &misses{},
	}
	for _, opt := range opts {
		opt.applyCovers(c)
	}

	return c
}

// Cover returns the cover of the game's main edition (see game.Game.MainEdition).
func (c *Covers) Cover(ctx context.Context, id game.ID) (Image, error) {
	g, err := c.games.Get(ctx, id)
	if err != nil {
		return Image{}, err
	}

	return c.EditionCover(ctx, id, g.MainEdition().System)
}

// EditionCover returns the cover of the game's edition on system, resolving and caching it on first
// use. The empty system is the cover of a game without copies.
func (c *Covers) EditionCover(ctx context.Context, id game.ID, system string) (Image, error) {
	if img, ok, err := c.store.GetCover(id, system); err != nil || ok {
		return img, err
	}

	if since, ok := c.store.CoverMissingSince(id, system); ok && since.After(coverLogicChanged) && c.now().Sub(since) < missingCoverRetry {
		return Image{}, ErrNoCover
	}
	// Concurrent requests for the same cover share one lookup.
	v, err, _ := c.group.Do(string(id)+"\x00"+system, func() (any, error) {
		return c.resolve(context.WithoutCancel(ctx), id, system)
	})
	if err != nil {
		return Image{}, err
	}

	return v.(Image), nil
}

// resolve finds an edition's cover (see resolveEdition). Once the main edition has resolved (a
// cover, or a miss remembered), the game's leftover "no cover found" markers go: those of systems
// it no longer has and the one from before editions, which nothing reads any more.
func (c *Covers) resolve(ctx context.Context, id game.ID, system string) (Image, error) {
	g, err := c.games.Get(ctx, id)
	if err != nil {
		return Image{}, err
	}

	img, err := c.resolveEdition(ctx, g, system)

	resolved := err == nil || (errors.Is(err, ErrNoCover) && !errors.Is(err, provider.ErrRateLimited))
	if resolved && system == g.MainEdition().System {
		if _, perr := c.store.PruneCoverMarkers(id, editionSystems(g)); perr != nil {
			c.log.Warn("pruning old cover markers", "game", g.Title(), "error", perr)
		}
	}

	return img, err
}

// editionSystems returns the systems of g's editions (and PC when it borrows a PC box, so that box's
// "no cover found" marker is kept), or the empty system of a game without copies.
func editionSystems(g *game.Game) []string {
	systems := g.Systems()
	if len(systems) == 0 {
		return []string{""}
	}

	if g.BorrowsPCBox() {
		systems = append(systems, game.SystemPC)
	}

	return systems
}

// resolveEdition finds the cover of g's edition on system, in this order:
//  1. the image decided for the edition: the cover cached before editions (main edition only), the
//     chosen photo, the chosen URL (chosen);
//  2. the provider chain, its fallback pass, and the base game's cover for an add-on in between
//     (fromProviders);
//  3. when a provider asked to slow down, nothing this time: the next request asks again;
//  4. the base game's cover for the same system (add-ons), if not tried yet;
//  5. else the edition is marked missing (rememberMissing).
func (c *Covers) resolveEdition(ctx context.Context, g *game.Game, system string) (Image, error) {
	edition, virtual, ok := coverEdition(g, system)
	if !ok {
		return Image{}, ErrNoCover // no copy on that system (any more)
	}

	ref := GameRef{g.ID(), g.Title()}

	if img, ok, err := c.chosen(ctx, g, ref, edition); err != nil || ok {
		return img, err
	}

	img, look, err := c.fromProviders(ctx, g, ref, system, virtual)
	if err == nil || !errors.Is(err, ErrNoCover) {
		return img, err
	}

	// A provider that asked to slow down said nothing about the game: the edition has no cover this
	// time, is not remembered as missing and does not borrow another's box, so the next request
	// asks again.
	if look.limited != nil {
		// Those that did answer "no cover" are remembered, so the next views only ask the limited
		// ones until the edition can be marked missing.
		c.misses.remember(g.ID(), system, look.none, c.now())

		// The provider's trouble comes first, so its kind (Unavailable) is the error's, not
		// ErrNoCover's: errkind.Of reads the first match in depth-first order.
		return Image{}, fmt.Errorf("%w: %w", look.limited, ErrNoCover)
	}

	// Add-ons (DLC, soundtracks…) rarely have art of their own: borrow the base game's.
	if look.canBorrow() {
		if img, ok := c.addOnCover(ctx, g, system); ok {
			return img, nil
		}
	}

	c.rememberMissing(g, ref, system, look)

	return Image{}, ErrNoCover
}

// coverEdition returns g's edition on system, and whether it is the virtual PC edition: a
// console-only game linked to a PC store has one, the store's PC box, which its console editions
// borrow as a last resort (labeled as a PC box). It has no copies, so no physical platform reaches
// the providers, and no chosen cover.
func coverEdition(g *game.Game, system string) (edition game.Edition, virtual, ok bool) {
	if system == "" && len(g.Copies()) == 0 {
		return g.MainEdition(), false, true // a game without copies: its cover is the one chosen for the game
	}

	if edition, ok := g.Edition(system); ok {
		return edition, false, true
	}

	if system == game.SystemPC && g.BorrowsPCBox() {
		return game.Edition{System: game.SystemPC}, true, true
	}

	return game.Edition{}, false, false
}

// chosen returns the image already decided for the edition, and whether there is one: a photo of
// the user's own copy, then a cover the user picked or pasted. A chosen image that cannot be had any
// more falls back to the providers.
func (c *Covers) chosen(ctx context.Context, g *game.Game, ref GameRef, edition game.Edition) (Image, bool, error) {
	system := edition.System

	// A photo of the user's own copy chosen as the cover wins over everything.
	if id := edition.Cover.Photo; id != "" {
		img, err := c.photos.Open(id, false)
		if err == nil {
			if err := c.store.PutCover(ref, system, img); err != nil {
				return Image{}, false, fmt.Errorf("caching cover: %w", err)
			}

			return img, true, nil
		}

		c.log.Warn("cover photo unavailable, falling back", "game", g.Title(), "system", system, "error", err)
	}

	// A cover the user picked or pasted comes next.
	if u := edition.Cover.URL; u != "" {
		img, err := c.fetchAndCache(ctx, ref, system, u)
		if err == nil {
			return img, true, nil
		}

		c.log.Warn("chosen cover failed, falling back to providers", "game", g.Title(), "system", system, "url", u)
	}

	return Image{}, false, nil
}

// rememberMissing marks the edition without cover when a provider was asked, and queues the game for
// the auto-linker: a store link found now would give it the store's box, and links found drop this
// marker.
func (c *Covers) rememberMissing(g *game.Game, ref GameRef, system string, look *lookup) {
	if len(look.asked) > 0 {
		c.log.Info("no cover found", "game", g.Title(), "system", system)

		if err := c.store.MarkCoverMissing(ref, system, c.now()); err != nil {
			c.log.Warn("marking missing cover", "error", err)
		}
	}

	c.autoLinks.Enqueue(g)
}
