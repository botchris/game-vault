package media

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"gamevault/internal/application/port"
	"gamevault/internal/application/providers"
	"gamevault/internal/application/unattended"
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

	// autoLinkLimitedJitter is the ±fraction autoLinkLimitedPause varies by.
	autoLinkLimitedJitter = 0.2
)

// autoLinkBackoff remembers, in memory, the games whose search a rate limit or a failure
// interrupted: they wait autoLinkLimitedRetry, without recording anything on the game.
type autoLinkBackoff struct {
	mu      sync.Mutex
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

// AutoLinker looks for the store links of games that have none, and adds those found without doubt
// as automatic links: one game at a time, in the background (Run), from a queue fed by the covers,
// the sheets and the details scan.
type AutoLinker struct {
	games     game.Repository
	reg       *providers.Registry
	linker    GameLinker
	details   DetailsStore
	downloads *Downloads
	now       port.Clock
	log       *slog.Logger
	group     singleflight.Group

	// finders reports that a provider can find links (LinkFinder).
	finders bool

	// misses are the covers' "no cover" answers, which links found make stale.
	misses *misses

	// backoff remembers the automatic link searches a rate limit or a failure interrupted.
	backoff autoLinkBackoff

	// queue holds the games waiting for the worker (Run).
	queue *autoLinkQueue

	// rhythm varies and waits the worker's pauses.
	rhythm unattended.Rhythm
}

// NewAutoLinker builds the auto-linker over the link finders in reg. It looks for nothing until
// SetGameLinker gives it the catalog to save links with. Options: WithDetailsStore (a game's sheet
// is dropped when links are added), WithDownloads (the worker yields to them), WithRhythm, WithClock,
// WithLogger.
func NewAutoLinker(games game.Repository, reg *providers.Registry, opts ...Option) *AutoLinker {
	o := newOptions(opts)
	finders := false

	for _, p := range reg.All() {
		if _, ok := p.(LinkFinder); ok {
			finders = true
		}
	}

	return &AutoLinker{
		games:     games,
		reg:       reg,
		details:   o.details,
		downloads: o.downloads,
		now:       o.now,
		log:       o.log,
		finders:   finders,
		misses:    o.misses,
		queue:     newAutoLinkQueue(),
		rhythm:    o.rhythm,
	}
}

// SetGameLinker sets the port that saves automatic links (the catalog, built after the media
// services). Without one, no links are looked for.
func (s *AutoLinker) SetGameLinker(l GameLinker) { s.linker = l }

// needsAutoLink reports whether automatic links are worth looking for: the game has no Steam link,
// the store with the richest sheets and art.
func needsAutoLink(g *game.Game) bool { return g.Links()[game.LinkSteam] == "" }

// hasLinkFinders reports whether links can be looked for and saved: a provider can find them and a
// GameLinker saves them.
func (s *AutoLinker) hasLinkFinders() bool {
	return s.linker != nil && s.finders
}

// AutoLink looks for the store links of a game that has no Steam link and adds those found without
// doubt as automatic links (game.Game.AddAutoLinks): first from the most trusted finder (IGDB's
// match, by chain order), then from a title search (Steam's only app with that title). A game with
// nothing found is left alone for 30 days, but only when every enabled finder answered: a rate
// limit, an outage or any other failure retries within the hour. Adding links drops
// the game's cached covers and sheet, so the store's fill them in.
func (s *AutoLinker) AutoLink(ctx context.Context, id game.ID) error {
	_, err := s.autoLinkReporting(ctx, id)

	return err
}

// autoLinkReporting is AutoLink that also reports whether this search was stopped by a finder
// asking to slow down or failing (limited): a game skipped because it is still waiting is not.
func (s *AutoLinker) autoLinkReporting(ctx context.Context, id game.ID) (limited bool, err error) {
	v, err, _ := s.group.Do("autolink:"+string(id), func() (any, error) {
		return s.autoLink(ctx, id)
	})

	limited, _ = v.(bool)

	return limited, err
}

func (s *AutoLinker) autoLink(ctx context.Context, id game.ID) (bool, error) {
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
		s.backoff.wait(id, now.Add(autoLinkLimitedRetry))
		return true, nil
	}

	// The provider calls above stay outside the transaction; the catalog re-reads, adds and saves
	// the game in one, and drops the covers the links change.
	added, err := s.linker.AddAutoLinks(ctx, id, found, now)
	if err != nil || len(added) == 0 {
		return false, err
	}

	s.log.Info("store links found automatically", "game", g.Title(), "links", added)
	s.misses.forgetGame(id)

	if err := s.details.Delete(ctx, id); err != nil {
		s.log.Warn("automatic links: dropping the sheet", "game", g.Title(), "error", err)
	}

	return false, nil
}

