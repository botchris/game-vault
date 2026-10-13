// Package apiclient is the HTTP client plugins share to call the JSON APIs of external services:
// the base URL (so tests point it at httptest), a User-Agent, headers sent with every request, a
// size limit on answers, JSON, form and GraphQL requests, and errors that keep the HTTP status so a
// plugin can turn it into its own sentinel error ("sign in again…"). Send returns the whole answer
// for callers that need its headers or the page a redirect ended on.
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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// UserAgent identifies Game Vault to services that do not need to see a launcher or a browser.
const UserAgent = "GameVault/1.0 (+https://github.com/botchris/game-vault)"

// BrowserUserAgent is the User-Agent of a desktop browser, for the stores whose web APIs serve a
// session the user copied from their browser (cookies, an NPSSO): a request that does not look
// like that browser is refused or sent to a bot check.
const BrowserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128 Safari/537.36"

// DefaultMaxBody bounds an answer: library lists are large, but never this large.
const DefaultMaxBody = 32 << 20

// Client calls one external JSON API. The zero value is not usable: build it with New and change
// the fields before the first request.
type Client struct {
	// BaseURL is prepended to every path ("https://api.example.com").
	BaseURL string

	// HTTP sends the requests; tests use the httptest server's client. A client holding a cookie
	// jar (browsersession) can take its place.
	HTTP *http.Client

	// UserAgent is sent with every request.
	UserAgent string

	// Header is sent with every request (an API key, an Accept-Language…). A key with a nil or
	// empty value removes that header instead, the default Accept included. Removing User-Agent
	// makes Go send its own; set UserAgent instead.
	Header http.Header

	// MaxBody bounds the answers read, in bytes. A longer answer is ErrTooLarge.
	MaxBody int64

	// NoRedirects makes a redirect the answer itself instead of following it, for sign-in flows
	// that read the code from its Location. It holds whatever HTTP is set to.
	NoRedirects bool
}

// New returns a client for the API at baseURL, with a 60 s timeout and Game Vault's User-Agent.
func New(baseURL string) *Client {
	return &Client{
		BaseURL:   strings.TrimSuffix(baseURL, "/"),
		HTTP:      &http.Client{Timeout: 60 * time.Second},
		UserAgent: UserAgent,
		Header:    http.Header{},
		MaxBody:   DefaultMaxBody,
	}
}

// Request is one call: Path is relative to BaseURL. At most one of Body, Form and Text is set.
type Request struct {
	// Method is the HTTP method; empty means GET.
	Method string

	// Path is appended to the client's BaseURL.
	Path string

	// Query is encoded into the URL. Errors never show it, so it may hold a key or a code.
	Query url.Values

	// Header is sent with this request only, over the client's (a bearer token, a cookie…). A key
	// with a nil or empty value removes that header, the default Accept and the client's included:
	// a browser's navigation (a sign-in redirect) sends {"Accept": nil}.
	Header http.Header

	// Body, when set, is sent as JSON.
	Body any

	// Form, when set, is sent URL-encoded, as OAuth token endpoints take it.
	Form url.Values

	// Text, when set, is sent as is with Content-Type text/plain: the body of APIs that take a
	// query language rather than JSON (IGDB's Apicalypse).
	Text string
}

// Response is an answer of any status, read whole (up to the client's MaxBody).
type Response struct {
	// Status is the HTTP status code.
	Status int

	// Header holds the answer's headers (a Location, a Set-Cookie…).
	Header http.Header

	// URL is where the request ended after the redirects followed: a sign-in page when a session
	// is no longer valid.
	URL *url.URL

	// Body is the answer.
	Body []byte

	method   string
	endpoint string
}

// StatusError is an answer with an unexpected HTTP status. Body holds the start of the answer,
// which often says why ("Invalid authorization header").
type StatusError struct {
	Method string
	URL    string
	Status int
	Body   string

	// retryAfter is the wait the answer's Retry-After header asked for (0 when none).
	retryAfter time.Duration
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.URL, e.Status)
}

// RetryAfter returns how long the service asked to wait before the next request (its Retry-After
// header, in seconds or as a date), or 0 when it did not say. ratelimit.Throttle rests that long.
func (e *StatusError) RetryAfter() time.Duration { return e.retryAfter }

