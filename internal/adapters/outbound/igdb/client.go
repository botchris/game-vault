// Package igdb implements a metadata provider and a cover provider backed by IGDB
// (https://www.igdb.com), Twitch's game database. It is free for non-commercial use under the
// Twitch Developer Service Agreement, with the Client ID and Secret of a Twitch application the user
// creates; Game Vault exchanges them for an app access token (OAuth client credentials).
//
// IGDB allows 4 requests per second and 8 open requests at once: one throttle keeps every request
// of the process within both.
package igdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/adapters/outbound/oauthapp"
	"gamevault/internal/adapters/outbound/ratelimit"
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

	// maxWait is the longest a request waits for its turn (an open slot, then its start); longer
	// queues fail as rate-limited so a library page opening many sheets does not hang.
	maxWait = 10 * time.Second

	// backoff is how long IGDB is left alone after it answered HTTP 429 or a server error: asking
	// again at once only prolongs the refusal. Lookups meanwhile are answered rate-limited.
	backoff = 30 * time.Second
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

// Client calls IGDB with an app access token it asks Twitch for and keeps in memory until shortly
// before it expires. The covers and the details share one Client, so they share its token and its
// throttle.
type Client struct {
	// API calls IGDB (https://api.igdb.com/v4); tests point it at a fake server.
	API *apiclient.Client

	// Auth calls Twitch's OAuth endpoint (https://id.twitch.tv); tests point it at a fake server.
	Auth *apiclient.Client

	// throttle keeps the requests within IGDB's limits; nil sends them at once.
	throttle *ratelimit.Throttle

	// now is the clock.
	now func() time.Time

	// tokens holds the access token of each Client ID and Secret.
	tokens *oauthapp.Tokens
}

// NewClient returns a client with IGDB's and Twitch's production endpoints.
func NewClient() *Client {
	api := apiclient.New(defaultAPIURL)
	api.HTTP.Timeout = 20 * time.Second
	auth := apiclient.New(defaultAuthURL)
	auth.HTTP.Timeout = 20 * time.Second

	c := &Client{
		API:      api,
		Auth:     auth,
		throttle: newThrottle(requestGap),
		now:      time.Now,
	}
	c.tokens = &oauthapp.Tokens{Now: func() time.Time { return c.now() }}

	return c
}

// newThrottle returns the throttle of IGDB's limits with the given gap.
func newThrottle(gap time.Duration) *ratelimit.Throttle {
	return ratelimit.New(ratelimit.Config{
		Gap:       gap,
		Open:      maxOpen,
		MaxWait:   maxWait,
		Backoff:   backoff,
		BackoffOn: serviceTrouble,
	})
}

// serviceTrouble reports whether a request's error is IGDB asking to slow down or failing (a 429
// or a 5xx), which starts the throttle's back-off. It sees the request's own error, a
// *apiclient.StatusError whose Retry-After the throttle honors; a request that got no answer does
// not count, since it may only be the caller giving up.
func serviceTrouble(err error) bool {
	var se *apiclient.StatusError

	return errors.As(err, &se) && apiclient.Transient(se.Status)
}

// credentials returns the Client ID, the Secret and the key of their cached token.
func credentials(s schema.Settings) (id, secret, key string, err error) {
	id, secret = strings.TrimSpace(s[settingClientID]), strings.TrimSpace(s[settingClientSecret])
	if id == "" || secret == "" {
		return "", "", "", ErrNotConfigured
	}

	return id, secret, oauthapp.Key(id, secret), nil
}

// fetchToken returns the request of an app access token for the credentials. They go in a form
// body, never in the URL: a transport error names the URL, and errors reach warnings and logs.
func (c *Client) fetchToken(id, secret string) oauthapp.Fetch {
	return func(ctx context.Context) (oauthapp.Token, error) {
		var out struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
		}

		err := c.Auth.Do(ctx, apiclient.Request{
			Method: http.MethodPost,
			Path:   "/oauth2/token",
			Form: url.Values{
				"client_id":     {id},
				"client_secret": {secret},
				"grant_type":    {"client_credentials"},
			},
		}, &out)

		var se *apiclient.StatusError

		switch {
		case apiclient.IsStatus(err, http.StatusTooManyRequests):
			return oauthapp.Token{}, errTooManyRequests
		case errors.As(err, &se) && se.Status >= 500:
			return oauthapp.Token{}, errTwitchDown
		case apiclient.IsStatus(err, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden):
			// Twitch answers 400 "invalid client" for an unknown Client ID and 403 "invalid client
			// secret" for a wrong secret.
			return oauthapp.Token{}, ErrBadCredentials
		case err != nil:
			// Not wrapped: nothing of the request, which carries the secret, may reach the message.
			return oauthapp.Token{}, errTwitchUnreachable
		}

		// A missing lifetime counts as oauthapp.DefaultLifetime rather than asking for a token on
		// every query.
		return oauthapp.Token{
			Value:    out.AccessToken,
			Lifetime: time.Duration(out.ExpiresIn) * time.Second,
		}, nil
	}
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
	id, secret, key, err := credentials(s)
	if err != nil {
		return err
	}

	err = c.tokens.WithRetry(ctx, key, c.fetchToken(id, secret), func(tok string) error {
		return c.send(ctx, id, tok, endpoint, body, out)
	})
	if errors.Is(err, apiclient.ErrTokenRejected) {
		// A token Twitch just issued that IGDB still refuses belongs to other credentials.
		return ErrBadCredentials
	}

	return err
}

// errTokenRefused is IGDB's HTTP 401 to an access token.
var errTokenRefused = fmt.Errorf("IGDB refused the access token: %w", apiclient.ErrTokenRejected)

// send makes one attempt of query with the token.
func (c *Client) send(ctx context.Context, id, tok, endpoint, body string, out any) error {
	err := c.throttle.Do(ctx, func() error {
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
		return err
	case !errors.As(err, &se):
		// No answer (no connection, a timeout): it says nothing about the game.
		return fmt.Errorf("IGDB is not answering, try again later (%w): %w", err, provider.ErrUnavailable)
	case se.Status >= 500:
		return fmt.Errorf("IGDB is not answering (HTTP %d): try again later: %w", se.Status, provider.ErrUnavailable)
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
	return fmt.Errorf("HTTP %d: %s: try again later; if it persists, report this message", se.Status, se.Body)
}

// Test checks the credentials with a new access token and one minimal query.
func (c *Client) Test(ctx context.Context, s schema.Settings) error {
	// The cached token, if any, counts as stale: the check must reach Twitch with these credentials.
	id, secret, key, err := credentials(s)
	if err != nil {
		return err
	}

	c.tokens.Forget(key)

	if _, err := c.tokens.Get(ctx, key, c.fetchToken(id, secret)); err != nil {
		return err
	}

	var out []struct {
		ID int64 `json:"id"`
	}

	return c.query(ctx, s, "games", "fields id; limit 1;", &out)
}
