package thegamesdb

import (
	"context"
	"errors"
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

// allowance remembers that a key used up its monthly allowance. TheGamesDB then answers every
// request with HTTP 429 and a generic "Unknown Error Code" (seen on 2026-10-10), the same answer as
// to a burst, so on a 429 the provider asks API/Limit, which costs nothing, which of the two it is.
// The state is kept per key: another key pasted in the settings is not blocked, and a Test that
// finds allowance left (extra allowance bought) clears it.
type allowance struct {
	// checking lets one allowance check run at a time, so lookups that hit HTTP 429 together ask
	// API/Limit once: the others find its result when their turn comes.
	checking sync.Mutex

	mu sync.Mutex

	// keys holds what is known about each API key's allowance.
	keys map[string]keyAllowance
}

// keyAllowance is what is known about one API key's allowance.
type keyAllowance struct {
	// until is when the used-up allowance renews; before it, no request with the key is sent.
	until time.Time

	// checked is when the allowance was last read successfully.
	checked time.Time

	// unknown reports that the last read found the key unknown to TheGamesDB.
	unknown bool
}

// limit is TheGamesDB's answer to API/Limit.
type limit struct {
	// Remaining is nil when the answer lacks it.
	Remaining *int `json:"remaining_monthly_allowance"`

	// Extra is the allowance bought on top of the monthly one.
	Extra int `json:"extra_allowance"`

	// Refresh is the number of seconds until the monthly allowance renews; nil when the answer
	// lacks it.
	Refresh *int `json:"allowance_refresh_timer"`
}

// verdict reads an API/Limit answer: nil with allowance left, ErrNoAllowance when it is used up, and
// ErrUnknownKey for an unknown key, which TheGamesDB answers with no allowance and a zero refresh
// timer (a valid key that used up its allowance still has a refresh timer).
func (l limit) verdict() error {
	switch {
	case l.Remaining == nil, *l.Remaining+l.Extra == 0 && l.Refresh != nil && *l.Refresh == 0:
		return ErrUnknownKey
	case *l.Remaining+l.Extra == 0:
		return ErrNoAllowance
	}

	return nil
}

// renewal is how long until a used-up allowance renews: the refresh timer, or allowanceFallback
// when the answer has none.
func (l limit) renewal() time.Duration {
	if l.Refresh == nil || *l.Refresh <= 0 {
		return allowanceFallback
	}

	return time.Duration(*l.Refresh) * time.Second
}

// usedUp reports whether apiKey's allowance is known to be used up: it returns the error lookups
// answer meanwhile, without sending anything.
func (p *Provider) usedUp(apiKey string) error {
	p.allowance.mu.Lock()
	defer p.allowance.mu.Unlock()

	return p.usedUpLocked(apiKey)
}

func (p *Provider) usedUpLocked(apiKey string) error {
	if left := p.allowance.keys[apiKey].until.Sub(p.clock()); left > 0 {
		return &allowanceError{left: left}
	}

	return nil
}

// note records what API/Limit said about apiKey's allowance, at now.
func (p *Provider) note(apiKey string, lim limit, now time.Time) {
	p.allowance.mu.Lock()
	defer p.allowance.mu.Unlock()

	if p.allowance.keys == nil {
		p.allowance.keys = map[string]keyAllowance{}
	}

	k := keyAllowance{
		checked: now,
		unknown: errors.Is(lim.verdict(), ErrUnknownKey),
	}
	if errors.Is(lim.verdict(), ErrNoAllowance) {
		k.until = now.Add(lim.renewal())
	}

	p.allowance.keys[apiKey] = k
}

// checkAllowance asks TheGamesDB after an HTTP 429 whether apiKey's allowance is used up. If it is,
// no request with that key is sent until it renews, and the returned error says so. An unknown key
// returns errUnknownKeyLimited, still a rate limit, also when a check a moment ago found it.
// Otherwise (allowance left, a failed check, or a check a moment ago that found allowance left) it
// returns nil and the 429 stays a plain rate limit. The caller holds the throttle's turn, so
// lookups queued meanwhile wait for the answer instead of being refused as "busy".
func (p *Provider) checkAllowance(ctx context.Context, apiKey string) error {
	p.allowance.checking.Lock()
	defer p.allowance.checking.Unlock()

	p.allowance.mu.Lock()
	err := p.usedUpLocked(apiKey) // a check that just ran found out
	k := p.allowance.keys[apiKey]
	recent := !k.checked.IsZero() && p.clock().Sub(k.checked) < allowanceRecheck
	p.allowance.mu.Unlock()

	switch {
	case err != nil:
		return err
	case recent && k.unknown: // the verdict of a check a moment ago still holds
		return errUnknownKeyLimited
	case recent:
		return nil
	}

	lim, err := p.limit(ctx, apiKey)
	if err != nil {
		return nil // the check failed: the next 429 checks again
	}

	p.note(apiKey, lim, p.clock())

	if errors.Is(lim.verdict(), ErrUnknownKey) {
		return errUnknownKeyLimited
	}

	return p.usedUp(apiKey)
}

// errUnknownKeyLimited is the answer to a lookup that got HTTP 429 with a key TheGamesDB does not
// know. It matches ErrUnknownKey and also provider.ErrRateLimited: the answer was a 429, which says
// nothing about the game, so no "no cover" marker or sheet may be remembered from it.
var errUnknownKeyLimited error = unknownKeyLimited{}

// unknownKeyLimited is errUnknownKeyLimited's type.
type unknownKeyLimited struct{}

// Error does not name TheGamesDB: the warnings that show it already start with "TheGamesDB: ".
func (unknownKeyLimited) Error() string {
	return "the API key was not recognized: check it in the provider settings"
}

// Is matches ErrUnknownKey and provider.ErrRateLimited.
func (unknownKeyLimited) Is(target error) bool {
	return target == ErrUnknownKey || target == provider.ErrRateLimited
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
	// No provider name: the warnings that show it already start with "TheGamesDB: ".
	return "the API key has used this month's allowance: covers and sheets resume when it renews in " + renewsIn(e.left)
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
