package media

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// reAddOn recognizes titles of extra content: "GRIP: Combat Racing Artifex DLC", "X - Soundtrack"….
var reAddOn = regexp.MustCompile(`(?i)\b(dlcs?|season\s+pass(es)?|expansions?(\s+pass)?|soundtracks?|ost|add-?on|content\s+pack|bonus\s+content|costumes?|skins?\s+pack|art\s*book|upgrade\s+pack|character\s+pack|map\s+pack)\b`)

// IsAddOn reports whether a title looks like extra content for another game. Stores often have no
// art of their own for these (or the AppID Humble gives does not exist on Steam).
func IsAddOn(title string) bool { return reAddOn.MatchString(title) }

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
		if g.ID() == add.ID() || len(k) < 3 || len(k) <= bestLen || !isWordPrefix(k, key) {
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
		if len(q) >= 2 && !slices.Contains(out, q) && len(out) < 4 {
			out = append(out, q)
		}
	}

	before := title
	if loc := reAddOn.FindStringIndex(title); loc != nil {
		before = title[:loc[0]]
	}

	words := strings.Fields(before)
	for n := len(words); n >= 1; n-- {
		add(strings.Join(words[:n], " "))
	}

	if i := strings.LastIndexAny(title, ":-–—"); i > 2 {
		add(title[:i])
	}

	return out
}

// addOnCover finds a cover for an add-on nobody has art for: the base game's cover, from the
// catalog when you own it, else from a store after finding the base game in its catalog by title.
func (s *Service) addOnCover(ctx context.Context, g *game.Game) (Image, bool) {
	if !IsAddOn(g.Title()) {
		return Image{}, false
	}

	ref := GameRef{g.ID(), g.Title()}
	if catalog, err := s.games.List(ctx); err == nil {
		if base := baseGameOf(g, catalog); base != nil {
			if img, err := s.Cover(ctx, base.ID()); err == nil {
				if err := s.store.PutCover(ref, img); err == nil {
					s.log.Info("add-on cover taken from its base game", "game", g.Title(), "base", base.Title())
					return img, true
				}
			}
		}
	}

	for _, ls := range s.searchers {
		if img, ok := s.baseCoverIn(ctx, ls, g, ref); ok {
			return img, true
		}
	}

	return Image{}, false
}

// baseCoverIn looks for the base game of the add-on g in one store's catalog.
func (s *Service) baseCoverIn(ctx context.Context, ls LinkSearcher, g *game.Game, ref GameRef) (Image, bool) {
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
			cq := CoverQuery{
				Title: m.Name,
				Links: game.Links{store.Key: m.ID},
			}
			if img, err := s.coverFromChain(ctx, ref, cq, map[provider.ID]bool{}); err == nil {
				s.log.Info("add-on cover taken from its base game", "game", g.Title(), "base", m.Name, "store", store.Name)
				return img, true
			}
		}
	}

	return Image{}, false
}
