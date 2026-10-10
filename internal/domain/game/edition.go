package game

import (
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"
)

// EditionCover is the cover chosen for an edition: an image URL or a photo of one of its copies.
// The zero value means the cover providers choose one.
type EditionCover struct {
	// URL is an image picked among the providers' proposals or pasted by the user.
	URL string

	// Photo is a photo of one of the edition's copies. It wins over URL.
	Photo PhotoID
}

// IsZero reports whether no cover was chosen.
func (c EditionCover) IsZero() bool { return c.URL == "" && c.Photo == "" }

// Edition is the game on one system: the copies whose System() is that system, and its cover.
// Editions are derived from the copies; only the chosen covers and the main system are stored.
type Edition struct {
	System string

	// Cover is the chosen cover; zero when the providers choose.
	Cover EditionCover

	// Main marks the edition shown when the game is listed once ("By game").
	Main bool
}

// ParseCoverURL checks a cover image address: empty, or an http(s) URL. It returns it trimmed.
func ParseCoverURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}

	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", invalid("cover url must be an http(s) URL")
	}

	return s, nil
}

func normalizeCover(c EditionCover) (EditionCover, error) {
	if c.Photo != "" {
		if _, err := ParsePhotoID(string(c.Photo)); err != nil {
			return c, err
		}

		return EditionCover{Photo: c.Photo}, nil // a photo wins: keeping a URL too would be ambiguous
	}

	u, err := ParseCoverURL(c.URL)

	return EditionCover{URL: u}, err
}

// systemStat counts a system's copies and says whether one is physical.
type systemStat struct {
	copies   int
	physical bool
}

func (g *Game) systemStats() map[string]systemStat {
	out := map[string]systemStat{}

	for _, c := range g.copies {
		s := out[c.System()]
		s.copies++
		s.physical = s.physical || c.Kind == KindPhysical
		out[c.System()] = s
	}

	return out
}

// Systems returns the systems the game's copies are played on, each once, in alphabetical order.
func (g *Game) Systems() []string {
	return slices.Sorted(maps.Keys(g.systemStats()))
}

func (g *Game) hasSystem(system string) bool {
	return slices.ContainsFunc(g.copies, func(c Copy) bool { return c.System() == system })
}

func (g *Game) hasPhotoOn(system string, id PhotoID) bool {
	return slices.ContainsFunc(g.copies, func(c Copy) bool { return c.System() == system && photoIndex(c.Photos, id) >= 0 })
}

// MainSystem returns the system the user chose for the main edition; empty means the default rule.
func (g *Game) MainSystem() string { return g.mainSystem }

// Covers returns the chosen covers by system (a copy).
func (g *Game) Covers() map[string]EditionCover { return maps.Clone(g.covers) }

// mainOf returns the main edition's system: the user's choice, else an edition with a physical
// copy, then the one with the most copies, then the first by system; empty without copies.
func (g *Game) mainOf() string {
	if g.mainSystem != "" && g.hasSystem(g.mainSystem) {
		return g.mainSystem
	}

	stats := g.systemStats()
	best := ""

	for _, s := range g.Systems() { // alphabetical, so a tie keeps the first
		st, b := stats[s], stats[best]
		if best == "" || (st.physical && !b.physical) || (st.physical == b.physical && st.copies > b.copies) {
			best = s
		}
	}

	return best
}

// MainEdition returns the edition shown when the game is listed once (see mainOf). A game without
// copies has no editions: its main edition is the zero Edition.
func (g *Game) MainEdition() Edition {
	main := g.mainOf()
	if main == "" {
		return Edition{}
	}

	return Edition{
		System: main,
		Cover:  g.covers[main],
		Main:   true,
	}
}

// Editions returns one edition per system of the game's copies: the main one first, then the
// others by system.
func (g *Game) Editions() []Edition {
	main := g.mainOf()
	systems := g.Systems()
	out := make([]Edition, 0, len(systems))

	if main != "" {
		out = append(out, g.MainEdition())
	}

	for _, s := range systems {
		if s != main {
			out = append(out, Edition{
				System: s,
				Cover:  g.covers[s],
			})
		}
	}

	return out
}

// Edition returns the game's edition on a system, and whether it has one.
func (g *Game) Edition(system string) (Edition, bool) {
	i := slices.IndexFunc(g.Editions(), func(e Edition) bool { return e.System == system })
	if i < 0 {
		return Edition{}, false
	}

	return g.Editions()[i], true
}

