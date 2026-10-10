package thegamesdb

import (
	"context"
	"time"
)

// minGap is the shortest time between the end of one request to TheGamesDB and the start of the
// next. Its API documents only a monthly allowance (about 1,000 requests for a free key), not a
// rate; but a library page asking for many console covers at once (the media service runs up to
// six lookups in parallel) was answered HTTP 429 on 2026-10-10. One request at a time, at most two
// a second, stays far below such a burst while a page of new editions still fills in quickly. These
// requests are on demand (a cover or a game sheet the user is looking at), never unattended, so they
// follow no fixed rhythm and need no jitter.
const minGap = 500 * time.Millisecond

// throttle sends requests one at a time, each at least gap after the previous one ended.
type throttle struct {
	gap   time.Duration
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	// turn holds a token while a request is under way: whoever puts one in may send.
	turn chan struct{}

	// last is when the previous request ended; only read and written while holding the turn.
	last time.Time
}

// newThrottle returns a throttle on the given clock (time.Now and sleep in production).
func newThrottle(gap time.Duration, now func() time.Time, sleep func(context.Context, time.Duration) error) *throttle {
	return &throttle{
		gap:   gap,
		now:   now,
		sleep: sleep,
		turn:  make(chan struct{}, 1),
	}
}

// do waits for its turn and the gap, then runs request. A nil throttle runs it at once.
func (t *throttle) do(ctx context.Context, request func() error) error {
	if t == nil {
		return request()
	}

	select {
	case t.turn <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	defer func() { <-t.turn }()

	if !t.last.IsZero() {
		if wait := t.last.Add(t.gap).Sub(t.now()); wait > 0 {
			if err := t.sleep(ctx, wait); err != nil {
				return err
			}
		}
	}

	err := request()
	t.last = t.now()

	return err
}

// sleep waits for d, or until ctx is done.
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
