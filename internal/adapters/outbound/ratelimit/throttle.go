// Package ratelimit paces the requests a plugin sends to one external service: a gap between
// them (with an optional burst), a bound on how many are under way, on how long a caller waits
// and on how many wait, and a rest after the service asks to slow down. The throttles are what
// keep Game Vault within each service's limits and terms, so every provider uses this one.
//
//	th := ratelimit.New(ratelimit.Config{Gap: time.Second, MaxWait: 3 * time.Second, Backoff: time.Minute})
//	err := th.Do(ctx, func() error { return api.Get(ctx, "/games", q, &out) })
//
// Requests made on demand (a cover the user is looking at) need no jitter; unattended loops draw
// their pauses with internal/application/unattended instead.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"gamevault/internal/domain/provider"
)

var (
	// ErrBusy means a caller would wait longer than MaxWait for its turn, or the queue is full: the
	// request is not sent, and the lookup is asked again later.
	ErrBusy = fmt.Errorf("busy with other lookups: %w", provider.ErrRateLimited)

	// ErrResting means the service asked to slow down (or did not answer) a moment ago: no request
	// is sent until the back-off ends.
	ErrResting = fmt.Errorf("resting after the service asked to slow down: %w", provider.ErrRateLimited)
)

// maxHint caps the wait a Retry-After hint can impose, so a broken header never silences a
// provider for days.
const maxHint = time.Hour

// Config sets a throttle's limits. Only Gap is needed; every other zero value means "no limit" or
// the default given.
type Config struct {
	// Gap is the shortest time between the starts of two requests.
	Gap time.Duration

	// AfterEnd counts Gap from the end of the previous request instead of its start, for services
	// that must never see two requests closer than Gap, however slowly they answer.
	AfterEnd bool

	// Burst is how many requests may start at once after a quiet spell (default 1); the average
	// stays one per Gap.
	Burst int

	// Open is how many requests may be under way at once (default 1).
	Open int

	// MaxWait is the longest a caller waits for its turn, from its call to Do: the wait for a slot,
	// the gap and any back-off together. A longer wait is ErrBusy (or ErrResting) as soon as it is
	// known. Zero waits until the caller's context ends.
	MaxWait time.Duration

	// MaxQueue is the most callers waiting or sending at once; more are ErrBusy. Zero is no bound.
	MaxQueue int

	// Backoff is how long no request is sent after one failed with an error BackoffOn accepts.
	// An error with a longer Retry-After hint (a RetryAfter() time.Duration method, as
	// apiclient.StatusError has) rests that long instead, up to an hour. A request whose caller's
	// context ended starts none: its "no answer" is the caller's doing. Zero never backs off.
	Backoff time.Duration

	// BackoffOn reports whether a request's error starts the back-off. The default accepts
	// provider.ErrRateLimited (a 429 or an outage) but not provider.ErrQuotaExhausted: a used-up
	// allowance is the provider's to handle, and another key must not wait for it.
	BackoffOn func(err error) bool

	// Now returns the current time (default time.Now); tests use a fake clock.
	Now func() time.Time

	// Sleep waits for d or until ctx ends, returning ctx's error (default a timer).
	Sleep func(ctx context.Context, d time.Duration) error
}

// Throttle paces requests by its Config. Build it with New; a nil *Throttle runs every request at
// once, so tests can leave it out.
type Throttle struct {
	cfg Config

	// slots holds a token for each request under way.
	slots chan struct{}

	mu sync.Mutex

	// queued counts the callers waiting for their turn or sending.
	queued int

	// tat is the theoretical start of the next request (generic cell rate algorithm): one may
	// start Burst-1 gaps before it.
	tat time.Time

	// restUntil is the end of the back-off.
	restUntil time.Time
}

// New returns a throttle with the limits of cfg.
func New(cfg Config) *Throttle {
	cfg.Burst = max(cfg.Burst, 1)
	cfg.Open = max(cfg.Open, 1)

	if cfg.BackoffOn == nil {
		cfg.BackoffOn = rateLimited
	}

	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}

	return &Throttle{
		cfg:   cfg,
		slots: make(chan struct{}, cfg.Open),
	}
}

