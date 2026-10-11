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
	ErrNotConfigured = errors.New("enter the Client ID and Client Secret of your Twitch application in the provider settings")

	// ErrBadCredentials means Twitch or IGDB rejected the Client ID or Secret.
	ErrBadCredentials = errors.New("the Twitch application's Client ID or Secret was rejected: check them in the provider settings")
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

// accessToken returns the cached token of the settings' credentials, or asks Twitch for one when
// there is none, it is about to expire, or fresh is set.
func (c *Client) accessToken(ctx context.Context, s schema.Settings, fresh bool) (string, error) {
	id, secret, key, err := credentials(s)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	tok, ok := c.tokens[key]
	c.mu.Unlock()

	if ok && !fresh && c.now().Before(tok.renewAt) {
		return tok.value, nil
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}

	err = c.Auth.Do(ctx, apiclient.Request{
		Method: http.MethodPost,
		Path:   "/oauth2/token",
		Query: url.Values{
			"client_id":     {id},
			"client_secret": {secret},
			"grant_type":    {"client_credentials"},
		},
	}, &out)

	switch {
	case apiclient.IsStatus(err, http.StatusTooManyRequests):
		return "", errTooManyRequests
	case apiclient.IsStatus(err, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden):
		// Twitch answers 400 "invalid client" for an unknown Client ID and 403 "invalid client
		// secret" for a wrong secret.
		return "", ErrBadCredentials
	case err != nil:
		return "", fmt.Errorf("asking Twitch for an access token: %w", err)
	case out.AccessToken == "":
		return "", errors.New("the access token was missing from Twitch's answer: try again later")
	}

	// Renew shortly before it expires: five minutes early, or halfway for a short-lived token.
	life := time.Duration(out.ExpiresIn) * time.Second
	tok = token{
		value:   out.AccessToken,
		renewAt: c.now().Add(life - min(5*time.Minute, life/2)),
	}

	c.mu.Lock()
	c.tokens[key] = tok
	c.mu.Unlock()

	return tok.value, nil
}

// forget drops the cached token of the settings' credentials.
func (c *Client) forget(s schema.Settings) {
	if _, _, key, err := credentials(s); err == nil {
		c.mu.Lock()
		delete(c.tokens, key)
		c.mu.Unlock()
	}
}

// query sends an Apicalypse query to an IGDB endpoint ("games", "external_games") and decodes the
// answer into out. A token IGDB refuses (it expired early, or was revoked) is renewed once and the
// query sent again.
func (c *Client) query(ctx context.Context, s schema.Settings, endpoint, body string, out any) error {
	err := c.send(ctx, s, endpoint, body, out, false)
	if errors.Is(err, errTokenRefused) {
		c.forget(s)

		err = c.send(ctx, s, endpoint, body, out, true)
		if errors.Is(err, errTokenRefused) {
			// A token Twitch just issued that IGDB still refuses belongs to other credentials.
			return ErrBadCredentials
		}
	}

	return err
}

// errTokenRefused is IGDB's HTTP 401 to an access token.
var errTokenRefused = errors.New("IGDB refused the access token")

// send makes one attempt of query.
func (c *Client) send(ctx context.Context, s schema.Settings, endpoint, body string, out any, fresh bool) error {
	tok, err := c.accessToken(ctx, s, fresh)
	if err != nil {
		return err
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
	if !errors.As(err, &se) {
		return err
	}

	switch se.Status {
	case http.StatusUnauthorized:
		return errTokenRefused
	case http.StatusTooManyRequests:
		return errTooManyRequests
	case http.StatusForbidden:
		return ErrBadCredentials
	}

	// IGDB explains a refused query in the body ([{"title":"Syntax Error","cause":"…"}]): keep it so
	// the user can paste it back.
	return fmt.Errorf("HTTP %d: %s", se.Status, se.Body)
}

// Test checks the credentials with a new access token and one minimal query.
func (c *Client) Test(ctx context.Context, s schema.Settings) error {
	if _, err := c.accessToken(ctx, s, true); err != nil {
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

// do runs fn when its turn comes.
func (l *limiter) do(ctx context.Context, fn func() error) error {
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

	select {
	case l.open <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	defer func() { <-l.open }()

	return fn()
}
