// Package oauthsession keeps the OAuth sessions of sources that sign in with a one-time code (Epic,
// GOG, Xbox, Amazon, PlayStation): the code is exchanged once for a session (a refresh token,
// stored in a schema.FieldState setting) and an access token, which is renewed from the session
// when it expires or the store refuses it.
//
// Refresh tokens often rotate: each refresh spends the old one. Sessions therefore never refreshes
// one session twice at once, always hands the newest session back into the settings it was given
// (for the sync service to save), and remembers which session replaced which, so a caller holding
// a spent refresh token gets its replacement instead of sending it again.
//
// Each store keeps only its exchange, its refresh and its session's shape:
//
//	sessions := &oauthsession.Sessions[session]{
//		CodeField: settingCode, StateField: settingSession, Clean: cleanCode,
//		Exchange: p.exchange, Refresh: p.refresh, RefreshToken: func(s session) string { return s.RefreshToken },
//		ErrNoSession: ErrNoSession, ErrSignedOut: ErrSessionExpired,
//	}
//	func (p *Provider) Prepare(ctx, settings) (source.Settings, error) { return p.sessions.Prepare(ctx, settings) }
//	err := p.sessions.WithRetry(ctx, settings, func(token string) error { … }) // 401 → apiclient.ErrTokenRejected
package oauthsession

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/domain/source"
)

const (
	// DefaultCodeTTL is how long the session of a just-exchanged code is kept, so "Test" and then
	// "Save" can both prepare the same one-time code.
	DefaultCodeTTL = 15 * time.Minute

	// DefaultLifetime is how long an access token whose lifetime the store did not give is used.
	DefaultLifetime = time.Hour

	// renewMargin is how long before its end an access token is renewed (at most half its life).
	renewMargin = time.Minute

	// successorTTL is how long a spent refresh token is remembered with the session that replaced
	// it: long enough for any caller that read the settings before the rotation.
	successorTTL = 24 * time.Hour
)

// Grant is a token endpoint's answer: the session to store and an access token.
type Grant[S any] struct {
	// Session is what the source stores between scans (JSON in the state setting).
	Session S

	// AccessToken authorizes the store's API calls.
	AccessToken string

	// Lifetime is how long AccessToken lives from now (expires_in).
	Lifetime time.Duration

	// Expires is when AccessToken ends, for stores that say so (expires_at); it wins over
	// Lifetime. With neither, it lives DefaultLifetime.
	Expires time.Time
}

// Sessions keeps the sessions of one source type. Set the exported fields before the first call;
// it must not be copied after.
type Sessions[S any] struct {
	// CodeField is the settings key of the pasted one-time code (a schema.FieldSecret).
	CodeField string

	// StateField is the settings key of the stored session (a schema.FieldState).
	StateField string

	// Clean turns what the user pasted (a URL, a JSON page) into the bare code, or says why it is
	// not one. Nil trims spaces and quotes.
	Clean func(pasted string) (string, error)

	// Exchange trades a code for a grant. It says itself whether the store rejected the code
	// (apiclient.Rejected) or had a passing trouble.
	Exchange func(ctx context.Context, code string) (Grant[S], error)

	// Refresh trades a session for a new grant. A rejected session is the store's signed-out
	// error (source.SignedOut); a passing trouble is not.
	Refresh func(ctx context.Context, session S) (Grant[S], error)

	// RefreshToken returns the session's refresh token, which identifies it.
	RefreshToken func(session S) string

	// ErrNoSession is returned when there is neither a code nor a stored session.
	ErrNoSession error

	// ErrSignedOut is returned when the store refuses even a renewed access token.
	ErrSignedOut error

	// CodeTTL is how long an exchanged code's session is kept (DefaultCodeTTL when 0).
	CodeTTL time.Duration

	// Now returns the current time (time.Now when nil); tests use a fake clock.
	Now func() time.Time

	mu sync.Mutex

	// codes holds the sessions of recently exchanged codes, by sha256(code).
	codes map[string]exchanged[S]

	// tokens holds the access token of each session, by sha256(refresh token).
	tokens map[string]accessToken

	// successors holds, by sha256(spent refresh token), the session that replaced it.
	successors map[string]successor[S]

	// refreshing serializes refreshes and exchanges: two at once would spend one rotating refresh
	// token, or one code, twice.
	refreshing sync.Mutex
}

type exchanged[S any] struct {
	session S
	expires time.Time
}

