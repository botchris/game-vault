package media

import (
	"context"
	"errors"
	"math/rand/v2"
	gosync "sync"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
)

// How often automatic links are looked for (see AutoLink).
const (
	// autoLinkRetry is how long a game with no link found is left alone.
	autoLinkRetry = 30 * 24 * time.Hour

	// autoLinkLimitedRetry is how long a game waits after a finder asked to slow down.
	autoLinkLimitedRetry = time.Hour

	// autoLinkTimeout bounds the search of one game by the worker.
	autoLinkTimeout = 2 * time.Minute

	// autoLinkLimitedPause is how long, ±20%, the worker rests after a finder asked to slow down
	// or failed, before it searches the next game.
	autoLinkLimitedPause = 15 * time.Minute
)

// autoLinkBackoff remembers, in memory, the games whose search a rate limit or a failure
// interrupted: they wait autoLinkLimitedRetry, without recording anything on the game.
type autoLinkBackoff struct {
	mu      gosync.Mutex
	retryAt map[game.ID]time.Time
}

func (b *autoLinkBackoff) waiting(id game.ID, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return now.Before(b.retryAt[id])
}

func (b *autoLinkBackoff) wait(id game.ID, until time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.retryAt == nil {
		b.retryAt = map[game.ID]time.Time{}
	}

	b.retryAt[id] = until
}

// GameLinker is the port that adds automatic links to a game (the catalog): it re-reads the game,
// adds the links it lacks and records the search in one transaction, so an edit made meanwhile is
// never lost, and invalidates the editions whose covers change.
type GameLinker interface {
	// AddAutoLinks adds the links as automatic where the game has none and the user did not reject
	// them, records the search at now, and returns the links added.
	AddAutoLinks(ctx context.Context, id game.ID, links game.Links, now time.Time) (game.Links, error)
}

// SetGameLinker sets the port that saves automatic links. Without one, no links are looked for.
func (s *Service) SetGameLinker(l GameLinker) { s.linker = l }

// needsAutoLink reports whether automatic links are worth looking for: the game has no Steam link,
// the store with the richest sheets and art.
func needsAutoLink(g *game.Game) bool { return g.Links()[game.LinkSteam] == "" }

// hasLinkFinders reports whether links can be looked for and saved: a provider can find them and a
// GameLinker saves them.
func (s *Service) hasLinkFinders() bool {
	if s.linker == nil {
		return false
	}

	for _, impl := range s.impls {
		if _, ok := impl.(LinkFinder); ok {
			return true
		}
	}

	return false
}

// AutoLink looks for the store links of a game that has no Steam link and adds those found without
// doubt as automatic links (game.Game.AddAutoLinks): first from the most trusted finder (IGDB's
// match, by chain order), then from a title search (Steam's only app with that title). A game with
// nothing found is left alone for 30 days, but only when every enabled finder answered: a rate
// limit, an outage or any other failure retries within the hour. Adding links drops
// the game's cached covers and sheet, so the store's fill them in.
func (s *Service) AutoLink(ctx context.Context, id game.ID) error {
	_, err := s.autoLinkReporting(ctx, id)

	return err
}

// autoLinkReporting is AutoLink that also reports whether this search was stopped by a finder
// asking to slow down or failing (limited): a game skipped because it is still waiting is not.
func (s *Service) autoLinkReporting(ctx context.Context, id game.ID) (limited bool, err error) {
	v, err, _ := s.group.Do("autolink:"+string(id), func() (any, error) {
		return s.autoLink(ctx, id)
	})

	limited, _ = v.(bool)

	return limited, err
}

func (s *Service) autoLink(ctx context.Context, id game.ID) (bool, error) {
	if !s.hasLinkFinders() {
		return false, nil
	}

	g, err := s.games.Get(ctx, id)
	if err != nil {
		return false, err
	}

	now := s.now()
	if !s.autoLinkDue(g, now) {
		return false, nil
	}

	found, limited := s.findLinks(ctx, QueryFor(g, ""))
	if limited {
		s.autoLinks.wait(id, now.Add(autoLinkLimitedRetry))
		return true, nil
	}

	// The provider calls above stay outside the transaction; the catalog re-reads, adds and saves
	// the game in one, and drops the covers the links change.
	added, err := s.linker.AddAutoLinks(ctx, id, found, now)
	if err != nil || len(added) == 0 {
		return false, err
	}

	s.log.Info("store links found automatically", "game", g.Title(), "links", added)
	s.misses.forget(id, "*")

	if s.details != nil {
		if err := s.details.Delete(ctx, id); err != nil {
			s.log.Warn("automatic links: dropping the sheet", "game", g.Title(), "error", err)
		}
	}

	return false, nil
}

// autoLinkDue reports whether a game's links should be looked for now: it has no Steam link, is
// not waiting after a rate limit or a failure, and was not searched in the last 30 days.
func (s *Service) autoLinkDue(g *game.Game, now time.Time) bool {
	if !needsAutoLink(g) || s.autoLinks.waiting(g.ID(), now) {
		return false
	}

	t := g.AutoLinkTriedAt()

	return t.IsZero() || now.Sub(t) >= autoLinkRetry
}

