package media

import (
	"slices"

	"gamevault/internal/domain/game"
)

// GameQuery is what a provider is told about a game: its title, the edition's system, its store
// links and its copies' platforms. Cover, details and link-finding providers all take it.
type GameQuery struct {
	Title string

	// System is the edition's system ("PC", "PS3"…); empty when the query names none (a game
	// without copies, a title search, a game sheet).
	System string

	// Links are the stores the game is linked to ({"steam": "620"}); a provider that knows a
	// store reads its link.
	Links game.Links

	// PhysicalPlatforms lists the platforms of the game's physical copies ("PS3", "Xbox 360"...).
	PhysicalPlatforms []string

	// Platforms lists the platforms of every copy.
	Platforms []string

	// Fallback is set on the second pass, after no store had art for a game it knows: providers
	// that keep their quota for games without store art may then help too.
	Fallback bool

	// LinksOnly asks providers to find the game by its store links alone, never by searching its
	// title: set for a virtual PC edition (game.Game.BorrowsPCBox), whose PC box must be the linked
	// store's art and nothing a title search might confuse with it.
	LinksOnly bool

	// Browse is set where the user chooses (the cover picker, the scan page's typed search): a
	// title search may then also propose near matches (other games its search found), after the
	// game's own art. The automatic resolution and barcode lookup never set it, since they keep
	// the first candidate.
	Browse bool
}

// HasStoreLink reports whether the game is linked to a store, whose providers have its own art.
// Quota-limited providers skip those games.
func (q GameQuery) HasStoreLink() bool { return len(q.Links) > 0 }

// ForPC reports whether PC store art fits the query: its system is PC, or it names none. Store
// providers apply only then.
func (q GameQuery) ForPC() bool { return q.System == "" || q.System == game.SystemPC }

// SearchSystems returns the systems a title search for the query is filtered to: the edition's
// system, or without one the systems of the game's physical copies (the boxes the user holds), or
// else of all its copies, each once. Platforms are named by their system ("PlayStation Store" →
// "PS4", "Steam" → "PC") and aliases by their catalog name ("Mega Drive" → "Mega Drive / Genesis"),
// the names the game databases' platform tables are keyed by, so covers and sheets map a copy to
// the same platform there, even one stored before its spelling became an alias.
func (q GameQuery) SearchSystems() []string {
	if q.System != "" {
		return []string{searchSystem(q.System)}
	}

	platforms := q.PhysicalPlatforms
	if len(platforms) == 0 {
		platforms = q.Platforms
	}

	systems := make([]string, 0, len(platforms))
	for _, pl := range platforms {
		if s := searchSystem(pl); s != game.SystemOther && !slices.Contains(systems, s) {
			systems = append(systems, s)
		}
	}

	return systems
}

// searchSystem names a platform or system by its catalog system: SystemOf for stores and exact
// names, then SystemNamed for an alias SystemOf leaves as typed.
func searchSystem(p string) string {
	s := game.SystemOf(p)
	if sys, ok := game.SystemNamed(s); ok {
		return sys.Name
	}

	return s
}

// HasPhysical reports whether the game has at least one physical copy.
func (q GameQuery) HasPhysical() bool { return len(q.PhysicalPlatforms) > 0 }

// QueryFor builds the query of a game's edition on system, from that edition's copies. An
// empty system takes every copy and names no system (games without copies, game sheets).
func QueryFor(g *game.Game, system string) GameQuery {
	q := GameQuery{
		Title:  g.Title(),
		System: system,
		Links:  g.Links(),
	}
	for _, c := range g.Copies() {
		if c.Platform == "" || (system != "" && c.System() != system) {
			continue
		}

		if c.Kind == game.KindPhysical && !slices.Contains(q.PhysicalPlatforms, c.Platform) {
			q.PhysicalPlatforms = append(q.PhysicalPlatforms, c.Platform)
		}

		if !slices.Contains(q.Platforms, c.Platform) {
			q.Platforms = append(q.Platforms, c.Platform)
		}
	}

	return q
}
