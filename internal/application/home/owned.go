package home

import (
	"context"
	"fmt"
	"slices"

	"gamevault/internal/application/ownership"
	"gamevault/internal/domain/game"
)

// unowned returns up to shownDeals deals of each list, in order, without the games owned on PC; a
// game held only on consoles stays, with those systems in OwnedOn. Both lists are checked together,
// in batches the ownership lookup accepts.
func (s *Service) unowned(ctx context.Context, featured, lows []game.Deal) ([]game.Deal, []game.Deal, error) {
	all := slices.Concat(featured, lows)
	results := make([]ownership.Result, 0, len(all))

	for batch := range slices.Chunk(all, ownership.MaxItems) {
		items := make([]ownership.Item, 0, len(batch))
		for _, d := range batch {
			// A deal without a Steam id is looked up by its title only: the lookup ignores a link
			// with an empty id, so it never matches every game without a Steam link.
			items = append(items, ownership.Item{
				Store: game.LinkSteam,
				ID:    d.SteamAppID,
				Title: d.Title,
			})
		}

		got, err := s.owners.Check(ctx, items)
		if err != nil {
			return nil, nil, err
		}

		// One answer per item is the port's promise; a short answer would shift every deal's.
		if len(got) != len(items) {
			return nil, nil, fmt.Errorf("checking deals against the catalog: %d answers for %d deals", len(got), len(items))
		}

		results = append(results, got...)
	}

	return keep(featured, results[:len(featured)]), keep(lows, results[len(featured):]), nil
}

// keep returns up to shownDeals deals whose result does not say the game is owned on PC. Only an
// owned answer leaves a deal out or marks it: a possible match (several games, or a title that
// matches once edition tags are ignored) may be another game, and the deal stays as it is.
func keep(deals []game.Deal, results []ownership.Result) []game.Deal {
	out := make([]game.Deal, 0, min(len(deals), shownDeals))

	for i, d := range deals {
		r := results[i]
		if r.Status == ownership.StatusOwned && r.Game != nil {
			systems := heldSystems(r.Game.Copies)
			if slices.Contains(systems, game.SystemPC) {
				continue
			}

			d.OwnedOn = systems
		}

		out = append(out, d)
		if len(out) == shownDeals {
			break
		}
	}

	return out
}

// heldSystems returns the systems of the copies, sorted and without repeats, nil when there are
// none. The lookup lists only the copies the user still has.
func heldSystems(copies []ownership.Copy) []string {
	var out []string

	for _, c := range copies {
		if !slices.Contains(out, c.System) {
			out = append(out, c.System)
		}
	}

	slices.Sort(out)

	return out
}