// findLinks asks the enabled link finders in chain order and returns the first links found. A
// finder that asks to slow down or fails (an outage, a timeout) stops the search (limited): its
// answer is unknown and the next finders are less reliable, so the search waits for it rather than
// recording "nothing found" for a month. One without its settings is skipped: it would fail the
// same way until the user fixes them, and its status already says so.
func (s *Service) findLinks(ctx context.Context, q CoverQuery) (links game.Links, limited bool) {
	for _, id := range s.order {
		f, ok := s.impls[id].(LinkFinder)
		if !ok {
			continue
		}

		v, err := s.find(ctx, id)
		if err != nil || !v.Enabled() {
			continue
		}

		links, err := f.FindLinks(ctx, q, v.Settings())
		s.noteResult(id, err)

		switch {
		case errors.Is(err, provider.ErrBadSettings):
			continue
		case errors.Is(err, provider.ErrRateLimited):
			return nil, true
		case err != nil:
			s.log.Warn("automatic links: a finder failed, asking again later", "provider", id, "game", q.Title, "error", err)
			return nil, true
		case len(links) > 0:
			return links, false
		}
	}

	return nil, false
}

// autoLinkQueue is the set of games waiting for the automatic-links worker, in arrival order and
// without duplicates.
type autoLinkQueue struct {
	mu     gosync.Mutex
	queued map[game.ID]struct{}
	order  []game.ID

	// ready has a token while the queue may have games, so the worker sleeps until one arrives.
	ready chan struct{}
}

func newAutoLinkQueue() *autoLinkQueue {
	return &autoLinkQueue{
		queued: map[game.ID]struct{}{},
		ready:  make(chan struct{}, 1),
	}
}

// push adds a game unless it is already waiting.
func (q *autoLinkQueue) push(id game.ID) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, ok := q.queued[id]; ok {
		return
	}

	q.queued[id] = struct{}{}
	q.order = append(q.order, id)

	select {
	case q.ready <- struct{}{}:
	default:
	}
}

// pop takes the oldest game, if any.
func (q *autoLinkQueue) pop() (game.ID, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.order) == 0 {
		return "", false
	}

	id := q.order[0]
	q.order = q.order[1:]
	delete(q.queued, id)

	return id, true
}

func (q *autoLinkQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	return len(q.order)
}

// enqueueAutoLink queues a game for the automatic-links worker (RunAutoLinker), when its links
// are due (autoLinkDue). It never blocks: a sheet or a cover is answered first.
func (s *Service) enqueueAutoLink(g *game.Game) {
	if !s.hasLinkFinders() || !s.autoLinkDue(g, s.now()) {
		return
	}

	s.autoLinkQueue.push(g.ID())
}

// AutoLinkQueueLen returns how many games wait for the automatic-links worker.
func (s *Service) AutoLinkQueueLen() int { return s.autoLinkQueue.len() }

// RunAutoLinker is the one worker that looks for automatic links (AutoLink): it takes the queued
// games one at a time, yields to cover downloads, pauses between games for pause to twice pause
// (unattended requests never follow a fixed rhythm) and rests autoLinkLimitedPause after a finder
// asked to slow down or failed. It returns when ctx ends, so it stops with the server.
func (s *Service) RunAutoLinker(ctx context.Context, pause time.Duration) {
	wait := func(d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()

		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			return true
		}
	}

	for {
		id, ok := s.autoLinkQueue.pop()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-s.autoLinkQueue.ready:
				continue
			}
		}

		// Covers being downloaded are what a person is waiting for: links can wait.
		for len(s.slots) > 0 {
			if !wait(pause) {
				return
			}
		}

		delay := pause + time.Duration(rand.Int64N(int64(pause)+1))
		if s.autoLinkOne(ctx, id) {
			delay = varyAutoLink(autoLinkLimitedPause)
		}

		if !wait(delay) {
			return
		}
	}
}

// autoLinkOne searches one game for the worker and reports whether this search was limited. A game
// deleted since it was queued is not an error.
func (s *Service) autoLinkOne(ctx context.Context, id game.ID) bool {
	ctx, cancel := context.WithTimeout(ctx, autoLinkTimeout)
	defer cancel()

	limited, err := s.autoLinkReporting(ctx, id)
	if err != nil && ctx.Err() == nil && !errors.Is(err, game.ErrGameNotFound) {
		s.log.Warn("automatic links", "game", id, "error", err)
	}

	return limited
}

// varyAutoLink varies d by up to 20% either way: the worker's rests never follow a fixed rhythm.
func varyAutoLink(d time.Duration) time.Duration {
	spread := int64(d) / 5

	return d - time.Duration(spread) + time.Duration(rand.Int64N(2*spread+1))
}