// SetEditionCover chooses the cover of the edition on system; a zero cover lets the providers
// choose again. A photo must be of one of that edition's copies.
func (g *Game) SetEditionCover(system string, cover EditionCover, now time.Time) error {
	if !g.hasSystem(system) {
		return invalid("%s: this game has no copy on that system; reload the page", system)
	}

	cover, err := normalizeCover(cover)
	if err != nil {
		return err
	}

	if cover.Photo != "" && !g.hasPhotoOn(system, cover.Photo) {
		return invalid("%s: the photo is not of a copy on that system", system)
	}

	if cover.IsZero() {
		delete(g.covers, system)
	} else {
		if g.covers == nil {
			g.covers = map[string]EditionCover{}
		}

		g.covers[system] = cover
	}

	g.updatedAt = now

	return nil
}

// SetMainSystem makes the edition on system the main one; an empty system goes back to the
// default rule.
func (g *Game) SetMainSystem(system string, now time.Time) error {
	if system != "" && !g.hasSystem(system) {
		return invalid("%s: this game has no copy on that system; reload the page", system)
	}

	g.mainSystem = system
	g.updatedAt = now

	return nil
}

// reconcileEditions drops what no longer fits the copies: covers of systems without copies, photo
// covers whose photo no copy of that system has, and a main system without copies.
func (g *Game) reconcileEditions() {
	for system, c := range g.covers {
		if !g.hasSystem(system) || (c.Photo != "" && !g.hasPhotoOn(system, c.Photo)) {
			delete(g.covers, system)
		}
	}

	if g.mainSystem != "" && !g.hasSystem(g.mainSystem) {
		g.mainSystem = ""
	}
}

// RestoreEditions sets the chosen covers and the main system read from storage, dropping what no
// longer fits the copies. Only repositories call it, right after Rehydrate.
func (g *Game) RestoreEditions(covers map[string]EditionCover, mainSystem string) {
	g.covers = maps.Clone(covers)
	g.mainSystem = mainSystem
	g.reconcileEditions()
}

// AdoptGameCover turns the single cover a game had before editions (stored documents of version 2)
// into edition covers. A photo goes to the edition of a copy that has it, and that edition becomes
// the main one, so the game looks as before. A URL goes to the main edition by the default rule,
// unless the photo took that edition; that system is stored as the main one when no photo set it.
// Only repositories call it, right after Rehydrate.
func (g *Game) AdoptGameCover(coverURL string, photo PhotoID) {
	defaultMain := g.mainOf()

	if photo != "" {
		if i := slices.IndexFunc(g.copies, func(c Copy) bool { return photoIndex(c.Photos, photo) >= 0 }); i >= 0 {
			system := g.copies[i].System()
			g.covers = map[string]EditionCover{system: {Photo: photo}}
			g.mainSystem = system
		}
	}

	if coverURL == "" || defaultMain == "" {
		return
	}

	if _, taken := g.covers[defaultMain]; !taken {
		if g.covers == nil {
			g.covers = map[string]EditionCover{}
		}

		g.covers[defaultMain] = EditionCover{URL: coverURL}
	}

	if g.mainSystem == "" {
		g.mainSystem = defaultMain
	}
}

// CoverFingerprints returns, per system of the game, a summary of what that edition's cover depends
// on: its chosen cover, the platforms of its copies (physical ones apart) and the store links. When
// a fingerprint changes, that edition's cached cover may be stale.
func (g *Game) CoverFingerprints() map[string]string {
	links := make([]string, 0, len(g.links))
	for _, k := range g.links.Keys() {
		links = append(links, k+"="+g.links[k])
	}

	out := map[string]string{}

	for _, system := range g.Systems() {
		var platforms []string

		for _, c := range g.copies {
			if c.System() != system {
				continue
			}

			p := c.Platform
			if c.Kind == KindPhysical {
				p += " (physical)"
			}

			if !slices.Contains(platforms, p) {
				platforms = append(platforms, p)
			}
		}

		slices.Sort(platforms)

		cover := g.covers[system]
		out[system] = strings.Join([]string{cover.URL, string(cover.Photo), strings.Join(platforms, "\x1f"), strings.Join(links, "\x1f")}, "\x1e")
	}

	return out
}

// ChangedSystems returns, sorted, the systems whose fingerprints differ between two results of
// CoverFingerprints, editions that appeared or disappeared included.
func ChangedSystems(before, after map[string]string) []string {
	var out []string

	for system, f := range after {
		if before[system] != f {
			out = append(out, system)
		}
	}

	for system := range before {
		if _, ok := after[system]; !ok {
			out = append(out, system)
		}
	}

	slices.Sort(out)

	return out
}
