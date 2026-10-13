package steam

import (
	"context"
	"time"

	"gamevault/internal/adapters/outbound/ratelimit"
)

// Steam's store host (store.steampowered.com) allows about 200 requests per 5 minutes (one every
// 1.5 s) before answering HTTP 429 (see .claude/memory/steam.md). Its requests (the store search for
// covers by title, Battle.net's covers and the link search; the app details of game sheets) share
// one throttle; the art lookups go to the Web API, which has its own higher limits, and are not
// paced: one request at a time, a short burst for the few covers of a page, then one every
// storeGap; a request that would wait longer than storeMaxWait is answered rate-limited at once (no
// "missing" marker: it is asked again later), like TheGamesDB's.
const (
	storeGap     = 1500 * time.Millisecond
	storeBurst   = 10
	storeMaxWait = 5 * time.Second
)

// storeBackoff is how long the store host is left alone after it answered HTTP 429 or did not
// answer: its limit counts over 5 minutes, so asking again at once only prolongs the refusal.
const storeBackoff = time.Minute

// newThrottle returns the store host's throttle with the given gap, on the given clock (nil for the
// real one).
func newThrottle(gap time.Duration, now func() time.Time, sleep func(context.Context, time.Duration) error) *ratelimit.Throttle {
	return ratelimit.New(ratelimit.Config{
		Gap:     gap,
		Burst:   storeBurst,
		MaxWait: storeMaxWait,
		Backoff: storeBackoff,
		Now:     now,
		Sleep:   sleep,
	})
}
