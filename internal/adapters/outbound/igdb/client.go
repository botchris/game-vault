// Package igdb implements a metadata provider and a cover provider backed by IGDB
// (https://www.igdb.com), Twitch's game database. It is free for non-commercial use under the
// Twitch Developer Service Agreement, with the Client ID and Secret of a Twitch application the user
// creates; Game Vault exchanges them for an app access token (OAuth client credentials).
//
// IGDB allows 4 requests per second and 8 open requests at once: a small limiter keeps every
// request of the process within both.
package igdb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

const (
	settingClientID     = "client_id"
	settingClientSecret = "client_secret"

	defaultAPIURL  = "https://api.igdb.com/v4"
	defaultAuthURL = "https://id.twitch.tv"

	// requestGap spaces the requests to stay under IGDB's 4 requests per second.
	requestGap = 250 * time.Millisecond

	// maxOpen is IGDB's limit of requests open at once.
	maxOpen = 8
)

var (
	// ErrNotConfigured means the Client ID or Secret is missing.
	ErrNotConfigured = provider.BadSettings("enter the Client ID and Client Secret of your Twitch application in the provider settings")

	// ErrBadCredentials means Twitch or IGDB rejected the Client ID or Secret.
	ErrBadCredentials = provider.BadSettings("the Twitch application's Client ID or Secret was rejected: check them in the provider settings")
)

// errTooManyRequests is IGDB's (or Twitch's) HTTP 429. Like the other errors here it does not name
// IGDB: the warnings that show it already do.
var errTooManyRequests = fmt.Errorf("HTTP %d: %w", http.StatusTooManyRequests, provider.ErrRateLimited)

// token is an app access token and when to stop using it.
type token struct {
	value   string
	renewAt time.Time
}

// Client calls IGDB with an app access token it asks Twitch for and keeps in memory until shortly
// before it expires. The covers and the details share one Client, so they share its token and its
// limiter.
type Client struct {
	// API calls IGDB (https://api.igdb.com/v4); tests point it at a fake server.
	API *apiclient.Client

	// Auth calls Twitch's OAuth endpoint (https://id.twitch.tv); tests point it at a fake server.
	Auth *apiclient.Client

	// limiter keeps the requests within IGDB's limits.
	limiter *limiter

	// now is the clock.
	now func() time.Time

	mu sync.Mutex

	// tokens holds the access token of each Client ID and Secret (by their hash).
	tokens map[string]token

	// group makes concurrent callers needing a token share one request.
	group singleflight.Group
}

// NewClient returns a client with IGDB's and Twitch's production endpoints.
func NewClient() *Client {
	api := apiclient.New(defaultAPIURL)
	api.HTTP.Timeout = 20 * time.Second
	auth := apiclient.New(defaultAuthURL)
	auth.HTTP.Timeout = 20 * time.Second

	return &Client{
		API:     api,
		Auth:    auth,
		limiter: newLimiter(requestGap, maxOpen, time.Now),
		now:     time.Now,
		tokens:  map[string]token{},
	}
}

// credentials returns the Client ID, the Secret and the key of their cached token.
func credentials(s schema.Settings) (id, secret, key string, err error) {
	id, secret = strings.TrimSpace(s[settingClientID]), strings.TrimSpace(s[settingClientSecret])
	if id == "" || secret == "" {
		return "", "", "", ErrNotConfigured
	}

	// The secret is not kept as a map key in clear: a hash identifies the pair just as well.
	sum := sha256.Sum256([]byte(id + "\x00" + secret))

	return id, secret, hex.EncodeToString(sum[:]), nil
}

