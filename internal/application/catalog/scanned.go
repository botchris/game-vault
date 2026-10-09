package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gamevault/internal/domain/game"
)

// MaxScannedCopies is the most copies AddScannedCopies saves in one call.
const MaxScannedCopies = 200

var (
	// ErrInvalidScannedCopies means a request the scan page never sends: too many copies, or a
	// new game without a title.
	ErrInvalidScannedCopies = errors.New("invalid scanned copies")

	// ErrScannedGameGone is the error of a scanned copy whose game was deleted meanwhile.
	ErrScannedGameGone = errors.New("the game no longer exists; choose another one")
)

// ScannedCopy is one box of a scanning session to save.
type ScannedCopy struct {
	// Ref identifies the box on the scanning device; its result carries it back.
	Ref string

	// GameID is the game to add the copy to; empty: a new game titled Title.
	GameID game.ID

	// Title is the new game's title when GameID is empty.
	Title string

	// CoverURL is the new game's chosen cover, if any.
	CoverURL string

	// Details is the copy itself.
	Details game.CopyDetails
}

// ScannedResult is what happened to one ScannedCopy.
type ScannedResult struct {
	// Ref is the ScannedCopy's Ref.
	Ref string

	// GameID is the game the copy was added to; empty when Err is set.
	GameID game.ID

	// CopyID is the new copy; empty when Err is set.
	CopyID game.ID

	// Err says why the copy was not saved; nil when it was.
	Err error
}

// AddScannedCopies saves the boxes of a scanning session in one transaction. Copies for the same
// game are saved together; copies for new games are grouped by their title's match key, so two
// discs of a game you did not have yet make one game with two copies. A copy that cannot be saved
// (its game was deleted, the domain refuses it) fails alone; only a storage error fails the call.
// It returns a result per item, in order, and every game created or changed.
func (s *Service) AddScannedCopies(ctx context.Context, items []ScannedCopy) ([]ScannedResult, []*game.Game, error) {
	if len(items) > MaxScannedCopies {
		return nil, nil, fmt.Errorf("%w: at most %d copies at once", ErrInvalidScannedCopies, MaxScannedCopies)
	}

	for _, it := range items {
		if it.GameID == "" && strings.TrimSpace(it.Title) == "" {
			return nil, nil, fmt.Errorf("%w: a new game needs a title", ErrInvalidScannedCopies)
		}
	}

	var (
		results []ScannedResult
		changed []*game.Game
	)

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		// Start over on every attempt, so a retried transaction reports only what it saved.
		results = make([]ScannedResult, len(items))
		changed = nil
		now := s.now()

		for i, it := range items {
			results[i].Ref = it.Ref
		}

		for _, group := range groupScanned(items) {
			g, err := s.scannedTarget(ctx, items, group, now)
			if err != nil {
				var v *game.ValidationError
				if !errors.Is(err, game.ErrGameNotFound) && !errors.As(err, &v) {
					return err
				}

				if errors.Is(err, game.ErrGameNotFound) {
					err = ErrScannedGameGone
				}

				for _, i := range group {
					results[i].Err = err
				}

				continue
			}

			added := false

			for _, i := range group {
				c, err := g.AddCopy(items[i].Details, now)
				if err != nil {
					results[i].Err = err
					continue
				}

				results[i].GameID, results[i].CopyID = g.ID(), c.ID
				added = true
			}

			if !added {
				continue
			}

			if err := s.games.Save(ctx, g); err != nil {
				return err
			}

			changed = append(changed, g)
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return results, changed, nil
}

// scannedTarget returns the game a group of items goes to: the existing game, or a new one with
// the group's title and the first cover one of its items chose.
func (s *Service) scannedTarget(ctx context.Context, items []ScannedCopy, group []int, now time.Time) (*game.Game, error) {
	first := items[group[0]]
	if first.GameID != "" {
		return s.games.Get(ctx, first.GameID)
	}

	g, err := game.New(first.Title, now)
	if err != nil {
		return nil, err
	}

	for _, i := range group {
		if items[i].CoverURL == "" {
			continue
		}

		if _, err := g.UpdateInfo(game.Info{
			Title:    g.Title(),
			CoverURL: items[i].CoverURL,
		}, now); err != nil {
			return nil, err
		}

		break
	}

	return g, nil
}

// groupScanned groups the items' indexes by the game they go to, in order of first appearance: an
// existing game by its id, a new game by its title's match key.
func groupScanned(items []ScannedCopy) [][]int {
	var groups [][]int

	index := map[string]int{}

	for i, it := range items {
		key := "id:" + string(it.GameID)
		if it.GameID == "" {
			key = "new:" + game.MatchKey(it.Title)
		}

		n, ok := index[key]
		if !ok {
			n = len(groups)
			index[key] = n

			groups = append(groups, nil)
		}

		groups[n] = append(groups[n], i)
	}

	return groups
}