type accessToken struct {
	value   string
	renewAt time.Time
}

type successor[S any] struct {
	session S
	at      time.Time
}

// Prepare implements sync.Preparer for the source: a pasted code is exchanged for a session, which
// is stored in StateField instead of the code (codes work once). Without a new code (none, or the
// UI's placeholder) the stored session is kept, or ErrNoSession returned when there is none.
// Preparing a code exchanged within CodeTTL reuses its session.
func (s *Sessions[S]) Prepare(ctx context.Context, settings source.Settings) (source.Settings, error) {
	out := maps.Clone(settings)
	if out == nil {
		out = source.Settings{}
	}

	delete(out, s.CodeField)

	code, err := s.clean(settings[s.CodeField])
	if err != nil {
		return nil, err
	}

	if code == "" || code == source.SecretPlaceholder {
		if out[s.StateField] == "" {
			return nil, s.ErrNoSession
		}

		return out, nil
	}

	session, err := s.exchange(ctx, code)
	if err != nil {
		return nil, err
	}

	if err := s.store(out, s.newest(session)); err != nil {
		return nil, err
	}

	return out, nil
}

// Token returns an access token for the settings' session, refreshing it when it has none or it
// is about to expire. The newest session (after a refresh, or one that replaced the stored one)
// is written back into settings, for the caller to save.
func (s *Sessions[S]) Token(ctx context.Context, settings source.Settings) (string, error) {
	held, err := s.Current(settings)
	if err != nil {
		return "", err
	}

	newest := s.newest(held)
	if tok, ok := s.cached(newest); ok {
		return tok, s.handBack(settings, held, newest)
	}

	type result struct {
		session S
		token   string
		err     error
	}

	// The refresh runs on its own, so a caller that gives up returns at once while the refresh
	// (which may already have spent the refresh token) still records the session that replaces it.
	done := make(chan result, 1)

	go func() {
		session, token, err := s.refresh(context.WithoutCancel(ctx), held)
		done <- result{session, token, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			return "", r.err
		}

		return r.token, s.handBack(settings, held, r.session)
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Current returns the session stored in settings, or ErrNoSession when there is none (or it has
// no refresh token).
func (s *Sessions[S]) Current(settings source.Settings) (S, error) {
	var session S

	raw := settings[s.StateField]
	if raw == "" || json.Unmarshal([]byte(raw), &session) != nil || s.RefreshToken(session) == "" {
		return session, s.ErrNoSession
	}

	return session, nil
}

// Forget drops a cached access token the store refused, so the next Token refreshes the session.
func (s *Sessions[S]) Forget(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, t := range s.tokens {
		if t.value == token {
			delete(s.tokens, k)
		}
	}
}

// WithRetry runs call with an access token, and once more with a renewed one when call's error
// wraps apiclient.ErrTokenRejected (it expired or was revoked early). A second refusal is
// ErrSignedOut: only then must the user sign in again.
func (s *Sessions[S]) WithRetry(ctx context.Context, settings source.Settings, call func(token string) error) error {
	for range 2 {
		token, err := s.Token(ctx, settings)
		if err != nil {
			return err
		}

		err = call(token)
		if !errors.Is(err, apiclient.ErrTokenRejected) {
			return err
		}

		s.Forget(token)
	}

	return s.ErrSignedOut
}

// exchange returns the session of a code: the one cached from a recent exchange, or a new one.
func (s *Sessions[S]) exchange(ctx context.Context, code string) (S, error) {
	key := hash(code)

	if session, ok := s.exchangedCode(key); ok {
		return session, nil
	}

	// One exchange at a time: "Test" and "Save" of the same code must not both send it.
	s.refreshing.Lock()
	defer s.refreshing.Unlock()

	if session, ok := s.exchangedCode(key); ok {
		return session, nil
	}

	// A code is spent once sent: the exchange runs to its end even when the caller gives up, so a
	// session it brings back is cached for the next attempt.
	grant, err := s.Exchange(context.WithoutCancel(ctx), code)
	if err != nil {
		var zero S
		return zero, err
	}

	if err := s.valid(grant); err != nil {
		var zero S
		return zero, err
	}

	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.prune(now)

	if s.codes == nil {
		s.codes = map[string]exchanged[S]{}
	}

	s.codes[key] = exchanged[S]{
		session: grant.Session,
		expires: now.Add(s.codeTTL()),
	}
	s.remember(grant, now)

	return grant.Session, nil
}

// exchangedCode returns the session of a code exchanged within CodeTTL.
func (s *Sessions[S]) exchangedCode(key string) (S, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, ok := s.codes[key]
	if !ok || !s.now().Before(c.expires) {
		var zero S
		return zero, false
	}

	return c.session, true
}

// refresh returns the newest session of held and an access token for it, refreshing the session
// unless a refresh that ran meanwhile already did.
func (s *Sessions[S]) refresh(ctx context.Context, held S) (S, string, error) {
	s.refreshing.Lock()
	defer s.refreshing.Unlock()

	newest := s.newest(held)
	if tok, ok := s.cached(newest); ok {
		return newest, tok, nil
	}

	grant, err := s.Refresh(ctx, newest)
	if err != nil {
		var zero S
		return zero, "", err
	}

	if err := s.valid(grant); err != nil {
		var zero S
		return zero, "", err
	}

	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.prune(now)
	s.remember(grant, now)

	if old := s.RefreshToken(newest); old != s.RefreshToken(grant.Session) {
		if s.successors == nil {
			s.successors = map[string]successor[S]{}
		}

		s.successors[hash(old)] = successor[S]{
			session: grant.Session,
			at:      now,
		}
	}

	return grant.Session, grant.AccessToken, nil
}

// newest follows the sessions that replaced session, if any.
func (s *Sessions[S]) newest(session S) S {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Bounded: a broken chain must not loop.
	for range len(s.successors) {
		next, ok := s.successors[hash(s.RefreshToken(session))]
		if !ok {
			break
		}

		session = next.session
	}

	return session
}

// cached returns the access token of session while it is not due for renewal.
func (s *Sessions[S]) cached(session S) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tokens[hash(s.RefreshToken(session))]
	if !ok || !s.now().Before(t.renewAt) {
		return "", false
	}

	return t.value, true
}

// remember caches the grant's access token, renewed renewMargin before its end (or halfway
// through a short life). The caller holds mu.
func (s *Sessions[S]) remember(g Grant[S], now time.Time) {
	life := g.Lifetime
	if !g.Expires.IsZero() {
		life = g.Expires.Sub(now)
	} else if life <= 0 {
		life = DefaultLifetime
	}

	if s.tokens == nil {
		s.tokens = map[string]accessToken{}
	}

	s.tokens[hash(s.RefreshToken(g.Session))] = accessToken{
		value:   g.AccessToken,
		renewAt: now.Add(life - min(renewMargin, life/2)),
	}
}

// prune drops the expired codes, access tokens and successors. The caller holds mu.
func (s *Sessions[S]) prune(now time.Time) {
	maps.DeleteFunc(s.codes, func(_ string, c exchanged[S]) bool { return !now.Before(c.expires) })
	maps.DeleteFunc(s.tokens, func(_ string, t accessToken) bool { return !now.Before(t.renewAt) })
	maps.DeleteFunc(s.successors, func(_ string, n successor[S]) bool { return now.Sub(n.at) > successorTTL })
}

// handBack writes newest into settings when it is not the session they hold.
func (s *Sessions[S]) handBack(settings source.Settings, held, newest S) error {
	if s.RefreshToken(held) == s.RefreshToken(newest) && settings[s.StateField] != "" {
		return nil
	}

	return s.store(settings, newest)
}

// store writes session into settings' StateField.
func (s *Sessions[S]) store(settings source.Settings, session S) error {
	raw, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("storing the session: %w", err)
	}

	settings[s.StateField] = string(raw)

	return nil
}

// valid checks a grant before it is kept.
func (s *Sessions[S]) valid(g Grant[S]) error {
	if g.AccessToken == "" || s.RefreshToken(g.Session) == "" {
		return errors.New("the sign-in answer had no token: try again later")
	}

	return nil
}

func (s *Sessions[S]) clean(pasted string) (string, error) {
	if s.Clean != nil {
		return s.Clean(pasted)
	}

	return strings.Trim(pasted, `"' `+"\t\n\r"), nil
}

func (s *Sessions[S]) codeTTL() time.Duration {
	if s.CodeTTL > 0 {
		return s.CodeTTL
	}

	return DefaultCodeTTL
}

func (s *Sessions[S]) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}

	return time.Now()
}

// hash keeps codes and refresh tokens out of the maps' keys in clear.
func hash(v string) string {
	sum := sha256.Sum256([]byte(v))

	return hex.EncodeToString(sum[:])
}
