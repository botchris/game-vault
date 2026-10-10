package game

import (
	"slices"
	"time"
)

// ImportedCopy is a copy reported by an external source (a Humble order, a Steam library, a CSV row).
type ImportedCopy struct {
	// ExternalID must be stable across scans of the same source, e.g. "steam:620".
	ExternalID string

	// PreviousExternalID is the id an older version of the source gave this copy, when the format
	// changed. A copy saved under it is adopted (renamed) if it belongs to the same game, instead
	// of a duplicate being created.
	PreviousExternalID string

	// Withdrawn means the source no longer counts this item as a copy (e.g. a key that turned out
	// not to be a game): a copy it imported earlier under ExternalID is removed, and its game too
	// if nothing else is left in it.
	Withdrawn bool
	Title     string

	// Links are the stores the source knows the game by (a Humble key reports its Steam AppID).
	// They are added to the game where it has no link to that store yet.
	Links   Links
	Details CopyDetails

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

	c.indexLinks(g, g.links)

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

// Apply consolidates the imported copies on behalf of sourceID (empty for one-off imports).
func (c *Consolidator) Apply(sourceID string, imported []ImportedCopy, now time.Time) ConsolidationResult {
	var r ConsolidationResult

	for _, in := range imported {
		if in.Withdrawn {
			c.withdraw(sourceID, in, now, &r)
			continue
		}

		if in.ExternalID == "" || in.Title == "" {
			r.Warnings = append(r.Warnings, "skipped a copy without title or external id: "+in.Title)
			continue
		}

		details, err := in.Details.normalize()
		if err != nil {
			r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
			continue
		}

		sourceSystem, err := normalizeSystem(in.System)
		if err != nil {
			r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
			sourceSystem = ""
		}

		if in.Links, err = in.Links.normalize(); err != nil {
			r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
			in.Links = nil
		}

		c.adoptPrevious(in)

		if g, ok := c.byExternalID[in.ExternalID]; ok {
			i := g.indexOfExternal(in.ExternalID)
			cp := &g.copies[i]

			changed := cp.applyImport(details)
			if cp.SourceSystem != sourceSystem {
				cp.SourceSystem = sourceSystem
				changed = true
			}

			if sourceID != "" && cp.SourceID != sourceID {
				cp.SourceID = sourceID // adopt copies created by a CSV import or an older source
				changed = true
			}

			if added := g.links.fill(in.Links); len(added) > 0 {
				c.indexLinks(g, added)

				changed = true
			}

			if changed {
				g.reconcileEditions() // a scan can change a copy's platform, and so its system
				cp.UpdatedAt, g.updatedAt = now, now
				c.markChanged(g)

				r.CopiesUpdated++
			} else {
				r.CopiesUnchanged++
			}

			continue
		}

		g := c.findGame(in)
		if g == nil {
			ng, err := New(in.Title, now)
			if err != nil {
				r.Warnings = append(r.Warnings, err.Error())
				continue
			}

			g = ng
			r.GamesCreated++
		}

		g.links.fill(in.Links)

		if _, err := g.addCopy(details, sourceID, in.ExternalID, now); err != nil {
			r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
			continue
		}

		g.copies[len(g.copies)-1].SourceSystem = sourceSystem

		c.index(g)
		c.markChanged(g)

		r.CopiesAdded++
	}

	// A game emptied by a withdrawal may have received another copy later in the same import.
	emptied := r.Emptied
	r.Emptied = nil

	for _, g := range c.order {
		if len(g.copies) == 0 && slices.Contains(emptied, g.id) {
			r.Emptied = append(r.Emptied, g.id)
			continue
		}

		r.Changed = append(r.Changed, g)
	}

	return r
}

// withdraw removes the copy the source imported under in.ExternalID. The previous id only counts
// when its copy is in the withdrawn item's own game: old ids could be shared by several items.
// Copies added by hand or by another source are never removed.
func (c *Consolidator) withdraw(sourceID string, in ImportedCopy, now time.Time, r *ConsolidationResult) {
	for _, ext := range []string{in.ExternalID, in.PreviousExternalID} {
		g, ok := c.byExternalID[ext]
		if ext == "" || !ok {
			continue
		}

		if ext == in.PreviousExternalID && c.findGame(in) != g {
			continue
		}

		cp := g.copies[g.indexOfExternal(ext)]
		if cp.SourceID != sourceID {
			continue
		}

		if _, err := g.RemoveCopy(cp.ID, now); err != nil {
			continue
		}

		delete(c.byExternalID, ext)
		c.markChanged(g)

		r.CopiesRemoved++

		if len(g.copies) == 0 {
			r.Emptied = append(r.Emptied, g.id)
		}
	}
}

// adoptPrevious renames the copy saved under in.PreviousExternalID to in.ExternalID when it is in
// the game the import belongs to. Its source details (key, status, deadline) are cleared first:
// under the old id several copies may have been written into it, so they can belong to another
// game. What the user added (notes, location, condition) is kept.
func (c *Consolidator) adoptPrevious(in ImportedCopy) {
	if in.PreviousExternalID == "" || in.PreviousExternalID == in.ExternalID {
		return
	}

	if _, ok := c.byExternalID[in.ExternalID]; ok {
		return
	}

	g, ok := c.byExternalID[in.PreviousExternalID]
	if !ok || c.findGame(in) != g {
		return
	}

	cp := &g.copies[g.indexOfExternal(in.PreviousExternalID)]
	cp.ExternalID = in.ExternalID
	cp.Key, cp.RedeemBy, cp.Status = "", "", ""

	delete(c.byExternalID, in.PreviousExternalID)
	c.byExternalID[in.ExternalID] = g
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
