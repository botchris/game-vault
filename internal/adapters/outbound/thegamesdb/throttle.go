package thegamesdb

import (
	"context"
	"time"

	"gamevault/internal/adapters/outbound/ratelimit"
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

// newThrottle returns the throttle of these limits with the given gap, on the given clock (nil for
// the real one).
func newThrottle(gap time.Duration, now func() time.Time, sleep func(context.Context, time.Duration) error) *ratelimit.Throttle {
	return ratelimit.New(ratelimit.Config{
		Gap:      gap,
		AfterEnd: true,
		MaxWait:  maxWait,
		MaxQueue: maxQueue,
		Backoff:  backoff,
		Now:      now,
		Sleep:    sleep,
	})
}
