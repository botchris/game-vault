package media

import (
	"context"
	"slices"
	"strings"

	"gamevault/internal/domain/game"
)

// Limits of the search for an add-on's base game.
const (
	// minBaseKeyLen is the shortest match key a base game may have: shorter titles ("Go") prefix
	// too many add-ons by chance.
	minBaseKeyLen = 3

	// maxBaseQueries is how many titles a store is searched by for the base game: each is one
	// request to a rate-limited store.
	maxBaseQueries = 4
)

// isWordPrefix reports whether every word of short starts long, and long has more words.
func isWordPrefix(short, long string) bool {
	return short != "" && len(short) < len(long) && strings.HasPrefix(long, short+" ")
}

// baseGameOf finds the catalog game an add-on belongs to: the game whose title is the longest
// word prefix of the add-on's ("GRIP: Combat Racing" for "GRIP: Combat Racing Artifex DLC").
func baseGameOf(add *game.Game, catalog []*game.Game) *game.Game {
	key := game.MatchKey(add.Title())

	var best *game.Game

	bestLen := 0

	for _, g := range catalog {
		k := game.MatchKey(g.Title())
		if g.ID() == add.ID() || len(k) < minBaseKeyLen || len(k) <= bestLen || !isWordPrefix(k, key) {
			continue
		}

		best, bestLen = g, len(k)
	}

	return best
}

// baseQueries are titles to search the base game by in a store, most specific first: the words
// before the add-on marker, then with trailing words dropped, then the title cut at a separator.
func baseQueries(title string) []string {
	var out []string

	add := func(q string) {
		q = strings.Trim(strings.TrimSpace(q), ":-–—, ")
		if len(q) >= minSearchLen && !slices.Contains(out, q) && len(out) < maxBaseQueries {
			out = append(out, q)
		}
	}

	words := strings.Fields(game.StripAddOnMarker(title))
	for n := len(words); n >= 1; n-- {
		add(strings.Join(words[:n], " "))
	}

	if i := strings.LastIndexAny(title, ":-–—"); i > 2 {
		add(title[:i])
	}

	return out
}

// addOnCover finds a cover for an add-on's edition nobody has art for: the base game's cover for
// the same system, from the catalog when you own the base game on it, else from a store after
// finding the base game in its catalog by title.
func (c *Covers) addOnCover(ctx context.Context, g *game.Game, system string) (Image, bool) {
	if !game.IsAddOnTitle(g.Title()) {
		return Image{}, false
	}

	ref := GameRef{g.ID(), g.Title()}
	if catalog, err := c.games.List(ctx); err == nil {
		if base := baseGameOf(g, catalog); base != nil {
			if img, ok := c.baseEditionCover(ctx, base, system); ok {
				if err := c.store.PutCover(ref, system, img); err == nil {
					c.log.Info("add-on cover taken from its base game", "game", g.Title(), "base", base.Title(), "system", system)
					return img, true
				}
			}
		}
	}

	// Only stores' providers answer the search below, and they have art for PC editions only: a
	// console edition would spend requests to rate-limited stores for nothing.
	if !(GameQuery{System: system}).ForPC() {
		return Image{}, false
	}

	for _, ls := range c.searchers {
		if img, ok := c.baseCoverIn(ctx, ls, g, ref, system); ok {
			return img, true
		}
	}

	return Image{}, false
}

// baseEditionCover is the base game's cover on system (its main edition's when system is empty),
// when the base game has a copy on it.
func (c *Covers) baseEditionCover(ctx context.Context, base *game.Game, system string) (Image, bool) {
	if system == "" {
		system = base.MainEdition().System
	}

	if _, ok := base.Edition(system); !ok && system != "" {
		return Image{}, false
	}

	img, err := c.EditionCover(ctx, base.ID(), system)

	return img, err == nil
}

// baseCoverIn looks for the base game of the add-on g in one store's catalog.
func (c *Covers) baseCoverIn(ctx context.Context, ls LinkSearcher, g *game.Game, ref GameRef, system string) (Image, bool) {
	store := ls.LinkStore()
	key := game.MatchKey(g.Title())

	for _, q := range baseQueries(g.Title()) {
		matches, err := ls.SearchLinks(ctx, q)
		if err != nil {
			return Image{}, false
		}

		for _, m := range matches {
			if m.ID == g.Links()[store.Key] || !isWordPrefix(game.MatchKey(m.Name), key) {
				continue
			}
			// Linked to the store, the query only reaches providers that know it (no quota spent).
			cq := GameQuery{
				Title:  m.Name,
				System: system,
				Links:  game.Links{store.Key: m.ID},
			}
			if img, err := c.coverFromChain(ctx, ref, system, cq, newLookup()); err == nil {
				c.log.Info("add-on cover taken from its base game", "game", g.Title(), "base", m.Name, "store", store.Name)
				return img, true
			}
		}
	}

	return Image{}, false
}