// autoLinkDue reports whether a game's links should be looked for now: it has no Steam link, is
// not waiting after a rate limit or a failure, and was not searched in the last 30 days.
func (s *AutoLinker) autoLinkDue(g *game.Game, now time.Time) bool {
	if !needsAutoLink(g) || s.backoff.waiting(g.ID(), now) {
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
func (s *AutoLinker) findLinks(ctx context.Context, q GameQuery) (links game.Links, limited bool) {
	for _, p := range s.reg.All() {
		f, ok := p.(LinkFinder)
		if !ok {
			continue
		}

		id := p.Descriptor().ID

		v, err := s.reg.Find(ctx, id)
		if err != nil || !v.Enabled() {
			continue
		}

		links, err := f.FindLinks(ctx, q, v.Settings())
		s.reg.ReportResult(id, err)

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

// Enqueue queues a game for the worker (Run), when its links are due (autoLinkDue). It never
// blocks: a sheet or a cover is answered first.
func (s *AutoLinker) Enqueue(g *game.Game) {
	if !s.hasLinkFinders() || !s.autoLinkDue(g, s.now()) {
		return
	}

	s.queue.push(g.ID())
}

// EnqueueDue queues for the worker the games without a Steam link that are due (see AutoLink). It
// only queues, so the details scan that calls it is never held back.
func (s *AutoLinker) EnqueueDue(ctx context.Context) {
	if !s.hasLinkFinders() {
		return
	}

	games, err := s.games.List(ctx)
	if err != nil {
		s.log.Warn("automatic links: listing games", "error", err)
		return
	}

	for _, g := range games {
		s.Enqueue(g)
	}
}

// QueueLen returns how many games wait for the worker.
func (s *AutoLinker) QueueLen() int { return s.queue.len() }

// Run is the one worker that looks for automatic links (AutoLink): it takes the queued
// games one at a time, yields to cover downloads, pauses between games for pause to twice pause
// (unattended requests never follow a fixed rhythm) and rests autoLinkLimitedPause after a finder
// asked to slow down or failed. It returns when ctx ends, so it stops with the server.
func (s *AutoLinker) Run(ctx context.Context, pause time.Duration) {
	wait := func(d time.Duration) bool { return s.rhythm.Wait(ctx, d) }

	for {
		id, ok := s.queue.pop()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-s.queue.ready:
				continue
			}
		}

		// Covers being downloaded are what a person is waiting for: links can wait.
		for s.downloads.Busy() {
			if !wait(pause) {
				return
			}
		}

		delay := s.rhythm.Between(pause, 2*pause)
		if s.autoLinkOne(ctx, id) {
			delay = s.rhythm.Vary(autoLinkLimitedPause, autoLinkLimitedJitter)
		}

		if !wait(delay) {
			return
		}
	}
}

// autoLinkOne searches one game for the worker and reports whether this search was limited. A game
// deleted since it was queued is not an error.
func (s *AutoLinker) autoLinkOne(ctx context.Context, id game.ID) bool {
	ctx, cancel := context.WithTimeout(ctx, autoLinkTimeout)
	defer cancel()

	limited, err := s.autoLinkReporting(ctx, id)
	if err != nil && ctx.Err() == nil && !errors.Is(err, game.ErrGameNotFound) {
		s.log.Warn("automatic links", "game", id, "error", err)
	}

	return limited
}
