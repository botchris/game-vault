// Package apiclient is the HTTP client plugins share to call the JSON APIs of external services:
// the base URL (so tests point it at httptest), a User-Agent, headers sent with every request, a
// size limit on answers, and errors that keep the HTTP status so a plugin can turn it into its own
// sentinel error ("sign in again…").
//
//	c := apiclient.New("https://api.example.com")
//	var out struct{ Games []game `json:"games"` }
//	err := c.Get(ctx, "/v1/library", url.Values{"page": {"1"}}, &out)
//	if apiclient.IsStatus(err, http.StatusUnauthorized, http.StatusForbidden) {
//		return ErrSignedOut
//	}
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// UserAgent identifies Game Vault to services that do not need to see a launcher or a browser.
const UserAgent = "GameVault/1.0 (+https://github.com/botchris/game-vault)"

// DefaultMaxBody bounds an answer: library lists are large, but never this large.
const DefaultMaxBody = 32 << 20

// Client calls one external JSON API. The zero value is not usable: build it with New and change
// the fields before the first request.
type Client struct {
	// BaseURL is prepended to every path ("https://api.example.com").
	BaseURL string

	// HTTP sends the requests; tests use the httptest server's client.
	HTTP *http.Client

	// UserAgent is sent with every request.
	UserAgent string

	// Header is sent with every request (an API key, an Accept-Language…).
	Header http.Header

	// MaxBody bounds the answers read, in bytes.
	MaxBody int64
}

// New returns a client for the API at baseURL, with a 60 s timeout and Game Vault's User-Agent.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"), HTTP: &http.Client{Timeout: 60 * time.Second},
		UserAgent: UserAgent, Header: http.Header{}, MaxBody: DefaultMaxBody,
	}
}

// Request is one call: Path is relative to BaseURL; Body, when set, is sent as JSON.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   any
}

// StatusError is an answer with an unexpected HTTP status. Body holds the start of the answer,
// which often says why ("Invalid authorization header").
type StatusError struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.URL, e.Status)
}

// ErrNotJSON means the service answered with something else than JSON, usually an HTML page from a
// bot check or a maintenance notice.
var ErrNotJSON = errors.New("the service did not answer with JSON")

// IsStatus reports whether err is a StatusError with one of the statuses.
func IsStatus(err error, statuses ...int) bool {
	var se *StatusError

	return errors.As(err, &se) && slices.Contains(statuses, se.Status)
}

// Get calls GET path with the query and decodes the JSON answer into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: query}, out)
}

// Post sends body as JSON to path and decodes the JSON answer into out (nil to ignore it).
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, Request{Method: http.MethodPost, Path: path, Body: body}, out)
}

// Do sends r and decodes a 2xx JSON answer into out (nil to ignore it). Any other status is a
// *StatusError; a 2xx answer that is not JSON is ErrNotJSON.
func (c *Client) Do(ctx context.Context, r Request, out any) error {
	u := c.BaseURL + r.Path
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}

	var body io.Reader

	if r.Body != nil {
		b, err := json.Marshal(r.Body)
		if err != nil {
			return fmt.Errorf("encoding the request: %w", err)
		}

		body = bytes.NewReader(b)
	}

	method := r.Method
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}

	c.setHeaders(req, r)

	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, c.maxBody()))
	if err != nil {
		return err
	}

	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &StatusError{Method: method, URL: c.BaseURL + r.Path, Status: res.StatusCode, Body: snippet(data)}
	}

	if out == nil {
		return nil
	}

	if ct := res.Header.Get("Content-Type"); ct != "" && !strings.Contains(ct, "json") {
		return ErrNotJSON
	}

	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unexpected answer from %s (the service may have changed): %w", c.BaseURL+r.Path, err)
	}

	return nil
}

func (c *Client) setHeaders(req *http.Request, r Request) {
	req.Header.Set("Accept", "application/json")

	if r.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	for _, h := range []http.Header{c.Header, r.Header} {
		for k, vs := range h {
			req.Header.Del(k)

			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
	}
}

func (c *Client) maxBody() int64 {
	if c.MaxBody > 0 {
		return c.MaxBody
	}

	return DefaultMaxBody
}

// snippet keeps the start of an error answer, on one line.
func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}

	return s
}