var (
	// ErrNotJSON means the service answered a 2xx with an HTML page instead of JSON, usually a bot
	// check or a maintenance notice.
	ErrNotJSON = errors.New("the service did not answer with JSON")

	// ErrTokenRejected means a service refused an access token (usually HTTP 401): it expired or
	// was revoked before its time. The shared token caches (oauthapp, oauthsession) renew it once
	// when a call returns an error that wraps it; only a second refusal reaches the user.
	ErrTokenRejected = errors.New("the service rejected the access token")

	// ErrTooLarge means an answer was longer than the client's MaxBody: the library or the page is
	// larger than expected, not a change of the service.
	ErrTooLarge = errors.New("the answer is larger than expected")
)

// IsStatus reports whether err is a StatusError with one of the statuses.
func IsStatus(err error, statuses ...int) bool {
	var se *StatusError

	return errors.As(err, &se) && slices.Contains(statuses, se.Status)
}

// BasicAuth returns the value of an Authorization header carrying an OAuth client's credentials,
// for Request.Header.
func BasicAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// Get calls GET path with the query and decodes the JSON answer into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, Request{
		Method: http.MethodGet,
		Path:   path,
		Query:  query,
	}, out)
}

// Post sends body as JSON to path and decodes the JSON answer into out (nil to ignore it).
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, Request{
		Method: http.MethodPost,
		Path:   path,
		Body:   body,
	}, out)
}

// Do sends r and decodes a 2xx JSON answer into out (nil to ignore it). Any other status is a
// *StatusError; a 2xx HTML page is ErrNotJSON.
func (c *Client) Do(ctx context.Context, r Request, out any) error {
	res, err := c.Send(ctx, r)
	if err != nil {
		return err
	}

	if err := res.Err(); err != nil {
		return err
	}

	if out == nil {
		return nil
	}

	return res.Decode(out)
}

// Send sends r and returns the answer whatever its status, for callers that read more than a JSON
// body: a redirect's Location, the page a session ended on, a status with its own meaning. The
// error is only a failure to get an answer: none (a transport error, its URL without the query),
// or one longer than MaxBody (ErrTooLarge).
func (c *Client) Send(ctx context.Context, r Request) (*Response, error) {
	endpoint := c.BaseURL + r.Path

	u := endpoint
	if len(r.Query) > 0 {
		u += "?" + r.Query.Encode()
	}

	body, err := r.body()
	if err != nil {
		return nil, err
	}

	method := r.Method
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, withoutQuery(err)
	}

	c.setHeaders(req, r)

	res, err := c.client().Do(req)
	if err != nil {
		return nil, withoutQuery(err)
	}
	defer res.Body.Close()

	// One byte more than the limit tells a longer answer from one of exactly the limit.
	data, err := io.ReadAll(io.LimitReader(res.Body, c.maxBody()+1))
	if err != nil {
		return nil, withoutQuery(err)
	}

	if int64(len(data)) > c.maxBody() {
		return nil, fmt.Errorf("answer from %s is larger than %s: %w", endpoint, size(c.maxBody()), ErrTooLarge)
	}

	return &Response{
		Status:   res.StatusCode,
		Header:   res.Header,
		URL:      res.Request.URL,
		Body:     data,
		method:   method,
		endpoint: endpoint,
	}, nil
}

// OK reports whether the status is a 2xx.
func (r *Response) OK() bool { return r.Status >= 200 && r.Status <= 299 }

// Err returns a *StatusError when the status is not a 2xx, nil otherwise.
func (r *Response) Err() error {
	if r.OK() {
		return nil
	}

	return &StatusError{
		Method:     r.method,
		URL:        r.endpoint,
		Status:     r.Status,
		Body:       snippet(r.Body),
		retryAfter: retryAfter(r.Header.Get("Retry-After"), time.Now()),
	}
}

// retryAfter reads a Retry-After header: a number of seconds or an HTTP date. A missing, broken or
// past value is 0.
func retryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}

	if secs, err := strconv.Atoi(v); err == nil {
		return max(time.Duration(secs)*time.Second, 0)
	}

	if at, err := http.ParseTime(v); err == nil {
		return max(at.Sub(now), 0)
	}

	return 0
}

