package media

import (
	"context"
	"fmt"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// CoverChoices are the covers proposed for an edition (see Covers.CoverCandidates).
type CoverChoices struct {
	// Candidates are the images proposed, the first pass's providers' first.
	Candidates []CoverCandidate

	// Warnings are the failures of providers for this game.
	Warnings []string

	// Unavailable reports that a provider had a provider-level problem (see
	// providers.Registry.Statuses), which is not a warning on the game.
	Unavailable bool
}

// CoverCandidates asks every enabled provider that applies to the game's edition on system (the
// main edition when system is empty) for images, for the "choose cover" dialog. Unlike the automatic
// resolution, the providers that keep to the fallback pass (TheGamesDB for a game a store knows,
// IGDB, SteamGridDB) are always asked too: the user opened the picker to see every choice, and it
// costs one request each, once per opening. Each provider is asked once; the candidates of those
// that apply to the first pass (the stores' own art) come first, then the fallback ones, each group
// in chain order.
// Providers failing for this game are reported as warnings (see CoverChoices).
func (c *Covers) CoverCandidates(ctx context.Context, id game.ID, system string) (CoverChoices, error) {
	g, err := c.games.Get(ctx, id)
	if err != nil {
		return CoverChoices{}, err
	}

	if system == "" {
		system = g.MainEdition().System
	}

	if _, ok := g.Edition(system); !ok && len(g.Copies()) > 0 {
		return CoverChoices{}, fmt.Errorf("%w (%s)", ErrNoEdition, system)
	}

	chain, err := c.chain.inOrder(ctx)
	if err != nil {
		return CoverChoices{}, err
	}

	first := QueryFor(g, system)
	first.Browse = true // the user chooses: near matches may follow the game's own art
	fallback := first
	fallback.Fallback = true
	asked := map[provider.ID]bool{}

	var out CoverChoices

	// A provider is asked with the first pass's query when it applies to it (as the automatic
	// resolution would ask it), else with the fallback pass's.
	for _, q := range []GameQuery{first, fallback} {
		for _, p := range chain {
			if asked[p.ID()] || !c.chain.impls[p.ID()].Applies(q) {
				continue
			}

			asked[p.ID()] = true

			found, w, limited := c.chain.candidates(ctx, p, q)
			out.Candidates = append(out.Candidates, found...)
			out.Warnings = append(out.Warnings, w...)
			out.Unavailable = out.Unavailable || limited
		}
	}

	return out, nil
}
