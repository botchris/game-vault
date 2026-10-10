package thegamesdb

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"time"

	"gamevault/internal/domain/provider"
)

// allowanceFallback is how long the allowance is taken as used up when TheGamesDB does not say
// when it renews (no refresh timer). Once it passes, the next lookup asks TheGamesDB again, and
// another HTTP 429 checks the allowance again.
const allowanceFallback = 6 * time.Hour

// allowanceRecheck is the shortest time between two allowance checks after HTTP 429. A key with
// allowance left that keeps getting 429 (a burst) is then left to the throttle's back-off rather
// than checked on every answer.
const allowanceRecheck = 5 * time.Minute

// allowance remembers that the key used up its monthly allowance. TheGamesDB then answers every
// request with HTTP 429 and a generic "Unknown Error Code" (seen on 2026-10-10), the same answer as
// to a burst, so on a 429 the provider asks API/Limit, which costs nothing, which of the two it is.
type allowance struct {
	mu sync.Mutex

	// until is when the used-up allowance renews; before it no request is sent.
	until time.Time

	// checked is when the allowance was last checked after an HTTP 429.
	checked time.Time
}

// limit is TheGamesDB's answer to API/Limit.
type limit struct {
	// Remaining is nil when the answer lacks it.
	Remaining *int `json:"remaining_monthly_allowance"`

	// Extra is the allowance bought on top of the monthly one.
	Extra int `json:"extra_allowance"`

	// Refresh is the number of seconds until the monthly allowance renews.
	Refresh int `json:"allowance_refresh_timer"`
}

// usedUp reports whether the allowance is known to be used up: it returns the error lookups answer
// meanwhile, without sending anything.
func (p *Provider) usedUp() error {
	p.allowance.mu.Lock()
	defer p.allowance.mu.Unlock()

	return p.usedUpLocked()
}

func (p *Provider) usedUpLocked() error {
	if left := p.allowance.until.Sub(p.clock()); left > 0 {
		return &allowanceError{left: left}
	}

	return nil
}

// checkAllowance asks TheGamesDB after an HTTP 429 whether the key's allowance is used up. If it
// is, no request is sent until it renews, and the returned error says so; otherwise (allowance
// left, a failed check, or a check made a moment ago) it returns nil and the 429 stays a plain rate
// limit. The check bypasses the throttle, which is backing off after the 429.
func (p *Provider) checkAllowance(ctx context.Context, apiKey string) error {
	p.allowance.mu.Lock()
	if err := p.usedUpLocked(); err != nil { // another lookup found out meanwhile
		p.allowance.mu.Unlock()
		return err
	}

	now := p.clock()
	if !p.allowance.checked.IsZero() && now.Sub(p.allowance.checked) < allowanceRecheck {
		p.allowance.mu.Unlock()
		return nil
	}

	p.allowance.checked = now
	p.allowance.mu.Unlock()

	lim, err := p.limit(ctx, apiKey)
	if err != nil || lim.Remaining == nil || *lim.Remaining+lim.Extra > 0 {
		return nil
	}

	renews := time.Duration(lim.Refresh) * time.Second
	if renews <= 0 {
		renews = allowanceFallback
	}

	p.allowance.mu.Lock()
	defer p.allowance.mu.Unlock()

	p.allowance.until = now.Add(renews)

	return p.usedUpLocked()
}

// limit reads the key's remaining allowance. That endpoint does not consume it, and it bypasses
// the throttle.
func (p *Provider) limit(ctx context.Context, apiKey string) (limit, error) {
	var out limit

	err := p.getNow(ctx, "/v1/API/Limit", url.Values{"apikey": {apiKey}}, &out)

	return out, err
}

// clock returns the current time; a Provider built without New uses the system clock.
func (p *Provider) clock() time.Time {
	if p.now == nil {
		return time.Now()
	}

	return p.now()
}

// allowanceError is the answer to lookups while the allowance is used up: it says when it renews
// and matches provider.ErrQuotaExhausted (and so provider.ErrRateLimited).
type allowanceError struct {
	left time.Duration
}

func (e *allowanceError) Error() string {
	return "TheGamesDB has used this month's request allowance; it renews in " + renewsIn(e.left)
}

func (e *allowanceError) Unwrap() error { return provider.ErrQuotaExhausted }

// renewsIn describes a wait in round days, hours or minutes ("12 days").
func renewsIn(d time.Duration) string {
	const day = 24 * time.Hour

	switch {
	case d >= 36*time.Hour:
		return fmt.Sprintf("%d days", d.Round(day)/day)
	case d >= 90*time.Minute:
		return fmt.Sprintf("%d hours", d.Round(time.Hour)/time.Hour)
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d minutes", d.Round(time.Minute)/time.Minute)
	}

	return "a minute"
}