// accessToken returns the cached token of the settings' credentials. It asks Twitch for a new one
// when there is none, it is about to expire, or it is stale: the token IGDB just refused (empty when
// none was). Concurrent callers share one request, and a caller holding a refused token that another
// one already replaced gets the replacement without asking again.
func (c *Client) accessToken(ctx context.Context, s schema.Settings, stale string) (string, error) {
	id, secret, key, err := credentials(s)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	tok, ok := c.tokens[key]
	c.mu.Unlock()

	if ok && tok.value != stale && c.now().Before(tok.renewAt) {
		return tok.value, nil
	}

	// The key is a hash of the credentials, so concurrent callers with the same ones share the
	// request; it runs to completion even if the first caller gives up, as the others still wait.
	v, err, _ := c.group.Do(key, func() (any, error) {
		c.mu.Lock()
		tok, ok := c.tokens[key]
		c.mu.Unlock()

		if ok && tok.value != stale && c.now().Before(tok.renewAt) {
			return tok.value, nil // renewed by the request that just finished
		}

		return c.newToken(context.WithoutCancel(ctx), id, secret, key)
	})
	if err != nil {
		return "", err
	}

	return v.(string), nil
}

// newToken asks Twitch for an app access token and caches it under key. The credentials go in a
// form body, never in the URL: a transport error names the URL, and errors reach warnings and logs.
func (c *Client) newToken(ctx context.Context, id, secret, key string) (string, error) {
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}

	err := c.Auth.Do(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/oauth2/token",
		Header: http.Header{"Content-Type": {"application/x-www-form-urlencoded"}},
		Text: url.Values{
			"client_id":     {id},
			"client_secret": {secret},
			"grant_type":    {"client_credentials"},
		}.Encode(),
	}, &out)

	var se *apiclient.StatusError

	switch {
	case apiclient.IsStatus(err, http.StatusTooManyRequests):
		return "", errTooManyRequests
	case errors.As(err, &se) && se.Status >= 500:
		return "", errTwitchDown
	case apiclient.IsStatus(err, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden):
		// Twitch answers 400 "invalid client" for an unknown Client ID and 403 "invalid client
		// secret" for a wrong secret.
		return "", ErrBadCredentials
	case err != nil:
		// Not wrapped: nothing of the request, which carries the secret, may reach the message.
		return "", errTwitchUnreachable
	case out.AccessToken == "":
		return "", errors.New("the access token was missing from Twitch's answer: try again later")
	}

	// Renew shortly before it expires: five minutes early, or halfway for a short-lived token. A
	// missing or zero lifetime counts as an hour rather than asking for a token on every query.
	life := time.Duration(out.ExpiresIn) * time.Second
	if life <= 0 {
		life = time.Hour
	}

	tok := token{
		value:   out.AccessToken,
		renewAt: c.now().Add(life - min(5*time.Minute, life/2)),
	}

	c.mu.Lock()
	c.tokens[key] = tok
	c.mu.Unlock()

	return tok.value, nil
}

// Twitch outages say nothing about a game: like rate limits, nothing is remembered from them (no
// "no cover" marker, no cached sheet), so an outage does not leave editions without covers for days.
var (
	// errTwitchUnreachable means the token request got no answer (no connection, a timeout).
	errTwitchUnreachable = fmt.Errorf("could not reach Twitch to sign in to IGDB: check the connection and try again later: %w", provider.ErrUnavailable)

	// errTwitchDown means Twitch answered the token request with a server error (5xx).
	errTwitchDown = fmt.Errorf("signing in to IGDB failed: Twitch is not answering: try again later: %w", provider.ErrUnavailable)
)

// query sends an Apicalypse query to an IGDB endpoint ("games", "external_games") and decodes the
// answer into out. A token IGDB refuses (it expired early, or was revoked) is renewed once and the
// query sent again.
func (c *Client) query(ctx context.Context, s schema.Settings, endpoint, body string, out any) error {
	refused, err := c.send(ctx, s, endpoint, body, out, "")
	if errors.Is(err, errTokenRefused) {
		_, err = c.send(ctx, s, endpoint, body, out, refused)
		if errors.Is(err, errTokenRefused) {
			// A token Twitch just issued that IGDB still refuses belongs to other credentials.
			return ErrBadCredentials
		}
	}

	return err
}

