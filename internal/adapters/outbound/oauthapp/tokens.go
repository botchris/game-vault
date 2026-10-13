// Package oauthapp caches the access tokens of OAuth applications (the client credentials grant:
// IGDB's Twitch app, Epic's launcher, eBay's keys), with one set of rules: a token is renewed
// shortly before it expires, concurrent callers share one token request, and a token the service
// refused is dropped and renewed once.
//
//	var tokens oauthapp.Tokens
//	key := oauthapp.Key(clientID, secret)
//	err := tokens.WithRetry(ctx, key, fetch, func(token string) error {
//		return api.Do(ctx, req(token), &out) // wrap apiclient.ErrTokenRejected on a 401
//	})
//
// The tokens of a user's session (a refresh token that rotates) are internal/adapters/outbound/
// oauthsession's.
package oauthapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"gamevault/internal/adapters/outbound/apiclient"
)

const (
	// DefaultLifetime is how long a token whose endpoint announced no lifetime is kept: asking for
	// one on every request would be worse than an early refusal, which renews it anyway.
	DefaultLifetime = time.Hour

	// renewMargin is how long before its end a token is renewed (at most half its lifetime).
	renewMargin = 5 * time.Minute
)

// Token is an access token a token endpoint issued.
type Token struct {
	// Value is the token itself.
	Value string

	// Lifetime is how long it lives from now (expires_in).
	Lifetime time.Duration

	// Expires is when it ends, for endpoints that say so (expires_at); it wins over Lifetime. With
	// neither, the token lives DefaultLifetime.
	Expires time.Time
}

// Fetch asks the token endpoint for a new token. Its error is returned as is and nothing is
// cached, so it should already say what to do (bad credentials, a passing trouble).
type Fetch func(ctx context.Context) (Token, error)

// Tokens caches one token per key. The zero value is ready to use; it must not be copied after.
type Tokens struct {
	// Now returns the current time (time.Now when nil); tests use a fake clock.
	Now func() time.Time

	mu      sync.Mutex
	entries map[string]entry
	group   singleflight.Group
}

// entry is a cached token and when to renew it.
type entry struct {
	value   string
	renewAt time.Time
}

// Key identifies a set of credentials without keeping the secret in clear as a map key: a hash of
// the parts.
func Key(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return hex.EncodeToString(sum[:])
}

// Get returns the token of key, asking fetch for a new one when there is none or it is about to
// expire. Concurrent callers of one key share one fetch, which runs to its end even when the
// caller that started it gives up (the others still wait for it). A caller whose ctx ends returns
// its error at once, without waiting for the fetch.
func (t *Tokens) Get(ctx context.Context, key string, fetch Fetch) (string, error) {
	if v, ok := t.cached(key); ok {
		return v, nil
	}

	done := t.group.DoChan(key, func() (any, error) {
		if v, ok := t.cached(key); ok {
			return v, nil // renewed by the fetch that just finished
		}

		tok, err := fetch(context.WithoutCancel(ctx))
		if err != nil {
			return "", err
		}

		if tok.Value == "" {
			return "", errors.New("the token endpoint answered without a token: try again later")
		}

		t.put(key, tok)

		return tok.Value, nil
	})

	// A caller that gives up returns at once; the fetch it shares goes on for the others and
	// caches its token.
	select {
	case r := <-done:
		if r.Err != nil {
			return "", r.Err
		}

		return r.Val.(string), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Drop forgets the token of key if it is still value: the service refused it, so the next Get
// fetches a new one. A caller holding a refused token that another caller already replaced
// leaves the replacement alone.
func (t *Tokens) Drop(key, value string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if e, ok := t.entries[key]; ok && e.value == value {
		delete(t.entries, key)
	}
}

// Forget drops whatever token key has, for a check that must reach the token endpoint with the
// credentials (a provider's Test).
func (t *Tokens) Forget(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.entries, key)
}

// WithRetry runs call with the token of key, and once more with a new one when call's error wraps
// apiclient.ErrTokenRejected (it expired or was revoked early). A second refusal is returned as
// is: the credentials themselves are wrong, which the caller turns into its own error.
func (t *Tokens) WithRetry(ctx context.Context, key string, fetch Fetch, call func(token string) error) error {
	var err error

	for range 2 {
		var token string

		token, err = t.Get(ctx, key, fetch)
		if err != nil {
			return err
		}

		err = call(token)
		if !errors.Is(err, apiclient.ErrTokenRejected) {
			return err
		}

		t.Drop(key, token)
	}

	return err
}

// cached returns the token of key while it is not due for renewal.
func (t *Tokens) cached(key string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	e, ok := t.entries[key]
	if !ok || !t.now().Before(e.renewAt) {
		return "", false
	}

	return e.value, true
}

// put caches tok under key, to be renewed renewMargin before its end, or halfway through a short
// life.
func (t *Tokens) put(key string, tok Token) {
	now := t.now()

	life := tok.Lifetime
	if !tok.Expires.IsZero() {
		life = tok.Expires.Sub(now)
	} else if life <= 0 {
		life = DefaultLifetime
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.entries == nil {
		t.entries = map[string]entry{}
	}

	t.entries[key] = entry{
		value:   tok.Value,
		renewAt: now.Add(life - min(renewMargin, life/2)),
	}
}

func (t *Tokens) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}

	return time.Now()
}
