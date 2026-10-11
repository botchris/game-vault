package thegamesdb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"gamevault/internal/domain/provider"
)

// minGap is the shortest time between the end of one request to TheGamesDB and the start of the
// next. Its API documents only a monthly allowance (about 1,000 requests for a free key), not a
// rate; but a library page asking for many console covers at once (the media service runs up to
// six lookups in parallel) was answered HTTP 429 on 2026-10-10. One request at a time, at most two
// a second, stays far below such a burst while a page of new editions still fills in quickly. These
// requests are on demand (a cover or a game sheet the user is looking at), never unattended, so they
// follow no fixed rhythm and need no jitter.
const minGap = 500 * time.Millisecond

// maxWait is the longest a lookup waits for its turn. A cover or a sheet that would wait longer is
// answered "rate-limited" at once (no "missing" marker: it is asked again later), so lookups never
// pile up behind TheGamesDB while the user has moved on.
const maxWait = 5 * time.Second

// maxQueue is the most lookups waiting for their turn at once; more are refused the same way. With
// minGap it keeps the queue within maxWait.
const maxQueue = 8

// backoff is how long TheGamesDB is left alone after it answered HTTP 429: lookups meanwhile fail
// fast as rate-limited instead of asking it again and prolonging the limit.
const backoff = 30 * time.Second

// errQueueFull is returned instead of waiting too long for a turn.
var errQueueFull = fmt.Errorf("busy with other lookups: %w", provider.ErrRateLimited)

// throttle sends requests one at a time, each at least gap after the previous one ended, refuses
// those that would wait too long, and backs off after a rate-limit answer.
type throttle struct {
	gap   time.Duration
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	// turn holds a token while a request is under way: whoever puts one in may send.
	turn chan struct{}

	mu sync.Mutex

	// queued counts the callers waiting for their turn or sending.
	queued int

	// next is the earliest time the next request may start.
	next time.Time
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

// do waits for its turn and the gap, then runs request. It returns an error wrapping
// provider.ErrRateLimited without running request when the wait would be too long. A request that
// fails with provider.ErrRateLimited starts the back-off, unless it is provider.ErrQuotaExhausted.
// A nil throttle runs request at once.
func (t *throttle) do(ctx context.Context, request func() error) error {
	if t == nil {
		return request()
	}

	if !t.enqueue() {
		return errQueueFull
	}
	defer t.dequeue()

	select {
	case t.turn <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	defer func() { <-t.turn }()

	t.mu.Lock()
	wait := t.next.Sub(t.now())
	t.mu.Unlock()

	if wait > maxWait { // a back-off started while this caller was queued
		return errQueueFull
	}

	if wait > 0 {
		if err := t.sleep(ctx, wait); err != nil {
			return err
		}
	}

	err := request()

	t.mu.Lock()
	defer t.mu.Unlock()

	t.next = t.now().Add(t.gap)
	// A used-up allowance is no reason to back off: the provider sends nothing more with that key
	// until it renews, and another key must not wait for it.
	if errors.Is(err, provider.ErrRateLimited) && !errors.Is(err, provider.ErrQuotaExhausted) {
		t.next = t.now().Add(backoff)
	}

	return err
}

// enqueue takes a place in the queue, unless it is full or its wait would exceed maxWait.
func (t *throttle) enqueue() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	wait := max(t.next.Sub(t.now()), 0) + time.Duration(t.queued)*t.gap
	if t.queued >= maxQueue || wait > maxWait {
		return false
	}

	t.queued++

	return true
}

func (t *throttle) dequeue() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.queued--
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
