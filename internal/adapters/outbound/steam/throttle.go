package steam

import (
	"context"
	"fmt"
	"time"

	"gamevault/internal/domain/provider"
)

// Steam's store host (store.steampowered.com) allows about 200 requests per 5 minutes (one every
// 1.5 s) before answering HTTP 429 (see .claude/memory/steam.md). Its requests (the store search for
// covers by title, Battle.net's covers and the link search; the app details of game sheets) share one
// throttle; the art lookups go to the Web API, which has its own higher limits, and are not paced: one request at a time, a short burst for the few
// covers of a page, then one every storeGap; a request that would wait longer than storeMaxWait is
// answered rate-limited at once (no "missing" marker: it is asked again later), like TheGamesDB's.
const (
	storeGap     = 1500 * time.Millisecond
	storeBurst   = 10
	storeMaxWait = 5 * time.Second
)

// errBusy is returned instead of waiting too long for a turn.
var errBusy = fmt.Errorf("steam store: busy with other lookups: %w", provider.ErrRateLimited)

// throttle paces requests with a generic cell rate: on average one per gap, with up to burst at once
// after a quiet spell, and one at a time.
type throttle struct {
	gap     time.Duration
	burst   int
	maxWait time.Duration
	now     func() time.Time
	sleep   func(ctx context.Context, d time.Duration) error

	// turn holds a token while a request waits or is under way: one at a time.
	turn chan struct{}

	// tat is the theoretical arrival time of the next request: requests may start burst-1 gaps
	// before it.
	tat time.Time
}

// newThrottle returns a throttle on the given clock (time.Now and sleep in production).
func newThrottle(gap time.Duration, burst int, maxWait time.Duration, now func() time.Time,
	sleep func(context.Context, time.Duration) error) *throttle {
	return &throttle{
		gap:     gap,
		burst:   max(burst, 1),
		maxWait: maxWait,
		now:     now,
		sleep:   sleep,
		turn:    make(chan struct{}, 1),
	}
}

// do runs fn when its turn comes, or fails with errBusy when that would take longer than maxWait. A
// nil throttle runs fn at once.
func (t *throttle) do(ctx context.Context, fn func() error) error {
	if t == nil {
		return fn()
	}

	// Waiting for the turn counts against maxWait too: a slow request ahead must not hold callers
	// until their context ends.
	timer := time.NewTimer(t.maxWait)
	select {
	case t.turn <- struct{}{}:
		timer.Stop()
	case <-timer.C:
		return errBusy
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	}

	defer func() { <-t.turn }()

	now := t.now()

	tat := t.tat
	if tat.Before(now) {
		tat = now
	}

	wait := tat.Add(-time.Duration(t.burst-1) * t.gap).Sub(now)
	if wait > t.maxWait {
		return errBusy
	}

	t.tat = tat.Add(t.gap)

	if wait > 0 {
		if err := t.sleep(ctx, wait); err != nil {
			return err
		}
	}

	return fn()
}

// sleep waits for d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
