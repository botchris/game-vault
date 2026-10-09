package valuation

import (
	"context"
	"errors"
	"sort"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// Scheduler timings (uniform at random, so requests never follow a fixed rhythm).
const (
	tickMin  = 3 * time.Minute
	tickMax  = 7 * time.Minute
	pauseMin = 20 * time.Second
	pauseMax = 60 * time.Second
)

// RunScheduler estimates copies on their own dates until ctx ends: every 3–7 minutes it plans the
// undated copies and estimates the due ones, one at a time.
func (s *Service) RunScheduler(ctx context.Context) {
	for {
		if err := s.sleep(ctx, s.between(tickMin, tickMax)); err != nil {
			return
		}

		if err := s.Tick(ctx); err != nil {
			s.log.Warn("estimating prices", "error", err)
		}
	}
}

type due struct {
	game game.ID
	copy game.ID
	at   time.Time
}

// Tick gives a date within the next 30 days to every priceable copy that has none (so the first
// round spreads over the month), then estimates the copies whose date has passed, oldest first,
// pausing 20–60 seconds between two of them. Nothing happens while no price provider is enabled.
func (s *Service) Tick(ctx context.Context) error {
	views, err := s.enabled(ctx)
	if err != nil || len(views) == 0 {
		return err
	}

	list, err := s.plan(ctx)
	if err != nil {
		return err
	}

	// A provider that fails (down, or showing a bot check) is not asked again in this round: its
	// previous estimates stay and the copies' dates still move.
	skip := map[provider.ID]bool{}

	for i, d := range list {
		if i > 0 {
			if err := s.sleep(ctx, s.between(pauseMin, pauseMax)); err != nil {
				return nil // shutting down
			}
		}

		_, warnings, failed, err := s.estimate(ctx, d.game, d.copy, skip)
		for _, id := range failed {
			skip[id] = true
		}

		if err != nil {
			if !errors.Is(err, ErrChanged) && !errors.Is(err, ErrNotValuable) && !errors.Is(err, game.ErrCopyNotFound) {
				s.log.Warn("estimating a copy's price", "game", d.game, "error", err)
			}
		} else if len(warnings) > 0 {
			s.log.Info("some price sources failed", "game", d.game, "warnings", warnings)
		}
	}

	return nil
}

// plan dates the undated priceable copies and returns the due ones, oldest first.
func (s *Service) plan(ctx context.Context) ([]due, error) {
	var list []due

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		games, err := s.games.List(ctx)
		if err != nil {
			return err
		}

		now := s.now()

		for _, g := range games {
			changed := false

			for _, c := range g.Copies() {
				if !c.Valuable() {
					continue
				}

				at := c.NextValuation
				if at.IsZero() {
					at = now.Add(time.Duration(s.random() * float64(days(firstDays))))
					if err := g.PlanValuation(c.ID, at); err != nil {
						return err
					}

					changed = true

					continue // a new date is never due on the same tick
				}

				if !at.After(now) {
					list = append(list, due{
						game: g.ID(),
						copy: c.ID,
						at:   at,
					})
				}
			}

			if changed {
				if err := s.games.Save(ctx, g); err != nil {
					return err
				}
			}
		}

		return nil
	})

	sort.Slice(list, func(i, j int) bool { return list[i].at.Before(list[j].at) })

	return list, err
}
