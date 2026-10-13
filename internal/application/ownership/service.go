// Package ownership answers "do I already own it?" for what a store page or a pasted list shows:
// store ids and titles, matched against the catalog without changing anything.
package ownership

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/game"
)

// MaxItems is the most items one check may hold.
const MaxItems = 200

// maxCandidates bounds the games a possible match lists.
const maxCandidates = 5

// ErrTooManyItems means a check held more than MaxItems items.
var ErrTooManyItems = errkind.New(errkind.Invalid,
	fmt.Sprintf("at most %d items can be checked at once; split the list", MaxItems))

// Status says whether an item is in the catalog.
type Status string

// Values of Status.
const (
	StatusOwned    Status = "owned"     // exactly one game matches
	StatusPossible Status = "possible"  // several games match, or one only once edition and store tags are ignored
	StatusNotOwned Status = "not_owned" // no game with a current copy matches
)

// Item is something seen on a store page or in a pasted list: a store's id for a game, its title,
// or both.
type Item struct {
	// Store is the store's link key ("steam", "gog"); it only counts together with ID.
	Store string

	// ID is the game's id in that store ("4410").
	ID    string
	Title string
}

// Copy summarizes one current copy of a matching game.
type Copy struct {
	Platform string
	System   string
	Kind     game.Kind

	// PendingKey reports a key not redeemed yet, which reads differently from an owned copy.
	PendingKey bool
}

// Match is a catalog game that matches an item.
type Match struct {
	ID    game.ID
	Title string

	// Copies are its current copies: sold, given away and expired ones are left out.
	Copies []Copy
}

// Result answers one item.
type Result struct {
	Status Status

	// Game is the matching game when Status is StatusOwned.
	Game *Match

	// Candidates are the matching games, by title, when Status is StatusPossible (at most five).
	Candidates []Match
}

// Catalog is the port that reads the games. A game.Repository satisfies it.
type Catalog interface {
	// List returns every game with its copies.
	List(ctx context.Context) ([]*game.Game, error)
}

// Service exposes the ownership lookup. It only reads: it saves nothing and calls no provider.
type Service struct{ games Catalog }

// NewService builds the service on the catalog's games.
func NewService(games Catalog) *Service { return &Service{games: games} }

// Check answers each item, in order: first by a store link (automatic links included) equal to
// {Store, ID}, then by the exact title (game.SameTitle), then by the title without edition and
// store tags (game.MatchKey), which can only make an item possible. Games without a current copy
// are not owned.
func (s *Service) Check(ctx context.Context, items []Item) ([]Result, error) {
	if len(items) > MaxItems {
		return nil, ErrTooManyItems
	}

	results := make([]Result, 0, len(items))
	if len(items) == 0 {
		return results, nil
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return nil, err
	}

	ix := newIndex(games)
	for _, it := range items {
		results = append(results, ix.check(it))
	}

	return results, nil
}

// index finds games by store link, by SameTitle's key and by MatchKey. It is built once per check
// and holds only games with a current copy.
type index struct {
	byLink  map[string][]*game.Game
	byTitle map[string][]*game.Game
	byKey   map[string][]*game.Game
}

func newIndex(games []*game.Game) index {
	ix := index{
		byLink:  map[string][]*game.Game{},
		byTitle: map[string][]*game.Game{},
		byKey:   map[string][]*game.Game{},
	}

	for _, g := range games {
		if len(currentCopies(g)) == 0 {
			continue
		}

		for store, id := range g.Links() {
			k := linkKey(store, id)
			ix.byLink[k] = append(ix.byLink[k], g)
		}

		if k := game.TitleKey(g.Title()); k != "" {
			ix.byTitle[k] = append(ix.byTitle[k], g)
		}

		if k := g.MatchKey(); k != "" {
			ix.byKey[k] = append(ix.byKey[k], g)
		}
	}

	return ix
}

func (ix index) check(it Item) Result {
	if strings.TrimSpace(it.Store) != "" && strings.TrimSpace(it.ID) != "" {
		if r, ok := decide(ix.byLink[linkKey(it.Store, it.ID)], false); ok {
			return r
		}
	}

	if r, ok := decide(ix.byTitle[game.TitleKey(it.Title)], false); ok {
		return r
	}

	if r, ok := decide(ix.byKey[game.MatchKey(it.Title)], true); ok {
		return r
	}

	return Result{Status: StatusNotOwned}
}

// decide turns the games one rule found into a result; ok is false when it found none. A loose
// rule (tags ignored) never says owned.
func decide(games []*game.Game, loose bool) (Result, bool) {
	switch {
	case len(games) == 0:
		return Result{}, false
	case len(games) == 1 && !loose:
		m := matchOf(games[0])

		return Result{
			Status: StatusOwned,
			Game:   &m,
		}, true
	}

	sorted := slices.Clone(games)
	slices.SortFunc(sorted, func(a, b *game.Game) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Title()), strings.ToLower(b.Title())), cmp.Compare(a.ID(), b.ID()))
	})

	sorted = sorted[:min(len(sorted), maxCandidates)]
	candidates := make([]Match, 0, len(sorted))

	for _, g := range sorted {
		candidates = append(candidates, matchOf(g))
	}

	return Result{
		Status:     StatusPossible,
		Candidates: candidates,
	}, true
}

func matchOf(g *game.Game) Match {
	return Match{
		ID:     g.ID(),
		Title:  g.Title(),
		Copies: currentCopies(g),
	}
}

// currentCopies summarizes the copies the user still has (game.Copy.Held).
func currentCopies(g *game.Game) []Copy {
	copies := g.Copies()
	out := make([]Copy, 0, len(copies))

	for _, c := range copies {
		if !c.Held() {
			continue
		}

		out = append(out, Copy{
			Platform:   c.Platform,
			System:     c.System(),
			Kind:       c.Kind,
			PendingKey: c.IsPendingKey(),
		})
	}

	return out
}

// linkKey is the index key of a store link: the store's key in lower case and the trimmed id.
func linkKey(store, id string) string {
	return strings.ToLower(strings.TrimSpace(store)) + "\x00" + strings.TrimSpace(id)
}
