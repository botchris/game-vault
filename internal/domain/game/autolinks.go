package game

import (
	"maps"
	"slices"
	"strings"
	"time"
)

// Automatic links are store links Game Vault found by itself (an IGDB match that names the game's
// Steam AppID, a Steam search that found exactly that title), so a console game gets the store's
// sheet and box without the user linking it by hand. They are links like the others for covers and
// sheets, but:
//
//   - they are marked, so the UI can label them;
//   - the user wins: a link the user sets or changes is theirs, and an automatic link the user
//     removes is remembered as rejected and never added again (adding it by hand clears that);
//   - a source's import wins too: it replaces an automatic link with its own;
//   - they are not used to recognize a game in imports (see Consolidator), so a guessed link can
//     never merge two games.

// AutoLinks returns, sorted, the keys of the links found automatically.
func (g *Game) AutoLinks() []string {
	return slices.Sorted(maps.Keys(g.autoLinks))
}

// LinkRef is one store link: a link key and the game's id there.
type LinkRef struct {
	Key string
	ID  string
}

// RejectedLinks returns, sorted, the automatic links the user removed, which are never added again.
func (g *Game) RejectedLinks() []LinkRef {
	out := slices.Collect(maps.Keys(g.rejectedLinks))
	slices.SortFunc(out, func(a, b LinkRef) int {
		if a.Key != b.Key {
			return strings.Compare(a.Key, b.Key)
		}

		return strings.Compare(a.ID, b.ID)
	})

	return out
}

func (g *Game) reject(key, id string) {
	if g.rejectedLinks == nil {
		g.rejectedLinks = map[LinkRef]bool{}
	}

	g.rejectedLinks[LinkRef{
		Key: key,
		ID:  id,
	}] = true
}

func (g *Game) rejected(key, id string) bool {
	return g.rejectedLinks[LinkRef{
		Key: key,
		ID:  id,
	}]
}

// AutoLinkTriedAt returns when links were last searched for automatically; zero when never.
func (g *Game) AutoLinkTriedAt() time.Time { return g.autoLinkTriedAt }

// RestoreLinkOrigins sets the automatic links' marks, the rejected links and the last automatic
// search read from storage, dropping marks of links that are gone. Only repositories call it, right
// after Rehydrate.
func (g *Game) RestoreLinkOrigins(auto []string, rejected []LinkRef, triedAt time.Time) {
	g.autoLinks = nil

	for _, k := range auto {
		if g.links[k] != "" {
			g.markAuto(k)
		}
	}

	g.rejectedLinks = nil
	for _, r := range rejected {
		g.reject(r.Key, r.ID)
	}

	g.autoLinkTriedAt = triedAt
}

// AddAutoLinks adds the links found automatically that the game lacks and the user has not
// rejected, marked as automatic, and returns them. Links the game already has (by the user, a
// source or an earlier search) are kept as they are, and invalid ones are skipped (Links.Valid
// says which).
func (g *Game) AddAutoLinks(found Links, now time.Time) Links {
	found, _ = found.Valid() // the caller that found them reports the invalid ones

	added := Links{}

	for _, k := range found.Keys() {
		id := found[k]
		if g.links[k] != "" || g.rejected(k, id) {
			continue
		}

		if g.links == nil {
			g.links = Links{}
		}

		g.links[k] = id
		g.markAuto(k)
		added[k] = id
	}

	if len(added) > 0 {
		g.updatedAt = now
	}

	return added
}

// MarkAutoLinkTried records an automatic search, which is not a change the user made: the game's
// update time stays (covers are addressed by it).
func (g *Game) MarkAutoLinkTried(at time.Time) { g.autoLinkTriedAt = at }

func (g *Game) markAuto(key string) {
	if g.autoLinks == nil {
		g.autoLinks = map[string]bool{}
	}

	g.autoLinks[key] = true
}

// consolidationLinks returns the links imports may recognize the game by: all but the automatic ones.
func (g *Game) consolidationLinks() Links {
	out := Links{}

	for k, v := range g.links {
		if !g.autoLinks[k] {
			out[k] = v
		}
	}

	return out
}

// applyUserLinks records what the user's new links mean for the automatic ones, before they replace
// the old: an automatic link removed or changed is rejected (and no longer automatic), and a
// rejected link the user sets by hand is no longer rejected.
func (g *Game) applyUserLinks(next Links) {
	for k := range g.autoLinks {
		if next[k] == g.links[k] {
			continue // kept as it was: still automatic
		}

		g.reject(k, g.links[k])
		delete(g.autoLinks, k)
	}

	for k, v := range next {
		if !g.autoLinks[k] {
			delete(g.rejectedLinks, LinkRef{
				Key: k,
				ID:  v,
			})
		}
	}
}

// importLinks adds a source's links: those the game lacks, and those that replace an automatic link
// (the source knows the store id). It returns the links added or replaced.
func (g *Game) importLinks(in Links) Links {
	added := Links{}

	for k, v := range in {
		if v == "" || (g.links[k] != "" && !g.autoLinks[k]) {
			continue
		}

		if g.links == nil {
			g.links = Links{}
		}

		if g.links[k] != v {
			added[k] = v
		}

		g.links[k] = v
		delete(g.autoLinks, k)
	}

	return added
}
