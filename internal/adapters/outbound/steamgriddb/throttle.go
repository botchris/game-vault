package steamgriddb

import (
	"context"
	"time"

	"gamevault/internal/adapters/outbound/ratelimit"
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