// errTokenRefused is IGDB's HTTP 401 to an access token.
var errTokenRefused = errors.New("IGDB refused the access token")

// send makes one attempt of query with the current token (a new one if stale is the cached one),
// and returns the token it used.
func (c *Client) send(ctx context.Context, s schema.Settings, endpoint, body string, out any, stale string) (string, error) {
	tok, err := c.accessToken(ctx, s, stale)
	if err != nil {
		return "", err
	}

	id, _, _, _ := credentials(s) // already checked by accessToken

	err = c.limiter.do(ctx, func() error {
		return c.API.Do(ctx, apiclient.Request{
			Method: http.MethodPost,
			Path:   "/" + endpoint,
			Header: http.Header{
				"Client-ID":     {id},
				"Authorization": {"Bearer " + tok},
			},
			Text: body,
		}, out)
	})

	var se *apiclient.StatusError

	switch {
	case err == nil, errors.Is(err, provider.ErrRateLimited), errors.Is(err, apiclient.ErrNotJSON):
		return tok, err
	case !errors.As(err, &se):
		// No answer (no connection, a timeout): it says nothing about the game.
		return tok, fmt.Errorf("IGDB is not answering, try again later (%w): %w", err, provider.ErrUnavailable)
	case se.Status >= 500:
		return tok, fmt.Errorf("IGDB is not answering (HTTP %d): try again later: %w", se.Status, provider.ErrUnavailable)
	}

	switch se.Status {
	case http.StatusUnauthorized:
		return tok, errTokenRefused
	case http.StatusTooManyRequests:
		return tok, errTooManyRequests
	case http.StatusForbidden:
		return tok, ErrBadCredentials
	}

	// IGDB explains a refused query in the body ([{"title":"Syntax Error","cause":"…"}]): keep it so
	// the user can paste it back.
	return tok, fmt.Errorf("HTTP %d: %s: try again later; if it persists, report this message", se.Status, se.Body)
}

// Test checks the credentials with a new access token and one minimal query.
func (c *Client) Test(ctx context.Context, s schema.Settings) error {
	// The cached token, if any, counts as stale: the check must reach Twitch with these credentials.
	_, _, key, err := credentials(s)
	if err != nil {
		return err
	}

	c.mu.Lock()
	cached := c.tokens[key].value
	c.mu.Unlock()

	if _, err := c.accessToken(ctx, s, cached); err != nil {
		return err
	}

	var out []struct {
		ID int64 `json:"id"`
	}

	return c.query(ctx, s, "games", "fields id; limit 1;", &out)
}

// errBusy means the limiter's queue is too long: IGDB would only be asked after a long wait.
var errBusy = fmt.Errorf("too many lookups waiting: %w", provider.ErrRateLimited)

// maxWait is the longest a request waits for its turn; longer queues fail as busy so a library page
// opening many sheets does not hang.
const maxWait = 10 * time.Second

// limiter starts requests at least gap apart and keeps at most a number of them open at once.
type limiter struct {
	gap  time.Duration
	open chan struct{}
	now  func() time.Time

	mu sync.Mutex

	// next is when the next request may start.
	next time.Time
}

func newLimiter(gap time.Duration, open int, now func() time.Time) *limiter {
	return &limiter{
		gap:  gap,
		open: make(chan struct{}, open),
		now:  now,
	}
}

// do runs fn when its turn comes: first an open slot, then a start time at least gap after the
// previous one. Reserving the time only once the slot is held keeps requests that waited for a slot
// from starting in a burst when it frees.
func (l *limiter) do(ctx context.Context, fn func() error) error {
	select {
	case l.open <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	defer func() { <-l.open }()

	l.mu.Lock()
	now := l.now()
	start := l.next

	if start.Before(now) {
		start = now
	}

	wait := start.Sub(now)
	if wait > maxWait {
		l.mu.Unlock()
		return errBusy
	}

	l.next = start.Add(l.gap)
	l.mu.Unlock()

	if wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}

	return fn()
}