// Decode decodes the JSON body into out, whatever the status. A web page (a bot check, a
// maintenance notice, a sign-in page) is ErrNotJSON.
func (r *Response) Decode(out any) error {
	// Many APIs label JSON as text/plain, so only an HTML page is refused before decoding.
	if strings.Contains(r.Header.Get("Content-Type"), "html") || bytes.HasPrefix(bytes.TrimSpace(r.Body), []byte("<")) {
		return ErrNotJSON
	}

	if err := json.Unmarshal(r.Body, out); err != nil {
		return fmt.Errorf("unexpected answer from %s (the service may have changed): %w", r.endpoint, err)
	}

	return nil
}

// body encodes the request's body, if any.
func (r Request) body() (io.Reader, error) {
	switch {
	case r.Body != nil:
		b, err := json.Marshal(r.Body)
		if err != nil {
			return nil, fmt.Errorf("encoding the request: %w", err)
		}

		return bytes.NewReader(b), nil
	case r.Form != nil:
		return strings.NewReader(r.Form.Encode()), nil
	case r.Text != "":
		return strings.NewReader(r.Text), nil
	}

	return nil, nil
}

func (c *Client) setHeaders(req *http.Request, r Request) {
	req.Header.Set("Accept", "application/json")

	switch {
	case r.Body != nil:
		req.Header.Set("Content-Type", "application/json")
	case r.Form != nil:
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	case r.Text != "":
		req.Header.Set("Content-Type", "text/plain")
	}

	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	// Request headers come last so they override the client's; a key without values only removes.
	for _, h := range []http.Header{c.Header, r.Header} {
		for k, vs := range h {
			req.Header.Del(k)

			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
	}
}

// client returns the HTTP client to send with: HTTP itself, or a copy of it that stops at the
// first redirect when NoRedirects is set (a copy, so the caller's client is left as it is).
func (c *Client) client() *http.Client {
	if !c.NoRedirects {
		return c.HTTP
	}

	hc := *c.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return &hc
}

// size writes a number of bytes the way a person reads it.
func size(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%d MB", n>>20)
	}

	return fmt.Sprintf("%d KB", n>>10)
}

func (c *Client) maxBody() int64 {
	if c.MaxBody > 0 {
		return c.MaxBody
	}

	return DefaultMaxBody
}

// withoutQuery drops the query from the URL a transport error (*url.Error) names: some services
// take their API key in the query, and these errors end up in warnings and logs. The error may
// wrap the transport error (a caller's context, a classification): the wrappers' messages already
// hold the URL, so the result says the same without the query and still matches what err matched.
func withoutQuery(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}

	clean := ue.URL
	if u, perr := url.Parse(ue.URL); perr == nil {
		u.RawQuery, u.Fragment = "", ""
		clean = u.String()
	} else if i := strings.IndexAny(clean, "?#"); i >= 0 {
		clean = clean[:i]
	}

	cleaned := &url.Error{
		Op:  ue.Op,
		URL: clean,
		Err: ue.Err,
	}
	if err == error(ue) {
		return cleaned
	}

	if clean == ue.URL {
		return err
	}

	return &queryless{
		msg:       strings.ReplaceAll(err.Error(), ue.URL, clean),
		original:  err,
		transport: cleaned,
	}
}

// queryless is an error that wraps a transport error, with the query dropped from its URL.
type queryless struct {
	msg       string
	original  error
	transport *url.Error
}

// Error implements error.
func (e *queryless) Error() string { return e.msg }

// Is reports whether the original error matches target, so a classification survives.
func (e *queryless) Is(target error) bool { return errors.Is(e.original, target) }

// As finds target in the original error, except the transport error, whose URL holds the query:
// errors.As then finds the cleaned one through Unwrap.
func (e *queryless) As(target any) bool {
	if _, ok := target.(**url.Error); ok {
		return false
	}

	return errors.As(e.original, target)
}

// Unwrap returns the transport error without the query.
func (e *queryless) Unwrap() error { return e.transport }

// snippet keeps the start of an error answer, on one line.
func snippet(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}

	return s
}
