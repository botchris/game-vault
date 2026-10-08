package media

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// reAddOn recognises titles of extra content: "GRIP: Combat Racing Artifex DLC", "X - Soundtrack"…
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

// baseQueries are titles to search the base game by on Steam, most specific first: the words
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
// catalog when you own it, else from Steam after finding the base game by title.
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
	if s.search == nil {
		return Image{}, false
	}
	key := game.MatchKey(g.Title())
	for _, q := range baseQueries(g.Title()) {
		apps, err := s.search.SearchApps(ctx, q)
		if err != nil {
			return Image{}, false
		}
		for _, a := range apps {
			if a.AppID == g.SteamAppID() || !isWordPrefix(game.MatchKey(a.Name), key) {
				continue
			}
			// Linked to a Steam AppID, the query only reaches providers that know it (no quota spent).
			if img, err := s.coverFromChain(ctx, ref, CoverQuery{Title: a.Name, SteamAppID: a.AppID}, map[provider.ID]bool{}); err == nil {
				s.log.Info("add-on cover taken from its base game on Steam", "game", g.Title(), "base", a.Name)
				return img, true
			}
		}
	}
	return Image{}, false
}
