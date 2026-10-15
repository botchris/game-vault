// Package webhook is the webhook plugin: every event posted as one JSON document to an address
// the user gives, so the user's own tools or scripts can react to it. The
// optional header value is a secret: nothing this package returns repeats it, and neither does an
// address, which may hold a token of its own.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/event"
	"gamevault/internal/domain/schema"
)

const (
	settingURL    = "url"
	settingHeader = "header"
	settingSecret = "secret"

	// version is the document's version, sent as "version": a change that breaks receivers
	// raises it.
	version = 1

	// testKind is the event of the sample a Test posts.
	testKind = "test"

	requestTimeout = 20 * time.Second
)

// Connector posts events to a webhook.
type Connector struct {
	// API holds the HTTP client and the User-Agent; the address comes from each connection.
	API *apiclient.Client
}

// NewConnector returns the connector with a 20 second timeout.
func NewConnector() *Connector {
	api := apiclient.New("")
	api.HTTP.Timeout = requestTimeout

	return &Connector{API: api}
}

// Descriptor implements notify.Connector.
func (*Connector) Descriptor() connection.TypeDescriptor {
	return connection.TypeDescriptor{
		Type:           "webhook",
		Name:           "Webhook",
		DescriptionKey: "notifications.webhook.description",
		Fields: schema.Fields{
			{
				Key:      settingURL,
				LabelKey: "notifications.webhook.url",
				HelpKey:  "notifications.webhook.urlHelp",
				Kind:     schema.FieldText,
				Required: true,
			},
			{
				Key:      settingHeader,
				LabelKey: "notifications.webhook.header",
				HelpKey:  "notifications.webhook.headerHelp",
				Kind:     schema.FieldText,
			},
			{
				Key:      settingSecret,
				LabelKey: "notifications.webhook.secret",
				HelpKey:  "notifications.webhook.secretHelp",
				Kind:     schema.FieldSecret,
			},
		},
	}
}

// document is what a receiver gets.
type document struct {
	Version int             `json:"version"`
	Event   string          `json:"event"`
	At      time.Time       `json:"at"`
	Data    json.RawMessage `json:"data"`
	Message docMessage      `json:"message"`
	Images  []string        `json:"images"`
}

// docMessage is the rendered message, for receivers that only want to forward it.
type docMessage struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	Link  string `json:"link"`
}

// Send implements notify.Connector.
func (c *Connector) Send(ctx context.Context, settings schema.Settings, m notify.Message) error {
	doc, err := newDocument(m)
	if err != nil {
		return err
	}

	return c.post(ctx, settings, doc)
}

// Test implements notify.Connector: a "test" event with empty data and the sample message.
func (c *Connector) Test(ctx context.Context, settings schema.Settings, m notify.Message) error {
	m.Event = event.Event{}
	m.Images = nil

	return c.Send(ctx, settings, m)
}

// newDocument builds the document of m. Its data is the payload as the notification codec writes
// it, so a receiver sees the same JSON the queue stores. A message without an event is a test.
func newDocument(m notify.Message) (document, error) {
	doc := document{
		Version: version,
		Event:   testKind,
		At:      time.Now().UTC(),
		Data:    json.RawMessage(`{}`),
		Message: docMessage{
			Title: m.Title,
			Text:  m.Text,
			Link:  m.Link,
		},
		Images: []string{},
	}

	for _, img := range m.Images {
		if img.URL != "" && len(img.Data) == 0 {
			doc.Images = append(doc.Images, img.URL)
		}
	}

	if m.Event.Kind == "" {
		return doc, nil
	}

	coded, err := notify.EncodeEvent(m.Event)
	if err != nil {
		return document{}, err
	}

	var envelope struct {
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal([]byte(coded), &envelope); err != nil {
		return document{}, fmt.Errorf("reading the encoded event: %w", err)
	}

	doc.Event = string(m.Event.Kind)
	doc.At = m.Event.At.UTC()
	doc.Data = envelope.Data

	return doc, nil
}

// post sends the document with the connection's address and header.
func (c *Connector) post(ctx context.Context, settings schema.Settings, doc document) error {
	endpoint := strings.TrimSpace(settings[settingURL])

	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return permanent("the webhook address must start with http:// or https:// and name a host")
	}

	name := strings.TrimSpace(settings[settingHeader])
	secret := settings[settingSecret]

	if name != "" && secret != "" && !validHeader(name, secret) {
		return permanent("the webhook header is not valid: use a plain name such as X-Webhook-Secret and a value without line breaks")
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("encoding the webhook document: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return permanent("the webhook address is not valid: check it")
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.API.UserAgent)

	if name != "" && secret != "" {
		req.Header.Set(name, secret)
	}

	res, err := c.client().Do(req)
	if err != nil {
		return unwrapTransport(err)
	}
	defer res.Body.Close()

	return classify(res)
}

// client returns the HTTP client to send with: redirects are never followed, because a redirect
// would carry the secret header to another address.
func (c *Connector) client() *http.Client {
	hc := *c.API.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return &hc
}

// classify turns an answer into nil (2xx) or the error that says what to do. The body is never
// quoted: receivers echo what they were sent in their error pages.
func classify(res *http.Response) error {
	switch status := res.StatusCode; {
	case status >= 200 && status < 300:
		return nil
	case status >= 300 && status < 400:
		return permanent("the webhook answered with a redirect: use its final address")
	case status == http.StatusTooManyRequests:
		after := time.Duration(0)
		if secs, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && secs > 0 {
			after = time.Duration(secs) * time.Second
		}

		return &rateLimitError{
			msg:   "the webhook asks to slow down (HTTP 429)",
			after: after,
		}
	case status == http.StatusRequestTimeout || status >= 500:
		return fmt.Errorf("the webhook is not answering (HTTP %d)", status)
	default:
		return permanent(fmt.Sprintf("the webhook refused the message (HTTP %d): check its address and the header it expects", status))
	}
}

// unwrapTransport drops the address a transport error names (it may hold a token), keeping the
// cause so errors.Is still sees a canceled context.
func unwrapTransport(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("could not reach the webhook: %w", ue.Err)
	}

	return fmt.Errorf("could not reach the webhook: %w", err)
}

// validHeader reports whether name is an HTTP token and value has no control characters.
func validHeader(name, value string) bool {
	for _, r := range name {
		if r <= ' ' || r >= 0x7f || strings.ContainsRune(`()<>@,;:\"/[]?={}`, r) {
			return false
		}
	}

	return !strings.ContainsAny(value, "\r\n\x00")
}

// permanentError is a failure retrying cannot fix. It is an Invalid error too, so a failed Test
// reaches the API as InvalidArgument and not as an internal error.
type permanentError string

// Error implements error.
func (e permanentError) Error() string { return string(e) }

// Is makes errors.Is(e, notify.ErrPermanent) true.
func (permanentError) Is(target error) bool { return target == notify.ErrPermanent }

// Kind returns errkind.Invalid.
func (permanentError) Kind() errkind.Kind { return errkind.Invalid }

func permanent(msg string) error { return permanentError(msg) }

// rateLimitError is the receiver's rate limit; the dispatcher waits RetryAfter before the next try.
type rateLimitError struct {
	msg   string
	after time.Duration
}

// Error implements error.
func (e *rateLimitError) Error() string { return e.msg }

// RetryAfter returns how long the receiver asked to wait, 0 when it did not say.
func (e *rateLimitError) RetryAfter() time.Duration { return e.after }
