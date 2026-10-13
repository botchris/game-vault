package game

import (
	"slices"
	"time"
)

// ImportedCopy is a copy reported by an external source (a Humble order, a Steam library, a CSV row).
type ImportedCopy struct {
	// ExternalID must be stable across scans of the same source, e.g. "steam:620".
	ExternalID string

	// Withdrawn means the source no longer counts this item as a copy (e.g. a key that turned out
	// not to be a game): a copy it imported earlier under ExternalID is removed, and its game too
	// if nothing else is left in it.
	Withdrawn bool
	Title     string

	// Links are the stores the source knows the game by (a Humble key reports its Steam AppID).
	// They are added to the game where it has no link to that store yet.
	Links Links

	// AutoLinks are links found automatically that an export carried (see Game.AutoLinks): they are
	// added as automatic links where the game has none and the user did not reject them, and never
	// used to recognize the game.
	AutoLinks Links
	Details   CopyDetails

	// System is the exact system the copy is played on, when the source knows it (PS4 or PS5 for a
	// PlayStation Store game). It wins over the one the platform implies, never over the user's.
	System string
}

// ConsolidationResult summarizes what a consolidation changed.
type ConsolidationResult struct {
	Changed         []*Game // games to persist (new or modified)
	CopiesAdded     int
	CopiesUpdated   int
	CopiesUnchanged int
	GamesCreated    int
	CopiesRemoved   int

	// Emptied are games whose only copies were withdrawn: the caller deletes them.
	Emptied  []ID
	Warnings []string

	// InvalidSystems lists, once each, the systems the source named that are not valid names; the
	// copies keep the system their platform implies. They are apart from Warnings because a source
	// sends the same one on every scan, and a caller may want to warn only once.
	InvalidSystems []InvalidSystem
}

// InvalidSystem is a system a source named that is not a valid name.
type InvalidSystem struct {
	// System is the name as the source sent it.
	System string

	// Warning says which copy had it first and why it was refused.
	Warning string
}

// addInvalidSystem records an invalid system the first time it is seen.
func (r *ConsolidationResult) addInvalidSystem(system, warning string) {
	if slices.ContainsFunc(r.InvalidSystems, func(i InvalidSystem) bool { return i.System == system }) {
		return
	}

	r.InvalidSystems = append(r.InvalidSystems, InvalidSystem{
		System:  system,
		Warning: warning,
	})
}

// Consolidator merges imported copies into the catalog, Sonarr-style: each imported copy is
// matched to the game it belongs to, so a game appears once no matter how many copies you own.
//
// Matching order:
//  1. a copy with the same ExternalID (the copy was imported before) → update it in place;
//  2. a game linked to the same id in one of the copy's stores (Links);
//  3. a game whose title has the same MatchKey;
//  4. otherwise a new game is created.
type Consolidator struct {
	byExternalID map[string]*Game
	byLink       map[string]*Game
	byMatchKey   map[string]*Game
	changed      map[ID]*Game
	order        []*Game
}

// NewConsolidator indexes the current catalog.
func NewConsolidator(games []*Game) *Consolidator {
	c := &Consolidator{
		byExternalID: map[string]*Game{},
		byLink:       map[string]*Game{},
		byMatchKey:   map[string]*Game{},
		changed:      map[ID]*Game{},
	}
	for _, g := range games {
		c.index(g)
	}

	return c
}

func (c *Consolidator) index(g *Game) {
	for _, cp := range g.copies {
		if cp.ExternalID != "" {
			c.byExternalID[cp.ExternalID] = g
		}
	}

	c.indexLinks(g, g.consolidationLinks()) // automatic links never merge games

	if k := g.MatchKey(); k != "" {
		if _, ok := c.byMatchKey[k]; !ok {
			c.byMatchKey[k] = g
		}
	}
}

func (c *Consolidator) markChanged(g *Game) {
	if _, ok := c.changed[g.id]; !ok {
		c.changed[g.id] = g
		c.order = append(c.order, g)
	}
}

// Apply consolidates the imported copies on behalf of sourceID (empty for one-off imports). Each
// copy is checked (prepare), then either updates the copy imported earlier under its ExternalID
// (updateExisting) or joins the game it matches, which is created when there is none (addToGame).
func (c *Consolidator) Apply(sourceID string, imported []ImportedCopy, now time.Time) ConsolidationResult {
	var r ConsolidationResult

	for _, in := range imported {
		if in.Withdrawn {
			c.withdraw(sourceID, in, now, &r)
			continue
		}

		p, ok := prepare(in, &r)
		if !ok {
			continue
		}

		g, ok := c.byExternalID[p.ExternalID]
		if !ok {
			c.addToGame(p, sourceID, now, &r)
			continue
		}

		if c.updateExisting(g, p, sourceID, now) {
			r.CopiesUpdated++
		} else {
			r.CopiesUnchanged++
		}
	}

	r.Changed, r.Emptied = c.collect(r.Emptied)

	return r
}

// preparedCopy is an imported copy whose details, system and links were checked.
type preparedCopy struct {
	ImportedCopy

	details      CopyDetails
	sourceSystem string
}

