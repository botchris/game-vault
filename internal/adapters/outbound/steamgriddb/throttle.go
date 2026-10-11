package steamgriddb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"gamevault/internal/domain/provider"
)

// minGap is the shortest time between the end of one request to SteamGridDB and the start of the
// next. Its API documents no rate, and its terms forbid automated use: one request at a time, at
// most one a second, keeps Game Vault far from anything that looks like a crawler. The lookups are
// on demand (a cover the user is looking at, and only after every other provider found nothing), so
// they follow no fixed rhythm and need no jitter.
const minGap = time.Second

// maxWait is the longest a lookup waits for its turn. A cover that would wait longer is answered
// "rate-limited" at once (no "missing" marker: it is asked again later), so a library page full of
// covers no other provider has never queues behind SteamGridDB.
const maxWait = 3 * time.Second

// maxQueue is the most lookups waiting for their turn at once; more are refused the same way. With
// minGap it keeps the queue within maxWait.
const maxQueue = 3

// backoff is how long SteamGridDB is left alone after it answered HTTP 429 or did not answer:
// lookups meanwhile fail fast as rate-limited instead of asking again and prolonging the problem.
const backoff = time.Minute

// errQueueFull is returned instead of waiting too long for a turn.
var errQueueFull = fmt.Errorf("busy with other lookups: %w", provider.ErrRateLimited)

// throttle sends requests one at a time, each at least gap after the previous one ended, refuses
// those that would wait too long, and backs off after a rate-limit answer.
type throttle struct {
	gap   time.Duration
	now   func() time.Time
	sleep func(ctx context.Context, d time.Duration) error

	// maxTurn is the longest a caller waits for the request under way to end: with the client's
	// timeout it bounds a lookup's wait even when SteamGridDB is slow.
	maxTurn time.Duration

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
		gap:     gap,
		now:     now,
		sleep:   sleep,
		maxTurn: maxWait,
		turn:    make(chan struct{}, 1),
	}
}

// do waits for its turn and the gap, then runs request. It returns an error wrapping
// provider.ErrRateLimited without running request when the wait would be too long. A request that
// fails with provider.ErrRateLimited (a 429, an outage) starts the back-off. A nil throttle runs
// request at once.
func (t *throttle) do(ctx context.Context, request func() error) error {
	if t == nil {
		return request()
	}

	if !t.enqueue() {
		return errQueueFull
	}
	defer t.dequeue()

	turnTimer := time.NewTimer(t.maxTurn)
	defer turnTimer.Stop()

	select {
	case t.turn <- struct{}{}:
	case <-turnTimer.C:
		return errQueueFull // the request under way is slow
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
	if errors.Is(err, provider.ErrRateLimited) {
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
