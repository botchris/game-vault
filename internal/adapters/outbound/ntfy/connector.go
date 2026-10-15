// Package ntfy is the ntfy plugin: push notifications to a topic of ntfy.sh or of a server the
// user runs. The access token, when there is one, travels only in the Authorization header, and
// never in an error.
package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gamevault/internal/adapters/outbound/apiclient"
	"gamevault/internal/application/notify"
	"gamevault/internal/domain/connection"
	"gamevault/internal/domain/errkind"
	"gamevault/internal/domain/schema"
)

const (
	settingServer = "server"
	settingTopic  = "topic"
	settingToken  = "token"

	defaultBaseURL = "https://ntfy.sh"

	requestTimeout = 20 * time.Second

	// maxHeader bounds the encoded Message header: ntfy (and the proxies before it) refuse
	// headers of about four kilobytes, so the text is cut below that.
	maxHeader = 3900

	// maxAnswer bounds what is read of an answer: they are a few hundred bytes.
	maxAnswer = 1 << 20
)

// Connector publishes messages to an ntfy topic.
type Connector struct {
	// API is the client of the default server; tests point its BaseURL at httptest.
	API *apiclient.Client
}

// NewConnector returns the connector with ntfy.sh as the default server and a 20 second timeout.
func NewConnector() *Connector {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = requestTimeout

	return &Connector{API: api}
}

// Descriptor implements notify.Connector.
func (*Connector) Descriptor() connection.TypeDescriptor {
	return connection.TypeDescriptor{
		Type:           "ntfy",
		Name:           "ntfy",
		DescriptionKey: "notifications.ntfy.description",
		Fields: schema.Fields{
			{
				Key:      settingServer,
				LabelKey: "notifications.ntfy.server",
				HelpKey:  "notifications.ntfy.serverHelp",
				Kind:     schema.FieldText,
			},
			{
				Key:      settingTopic,
				LabelKey: "notifications.ntfy.topic",
				HelpKey:  "notifications.ntfy.topicHelp",
				Kind:     schema.FieldText,
				Required: true,
			},
			{
				Key:      settingToken,
				LabelKey: "notifications.ntfy.token",
				HelpKey:  "notifications.ntfy.tokenHelp",
				Kind:     schema.FieldSecret,
			},
		},
	}
}

// Send implements notify.Connector: the first image is attached (its bytes as the upload, a public
// URL as a link), and a server that refuses the attachment gets the message again without it.
func (c *Connector) Send(ctx context.Context, settings schema.Settings, m notify.Message) error {
	endpoint, token, err := c.target(settings)
	if err != nil {
		return err
	}

	err = c.publish(ctx, endpoint, token, m)

	var big *tooLargeError
	if errors.As(err, &big) && len(m.Images) > 0 {
		m.Images = nil

		return c.publish(ctx, endpoint, token, m)
	}

	return err
}

// Test implements notify.Connector: a plain message, so a topic is checked even before any cover
// exists.
func (c *Connector) Test(ctx context.Context, settings schema.Settings, m notify.Message) error {
	m.Images = nil

	return c.Send(ctx, settings, m)
}

// target reads the settings: the topic's URL and the access token.
func (c *Connector) target(settings schema.Settings) (endpoint, token string, err error) {
	topic := strings.TrimSpace(settings[settingTopic])
	if topic == "" {
		return "", "", permanent("ntfy needs the topic: pick a name nobody else would guess and use it here and in the ntfy app")
	}

	base := strings.TrimSpace(settings[settingServer])
	if base == "" {
		base = c.API.BaseURL
	}

	u, perr := url.Parse(base)
	if perr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", permanent("the ntfy server must be an address starting with http:// or https:// (leave it empty for https://ntfy.sh)")
	}

	return strings.TrimRight(base, "/") + "/" + url.PathEscape(topic), strings.TrimSpace(settings[settingToken]), nil
}

// publish sends one PUT.
func (c *Connector) publish(ctx context.Context, endpoint, token string, m notify.Message) error {
	body, header := compose(m)

	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return permanent("the ntfy address is not valid: check the server and the topic")
	}

	for k, v := range header {
		req.Header[k] = v
	}

	req.Header.Set("User-Agent", c.API.UserAgent)

	res, err := c.API.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	if err != nil {
		return err
	}

	return classify(res, data)
}