// Do waits for the request's turn, then runs it and returns its error. It returns ErrBusy or
// ErrResting (both provider.ErrRateLimited) without running request when the wait would exceed
// MaxWait, and ctx's error when the caller gives up first. A request that fails after ctx ended
// starts no back-off.
func (t *Throttle) Do(ctx context.Context, request func() error) error {
	if t == nil {
		return request()
	}

	// MaxWait bounds the whole wait, from here: the time spent waiting for a slot counts against
	// the wait for the request's start.
	start := t.cfg.Now()

	if err := t.enqueue(); err != nil {
		return err
	}
	defer t.dequeue()

	if err := t.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-t.slots }()

	wait, err := t.reserve(start)
	if err != nil {
		return err
	}

	if wait > 0 {
		if err := t.cfg.Sleep(ctx, wait); err != nil {
			return err
		}
	}

	err = request()
	t.done(err, ctx.Err() != nil)

	return err
}

// Queued returns how many callers are waiting for their turn or sending.
func (t *Throttle) Queued() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.queued
}

// enqueue takes a place in the queue, unless it is full or the wait it promises exceeds MaxWait.
func (t *Throttle) enqueue() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cfg.MaxQueue > 0 && t.queued >= t.cfg.MaxQueue {
		return ErrBusy
	}

	if t.cfg.MaxWait > 0 {
		now := t.cfg.Now()
		ahead := time.Duration(t.queued/t.cfg.Open) * t.cfg.Gap

		if wait := t.wait(now) + ahead; wait > t.cfg.MaxWait {
			return t.refusal(now)
		}
	}

	t.queued++

	return nil
}

func (t *Throttle) dequeue() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.queued--
}

// acquire waits for a free slot, at most MaxWait: a slow request under way must not hold the
// callers behind it until their contexts end.
func (t *Throttle) acquire(ctx context.Context) error {
	var timeout <-chan time.Time

	if t.cfg.MaxWait > 0 {
		timer := time.NewTimer(t.cfg.MaxWait)
		defer timer.Stop()

		timeout = timer.C
	}

	select {
	case t.slots <- struct{}{}:
		return nil
	case <-timeout:
		return ErrBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

// reserve books the request's start and returns how long to wait for it. It refuses when the time
// waited since start plus that wait exceeds MaxWait: a slow request ahead, or a back-off that
// started while the caller was queued, must not stretch a caller's wait beyond the bound.
func (t *Throttle) reserve(start time.Time) (time.Duration, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.cfg.Now()

	wait := t.wait(now)
	if t.cfg.MaxWait > 0 && now.Sub(start)+wait > t.cfg.MaxWait {
		return 0, t.refusal(now)
	}

	t.tat = later(t.tat, now.Add(wait)).Add(t.cfg.Gap)

	return wait, nil
}

// done records the end of a request: the gap after it, and the back-off its error asks for. A
// request whose caller gave up (abandoned) starts no back-off: its error ("no answer") is the
// caller's deadline or cancellation, which says nothing about the service.
func (t *Throttle) done(err error, abandoned bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	end := t.cfg.Now()
	if t.cfg.AfterEnd {
		t.tat = later(t.tat, end.Add(t.cfg.Gap))
	}

	if abandoned || t.cfg.Backoff <= 0 || err == nil || !t.cfg.BackoffOn(err) {
		return
	}

	rest := t.cfg.Backoff

	var hinted interface{ RetryAfter() time.Duration }
	if errors.As(err, &hinted) {
		rest = max(rest, min(hinted.RetryAfter(), maxHint))
	}

	t.restUntil = later(t.restUntil, end.Add(rest))
}

// wait returns how long a request asking now waits for its start. The caller holds mu.
func (t *Throttle) wait(now time.Time) time.Duration {
	earliest := t.tat.Add(-time.Duration(t.cfg.Burst-1) * t.cfg.Gap)

	return max(earliest.Sub(now), t.restUntil.Sub(now), 0)
}

// refusal is the error of a wait that is too long: resting during a back-off, busy otherwise.
func (t *Throttle) refusal(now time.Time) error {
	if now.Before(t.restUntil) {
		return ErrResting
	}

	return ErrBusy
}

// rateLimited is the default BackoffOn.
func rateLimited(err error) bool {
	return errors.Is(err, provider.ErrRateLimited) && !errors.Is(err, provider.ErrQuotaExhausted)
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}

	return b
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
