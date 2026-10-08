package game

import "time"

// ImportedCopy is a copy reported by an external source (a Humble order, a Steam library, a CSV row).
type ImportedCopy struct {
	// ExternalID must be stable across scans of the same source, e.g. "steam:620".
	ExternalID string
	// PreviousExternalID is the id an older version of the source gave this copy, when the format
	// changed. A copy saved under it is adopted (renamed) if it belongs to the same game, instead
	// of a duplicate being created.
	PreviousExternalID string
	Title              string
	SteamAppID         int64
	Details            CopyDetails
}

// ConsolidationResult summarizes what a consolidation changed.
type ConsolidationResult struct {
	Changed         []*Game // games to persist (new or modified)
	CopiesAdded     int
	CopiesUpdated   int
	CopiesUnchanged int
	GamesCreated    int
	Warnings        []string
}

// Consolidator merges imported copies into the catalog, Sonarr-style: each imported copy is
// matched to the game it belongs to, so a game appears once no matter how many copies you own.
//
// Matching order:
//  1. a copy with the same ExternalID (the copy was imported before) → update it in place;
//  2. a game with the same Steam AppID;
//  3. a game whose title has the same MatchKey;
//  4. otherwise a new game is created.
type Consolidator struct {
	byExternalID map[string]*Game
	bySteamID    map[int64]*Game
	byMatchKey   map[string]*Game
	changed      map[ID]*Game
	order        []*Game
}

// NewConsolidator indexes the current catalog.
func NewConsolidator(games []*Game) *Consolidator {
	c := &Consolidator{
		byExternalID: map[string]*Game{},
		bySteamID:    map[int64]*Game{},
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

	if g.steamAppID != 0 {
		if _, ok := c.bySteamID[g.steamAppID]; !ok {
			c.bySteamID[g.steamAppID] = g
		}
	}

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
		if in.ExternalID == "" || in.Title == "" {
			r.Warnings = append(r.Warnings, "skipped a copy without title or external id: "+in.Title)
			continue
		}

		details, err := in.Details.normalize()
		if err != nil {
			r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
			continue
		}

		c.adoptPrevious(in)

		if g, ok := c.byExternalID[in.ExternalID]; ok {
			i := g.indexOfExternal(in.ExternalID)
			cp := &g.copies[i]

			changed := cp.applyImport(details)
			if sourceID != "" && cp.SourceID != sourceID {
				cp.SourceID = sourceID // adopt copies created by a CSV import or an older source
				changed = true
			}

			if g.steamAppID == 0 && in.SteamAppID != 0 {
				g.steamAppID = in.SteamAppID
				c.bySteamID[in.SteamAppID] = g
				changed = true
			}

			if changed {
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

			ng.steamAppID = in.SteamAppID
			g = ng
			r.GamesCreated++
		} else if g.steamAppID == 0 && in.SteamAppID != 0 {
			g.steamAppID = in.SteamAppID
		}

		if _, err := g.addCopy(details, sourceID, in.ExternalID, now); err != nil {
			r.Warnings = append(r.Warnings, in.Title+": "+err.Error())
			continue
		}

		c.index(g)
		c.markChanged(g)

		r.CopiesAdded++
	}

	r.Changed = c.order

	return r
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

func (c *Consolidator) findGame(in ImportedCopy) *Game {
	if in.SteamAppID != 0 {
		if g, ok := c.bySteamID[in.SteamAppID]; ok {
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
