package home

import (
	"context"

	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

// store saves what a refresh gathered for block b, then publishes what is new since the previous
// gathering: releases of the user's series and historical lows of games not owned on PC. The first
// gathering publishes nothing, since everything in it would look new. owned are the games the
// releases were asked for. s.mu is held.
func (s *Service) store(ctx context.Context, gathered Snapshot, b Block, owned []OwnedRef) error {
	prev, _, err := s.snaps.Snapshot(ctx, b)
	if err != nil {
		return err
	}

	gathered.Block, gathered.FetchedAt = b, s.now()
	if err := s.snaps.SaveSnapshot(ctx, gathered); err != nil {
		return err
	}

	if prev.FetchedAt.IsZero() {
		return nil
	}

	if b == BlockDeals {
		s.publishLows(ctx, prev.HistoricalLows, gathered.HistoricalLows)
	} else {
		s.publishReleases(ctx, prev.FromSeries, gathered.FromSeries, owned)
	}

	return nil
}

// publishReleases announces the upcoming releases of next that prev did not have, as long as the
// game that explains them is still owned.
func (s *Service) publishReleases(ctx context.Context, prev, next []game.Release, owned []OwnedRef) {
	known := make(map[string]bool, len(prev))
	for _, r := range prev {
		known[releaseKey(r)] = true
	}

	ids := make(map[game.ID]bool, len(owned))
	for _, o := range owned {
		ids[o.Game] = true
	}

	now := s.now()

	for _, r := range upcoming(next, game.DateOf(now), ids) {
		key := releaseKey(r)
		if known[key] {
			continue
		}

		known[key] = true

		s.events.Publish(ctx, event.New(event.KindReleaseAnnounced, now, event.ReleaseOf(r)))
	}
}

// releaseKey identifies a release across gatherings: the database's id when it gives one (a date
// moved is not a new release), else its title and date.
func releaseKey(r game.Release) string {
	if r.ProviderGameID != "" {
		return "id:" + r.ProviderGameID
	}

	return "title:" + r.Title + "\x00" + string(r.Date)
}

// publishLows tells the historical lows of next that were not lows in prev, leaving out the games
// owned on PC as the page does. The snapshot is already saved, so a failed ownership check is only
// logged: those lows are not told.
func (s *Service) publishLows(ctx context.Context, prev, next []game.Deal) {
	known := make(map[string]bool, len(prev))

	for _, d := range prev {
		if d.HistoricalLow {
			known[dealKey(d)] = true
		}
	}

	var fresh []game.Deal

	for _, d := range next {
		key := dealKey(d)
		if !d.HistoricalLow || known[key] {
			continue
		}

		known[key] = true

		fresh = append(fresh, d)
	}

	if len(fresh) == 0 {
		return
	}

	now := s.now()

	_, lows, err := s.unowned(ctx, nil, current(fresh, now))
	if err != nil {
		s.log.Warn("checking new historical lows against the collection", "error", err)
		return
	}

	for _, d := range lows {
		s.events.Publish(ctx, event.New(event.KindHistoricalLow, now, event.HistoricalLowOf(d)))
	}
}

// dealKey identifies a deal's game across gatherings: the service's id, else its title.
func dealKey(d game.Deal) string {
	if d.ProviderGameID != "" {
		return "id:" + d.ProviderGameID
	}

	return "title:" + d.Title
}

// SeriesReleases returns the upcoming releases of the user's series as last gathered, without
// those whose game left the collection; none when no releases provider is enabled. It implements
// the notification service's ReleaseSource and, like Home, only reads.
func (s *Service) SeriesReleases(ctx context.Context) ([]game.Release, error) {
	views, err := s.enabled(ctx, BlockReleases)
	if err != nil || len(views) == 0 {
		return nil, err
	}

	snap, _, err := s.snaps.Snapshot(ctx, BlockReleases)
	if err != nil {
		return nil, err
	}

	games, err := s.games.List(ctx)
	if err != nil {
		return nil, err
	}

	ids := make(map[game.ID]bool, len(games))
	for _, g := range games {
		ids[g.ID()] = true
	}

	return upcoming(snap.FromSeries, game.DateOf(s.now()), ids), nil
}
