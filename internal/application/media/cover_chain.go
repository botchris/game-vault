package media

import (
	"context"
	"errors"
	"fmt"

	"gamevault/internal/application/providers"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// candidatesTried is how many of a provider's candidates are downloaded before the next provider is
// asked: the second is a fallback for a first image that fails to download, and the rest are near
// matches only the picker shows.
const candidatesTried = 2

// coverChain is the chain of cover providers, as the covers and the identification ask it.
type coverChain struct {
	reg   *providers.Registry
	impls map[provider.ID]CoverProvider
}

func newCoverChain(reg *providers.Registry) coverChain {
	return coverChain{
		reg:   reg,
		impls: implsOf[CoverProvider](reg, provider.KindCover),
	}
}

// inOrder returns the cover providers to ask: the enabled ones that are compiled in, in the
// user's order.
func (ch coverChain) inOrder(ctx context.Context) ([]providers.View, error) {
	views, err := ch.reg.Enabled(ctx, provider.KindCover)
	if err != nil {
		return nil, err
	}

	return withImpl(views, ch.impls), nil
}

// candidates asks one provider for candidates a user picks from (the cover picker, the scan page's
// suggestions): its candidates, a warning when it failed for this game, or limited when the
// problem is the provider's, which only sets its status (see Covers.CoverCandidates).
func (ch coverChain) candidates(ctx context.Context, p providers.View, q GameQuery) (
	candidates []CoverCandidate, warnings []string, limited bool) {
	c, err := ch.impls[p.ID()].Covers(ctx, q, p.Settings())
	ch.reg.ReportResult(p.ID(), err)

	switch {
	case err == nil:
		return c, nil, false
	case providers.ProviderLevel(err):
		// A provider-level problem (busy, allowance used up, rejected key) is the provider's
		// status, shown on the System and Providers pages, not a warning on every game.
		return nil, nil, true
	default:
		return nil, []string{fmt.Sprintf("%s: %v", p.Descriptor.Name, err)}, false
	}
}

// lookup is what the passes of one cover lookup share.
type lookup struct {
	// asked holds the providers already asked, so the fallback pass skips them.
	asked map[provider.ID]bool

	// limited is set when a provider asked to slow down: provider.ErrQuotaExhausted when every one
	// that did had used up its allowance, else provider.ErrRateLimited (the shorter wait).
	limited error

	// none holds the providers that answered without any candidate.
	none []provider.ID

	// virtual is set for a virtual PC edition, which is the store's art or nothing: it borrows no
	// base game's.
	virtual bool

	// borrowTried reports that the add-on already looked for its base game's cover.
	borrowTried bool
}

func newLookup() *lookup { return &lookup{asked: map[provider.ID]bool{}} }

// canBorrow reports whether the edition may still borrow its base game's cover.
func (l *lookup) canBorrow() bool { return !l.virtual && !l.borrowTried }

// fromProviders asks the cover chain for an edition's cover: the first pass, then (for an add-on)
// the base game's cover, then the fallback pass. The lookup it returns says what the providers
// answered.
func (c *Covers) fromProviders(ctx context.Context, g *game.Game, ref GameRef, system string, virtual bool) (Image, *lookup, error) {
	q := QueryFor(g, system)
	q.LinksOnly = virtual
	look := newLookup()
	look.virtual = virtual

	// Providers that already said "no cover" while another one was limited are not asked again yet.
	for _, id := range c.misses.fresh(g.ID(), system, c.now()) {
		look.asked[id] = true
	}

	img, err := c.coverFromChain(ctx, ref, system, q, look)
	if !errors.Is(err, ErrNoCover) {
		return img, look, err
	}

	// An add-on borrows its base game's cover before the fallback pass, so a DLC or soundtrack key
	// does not spend title searches first. Not after a provider asked to slow down: it may have had
	// the add-on's own art.
	if look.limited == nil && look.canBorrow() && game.IsAddOnTitle(g.Title()) {
		if img, ok := c.addOnCover(ctx, g, system); ok {
			return img, look, nil
		}

		look.borrowTried = true
	}

	// A virtual PC edition asks the PC stores only: their art is the point, and the fallback pass
	// would spend TheGamesDB's quota and title searches on a box the console editions only borrow.
	if virtual {
		return Image{}, look, err
	}

	// No provider had art for this edition in the first pass (a game only on Battle.net, a console
	// edition of a Steam game, a key no store links…): ask the ones that kept out of it, which
	// search by title or have one cover per game. Providers already asked are skipped, so a game
	// without store links, which TheGamesDB already got in the first pass, costs it nothing more.
	q.Fallback = true
	img, err = c.coverFromChain(ctx, ref, system, q, look)

	return img, look, err
}

// coverFromChain asks the enabled cover providers that apply to q, in order, and caches the first
// image that downloads. Providers already asked in look are skipped; the ones asked are added to it.
func (c *Covers) coverFromChain(ctx context.Context, ref GameRef, system string, q GameQuery, look *lookup) (Image, error) {
	chain, err := c.chain.inOrder(ctx)
	if err != nil {
		return Image{}, err
	}

	for _, p := range chain {
		impl := c.chain.impls[p.ID()]
		if look.asked[p.ID()] || !impl.Applies(q) {
			continue
		}

		look.asked[p.ID()] = true

		candidates, err := impl.Covers(ctx, q, p.Settings())
		c.reg.ReportResult(p.ID(), err)

		if errors.Is(err, provider.ErrRateLimited) {
			look.noteLimited(err)
			c.log.Info("cover provider unavailable, asking again later", "provider", p.ID(), "game", ref.Title, "system", system, "error", err)

			continue
		}

		if err != nil {
			c.log.Warn("cover provider failed", "provider", p.ID(), "game", ref.Title, "error", err)
			continue
		}

		if len(candidates) == 0 {
			look.none = append(look.none, p.ID())
		}

		for _, cand := range candidates[:min(len(candidates), candidatesTried)] {
			if img, err := c.fetchAndCache(ctx, ref, system, cand.URL); err == nil {
				c.log.Debug("cover cached", "game", ref.Title, "provider", p.ID(), "url", cand.URL)
				return img, nil
			}
		}
	}

	return Image{}, ErrNoCover
}

// noteLimited records that a provider asked to slow down with err: the shorter wait wins, so a
// used-up allowance is only kept when no provider was merely busy.
func (l *lookup) noteLimited(err error) {
	switch {
	case !errors.Is(err, provider.ErrQuotaExhausted):
		l.limited = provider.ErrRateLimited
	case l.limited == nil:
		l.limited = provider.ErrQuotaExhausted
	}
}

// fetchAndCache downloads an image and stores it as the edition's cover.
func (c *Covers) fetchAndCache(ctx context.Context, g GameRef, system, url string) (Image, error) {
	img, err := c.downloads.Fetch(ctx, url)
	if err != nil {
		return Image{}, err
	}

	if err := c.store.PutCover(g, system, img); err != nil {
		return Image{}, fmt.Errorf("caching cover: %w", err)
	}

	return img, nil
}

// implsOf returns the providers of kind in reg that implement T, by id. The plugins declare each
// provider in the list of its kind, typed, so every one of them does (plugintest checks the kinds).
func implsOf[T any](reg *providers.Registry, kind provider.Kind) map[provider.ID]T {
	out := map[provider.ID]T{}

	for _, p := range reg.All() {
		d := p.Descriptor()
		if impl, ok := p.(T); ok && d.Kind == kind {
			out[d.ID] = impl
		}
	}

	return out
}

// withImpl keeps the views whose provider is in impls.
func withImpl[T any](views []providers.View, impls map[provider.ID]T) []providers.View {
	out := views[:0]

	for _, v := range views {
		if _, ok := impls[v.ID()]; ok {
			out = append(out, v)
		}
	}

	return out
}