// compose returns the body and the headers that publish m: a local image is the body and the text
// goes in the Message header, a public image is an Attach header, and without images the text is
// the body.
func compose(m notify.Message) (body []byte, header http.Header) {
	header = http.Header{}

	if m.Title != "" {
		header.Set("Title", encodeHeader(oneLine(m.Title)))
	}

	header.Set("Priority", "default")

	if m.Important {
		header.Set("Priority", "high")
	}

	if m.Link != "" {
		header.Set("Click", m.Link)
	}

	switch {
	case len(m.Images) > 0 && len(m.Images[0].Data) > 0:
		img := m.Images[0]

		name := img.Name
		if name == "" {
			name = "cover.jpg"
		}

		header.Set("Filename", name)

		if img.ContentType != "" {
			header.Set("Content-Type", img.ContentType)
		}

		if m.Text != "" {
			header.Set("Message", fitHeader(strings.NewReplacer("\r\n", `\n`, "\n", `\n`, "\r", `\n`).Replace(m.Text)))
		}

		return img.Data, header
	case len(m.Images) > 0 && m.Images[0].URL != "":
		header.Set("Attach", m.Images[0].URL)
	}

	return []byte(m.Text), header
}

// classify turns an answer into nil (published) or the error that says what to do.
func classify(res *http.Response, data []byte) error {
	reason := describe(data)

	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return permanent("ntfy refused the access: check the access token (and that it may publish to the topic) (" + reason + ")")
	case res.StatusCode == http.StatusNotFound:
		return permanent("ntfy does not know that address: check the topic and the server (" + reason + ")")
	case res.StatusCode == http.StatusRequestEntityTooLarge:
		return &tooLargeError{permanentError("ntfy refused the image as too large (" + reason + ")")}
	case res.StatusCode == http.StatusTooManyRequests:
		after := time.Duration(0)
		if secs, err := strconv.Atoi(strings.TrimSpace(res.Header.Get("Retry-After"))); err == nil && secs > 0 {
			after = time.Duration(secs) * time.Second
		}

		return &rateLimitError{
			msg:   "ntfy asks to slow down (" + reason + ")",
			after: after,
		}
	case res.StatusCode == http.StatusRequestTimeout || res.StatusCode >= 500:
		return fmt.Errorf("the ntfy server is not answering (HTTP %d)", res.StatusCode)
	case res.StatusCode >= 400:
		return permanent(fmt.Sprintf("ntfy rejected the message (HTTP %d): %s", res.StatusCode, reason))
	case res.StatusCode >= 300:
		return fmt.Errorf("the ntfy server answered with a redirect (HTTP %d): use its final address as the server", res.StatusCode)
	}

	// A published message is answered with its JSON (an id): anything else with 200 is a proxy's
	// or a captive portal's page, and the message did not arrive.
	var published struct {
		ID string `json:"id"`
	}

	if err := json.Unmarshal(data, &published); err != nil || published.ID == "" {
		return fmt.Errorf("unexpected answer from the ntfy server (HTTP %d): is the address an ntfy server?", res.StatusCode)
	}

	return nil
}

// describe returns ntfy's error text on one line, or a placeholder.
func describe(data []byte) string {
	var e struct {
		Error string `json:"error"`
	}

	// A body that is not ntfy's JSON (a proxy's page) says nothing worth showing.
	_ = json.Unmarshal(data, &e)

	s := strings.Join(strings.Fields(e.Error), " ")
	if len(s) > 200 {
		s = s[:200]
	}

	if s == "" {
		return "no reason given"
	}

	return s
}

// oneLine joins the lines of s: a header cannot hold a line break.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// encodeHeader returns s as a header value: ntfy reads non-ASCII text only as RFC 2047 encoded
// words, and decodes them.
func encodeHeader(s string) string {
	return mime.QEncoding.Encode("utf-8", s)
}

// fitHeader encodes s for a header and, when that is longer than the limit, cuts its text (never
// the encoded word, which could end in half a character) and ends it with an ellipsis.
func fitHeader(s string) string {
	runes := []rune(s)

	for len(runes) > 0 {
		if enc := encodeHeader(string(runes)); len(enc) <= maxHeader {
			return enc
		}

		// The encoding triples non-ASCII text, so cut a tenth at a time and mark the cut.
		runes = runes[:len(runes)-max(len(runes)/10, 1)]

		if enc := encodeHeader(string(runes) + "…"); len(enc) <= maxHeader {
			return enc
		}
	}

	return ""
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

// tooLargeError is ntfy refusing an attachment; the message itself may still be fine.
type tooLargeError struct {
	permanentError
}

// rateLimitError is ntfy's rate limit; the dispatcher waits RetryAfter before the next try.
type rateLimitError struct {
	msg   string
	after time.Duration
}

// Error implements error.
func (e *rateLimitError) Error() string { return e.msg }

// RetryAfter returns how long ntfy asked to wait, 0 when it did not say.
func (e *rateLimitError) RetryAfter() time.Duration { return e.after }