// prepare checks an imported copy. A copy without title or id, or with invalid details, is
// skipped with a warning; an invalid system or invalid links are dropped with one, and the copy
// is kept.
func prepare(in ImportedCopy, r *ConsolidationResult) (preparedCopy, bool) {
	if in.ExternalID == "" || in.Title == "" {
		r.Warnings = append(r.Warnings, "skipped a copy without title or external id: "+in.Title)
		return preparedCopy{}, false
	}

	details, err := in.Details.normalize()
	if err != nil {
		r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
		return preparedCopy{}, false
	}

	sourceSystem, err := normalizeSystem(in.System)
	if err != nil {
		r.addInvalidSystem(in.System, in.Title+": "+err.Error())

		sourceSystem = ""
	}

	if in.Links, err = in.Links.normalize(); err != nil {
		r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
		in.Links = nil
	}

	return preparedCopy{
		ImportedCopy: in,
		details:      details,
		sourceSystem: sourceSystem,
	}, true
}

// updateExisting applies an import to the copy g holds under its ExternalID and reports whether
// anything changed.
func (c *Consolidator) updateExisting(g *Game, p preparedCopy, sourceID string, now time.Time) bool {
	cp := &g.copies[g.indexOfExternal(p.ExternalID)]

	changed := cp.applyImport(p.details)
	// A one-off import (a file) knows no source, so it must not erase what a source said.
	if sourceID != "" && cp.SourceSystem != p.sourceSystem {
		cp.SourceSystem = p.sourceSystem
		changed = true
	}

	if sourceID != "" && cp.SourceID != sourceID {
		cp.SourceID = sourceID // adopt copies created by a CSV import or an older source
		changed = true
	}

	own, linksChanged := applyLinks(g, p.ImportedCopy, now)
	c.indexLinks(g, own)

	if !changed && !linksChanged {
		return false
	}

	g.reconcileEditions() // a scan can change a copy's platform, and so its system
	cp.UpdatedAt, g.updatedAt = now, now
	c.markChanged(g)

	return true
}

// addToGame adds an imported copy to the game it matches, or to a new game.
func (c *Consolidator) addToGame(p preparedCopy, sourceID string, now time.Time, r *ConsolidationResult) {
	g := c.findGame(p.ImportedCopy)
	if g == nil {
		ng, err := New(p.Title, now)
		if err != nil {
			r.Warnings = append(r.Warnings, err.Error())
			return
		}

		g = ng
		r.GamesCreated++
	}

	applyLinks(g, p.ImportedCopy, now) // before the copy: links can decide its game's editions

	from := copyOrigin{
		sourceID:     sourceID,
		externalID:   p.ExternalID,
		sourceSystem: p.sourceSystem,
	}
	if _, err := g.addCopy(p.details, from, now); err != nil {
		r.Warnings = append(r.Warnings, p.Title+": "+err.Error())
		return
	}

	c.index(g)
	c.markChanged(g)

	r.CopiesAdded++
}

// applyLinks adds an import's links to g in the same order for every copy: the source's own links
// first, which replace automatic ones because the source knows the store id, then the automatic
// ones where g still has no link. It returns the source's links it added, which imports may now
// recognize g by, and whether g's links changed.
func applyLinks(g *Game, in ImportedCopy, now time.Time) (Links, bool) {
	own := g.importLinks(in.Links)
	auto := g.AddAutoLinks(in.AutoLinks, now)

	return own, len(own) > 0 || len(auto) > 0
}

// collect returns the games to save, in the order they first changed, and the emptied games to
// delete. A game emptied by a withdrawal may have received another copy later in the same import:
// it is saved instead.
func (c *Consolidator) collect(emptied []ID) (changed []*Game, deleted []ID) {
	for _, g := range c.order {
		if len(g.copies) == 0 && slices.Contains(emptied, g.id) {
			deleted = append(deleted, g.id)
			continue
		}

		changed = append(changed, g)
	}

	return changed, deleted
}

// withdraw removes the copy the source imported under in.ExternalID. Copies added by hand or by
// another source are never removed.
func (c *Consolidator) withdraw(sourceID string, in ImportedCopy, now time.Time, r *ConsolidationResult) {
	g, ok := c.byExternalID[in.ExternalID]
	if in.ExternalID == "" || !ok {
		return
	}

	cp := g.copies[g.indexOfExternal(in.ExternalID)]
	if cp.SourceID != sourceID {
		return
	}

	if _, err := g.RemoveCopy(cp.ID, now); err != nil {
		return
	}

	delete(c.byExternalID, in.ExternalID)
	c.markChanged(g)

	r.CopiesRemoved++

	if len(g.copies) == 0 {
		r.Emptied = append(r.Emptied, g.id)
	}
}

// indexLinks indexes g under links, unless another game already holds one of them: the first game
// linked to a store id keeps it, as with titles.
func (c *Consolidator) indexLinks(g *Game, links Links) {
	for k, v := range links {
		if _, ok := c.byLink[linkIndex(k, v)]; !ok {
			c.byLink[linkIndex(k, v)] = g
		}
	}
}

func linkIndex(store, id string) string { return store + "\x00" + id }

func (c *Consolidator) findGame(in ImportedCopy) *Game {
	for _, k := range in.Links.Keys() {
		if g, ok := c.byLink[linkIndex(k, in.Links[k])]; ok {
			return g
		}
	}

	if g, ok := c.byMatchKey[MatchKey(in.Title)]; ok {
		return g
	}

	return nil
}

func (g *Game) indexOfExternal(ext string) int {
	for i := range g.copies {
		if g.copies[i].ExternalID == ext {
			return i
		}
	}

	return -1
}
