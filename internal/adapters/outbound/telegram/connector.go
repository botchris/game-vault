// Package telegram is the Telegram plugin: notifications sent through a bot the user created with
// @BotFather. The bot token is part of every request's URL path, so everything this package
// returns passes through redact: an error, a log line or an API answer never names it.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
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
	settingToken = "token"
	settingChat  = "chat"

	defaultBaseURL = "https://api.telegram.org"

	// botFatherURL is where the user creates a bot and gets its token.
	botFatherURL = "https://t.me/BotFather"

	requestTimeout = 20 * time.Second

	// gap is the time between two messages of one bot: Telegram allows about one a second to a
	// chat, and a bot that goes over it is asked to wait longer, so three seconds keeps well below.
	gap = 3 * time.Second

	// maxMessage and maxCaption are Telegram's limits, in characters after the markup is read.
	maxMessage = 4096
	maxCaption = 1024

	// maxAnswer bounds what is read of an answer: they are a few hundred bytes.
	maxAnswer = 1 << 20

	// maxAlbum is the most photos Telegram takes in one album.
	maxAlbum = 10
)

// Connector sends messages through a Telegram bot.
type Connector struct {
	// API is the Bot API's client; tests point its BaseURL at httptest.
	API *apiclient.Client
}

// NewConnector returns the connector with Telegram's production endpoint, a 20 second timeout and
// no redirects: the API never redirects, and a redirect would carry the token in its URL elsewhere.
func NewConnector() *Connector {
	api := apiclient.New(defaultBaseURL)
	api.HTTP.Timeout = requestTimeout
	api.NoRedirects = true

	return &Connector{API: api}
}

// Descriptor implements notify.Connector.
func (*Connector) Descriptor() connection.TypeDescriptor {
	return connection.TypeDescriptor{
		Type:           "telegram",
		Name:           "Telegram",
		DescriptionKey: "notifications.telegram.description",
		Fields: schema.Fields{
			{
				Key:      settingToken,
				LabelKey: "notifications.telegram.token",
				HelpKey:  "notifications.telegram.tokenHelp",
				HelpURL:  botFatherURL,
				Kind:     schema.FieldSecret,
				Required: true,
			},
			{
				Key:      settingChat,
				LabelKey: "notifications.telegram.chat",
				HelpKey:  "notifications.telegram.chatHelp",
				Kind:     schema.FieldText,
				Required: true,
			},
		},
	}
}

// Gap implements notify.Pacer.
func (*Connector) Gap() time.Duration { return gap }

// Send implements notify.Connector: one image is a photo, two are an album, none is a text message.
func (c *Connector) Send(ctx context.Context, settings schema.Settings, m notify.Message) error {
	token, chat, err := credentials(settings, true)
	if err != nil {
		return err
	}

	err = c.deliver(ctx, token, chat, m)

	return redact(err, token)
}

// Test implements notify.Connector.
func (c *Connector) Test(ctx context.Context, settings schema.Settings, m notify.Message) error {
	return c.Send(ctx, settings, m)
}

// FindChat implements notify.ChatFinder: the chat of the last message someone sent to the bot.
func (c *Connector) FindChat(ctx context.Context, settings schema.Settings) (key, value string, err error) {
	token, _, err := credentials(settings, false)
	if err != nil {
		return "", "", err
	}

	var updates []struct {
		Message *struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
		ChannelPost *struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"channel_post"`
	}

	body, err := json.Marshal(map[string]any{"allowed_updates": []string{"message", "channel_post"}})
	if err != nil {
		return "", "", err
	}

	if err := c.call(ctx, token, "getUpdates", "application/json", body, &updates); err != nil {
		return "", "", redact(err, token)
	}

	for i := len(updates) - 1; i >= 0; i-- {
		switch {
		case updates[i].Message != nil:
			return settingChat, strconv.FormatInt(updates[i].Message.Chat.ID, 10), nil
		case updates[i].ChannelPost != nil:
			return settingChat, strconv.FormatInt(updates[i].ChannelPost.Chat.ID, 10), nil
		}
	}

	return "", "", errkind.Invalidf("Telegram has no message for this bot yet: send a message to the bot (or add it to the group and write there), then press Test again")
}

// credentials reads the settings; the chat is only needed to send.
func credentials(settings schema.Settings, needChat bool) (token, chat string, err error) {
	token = strings.TrimSpace(settings[settingToken])
	chat = strings.TrimSpace(settings[settingChat])

	switch {
	case token == "":
		return "", "", permanent("Telegram needs the bot token: create a bot with @BotFather and paste its token")
	case needChat && chat == "":
		return "", "", permanent("Telegram needs the chat: send a message to the bot, then press Test with the chat empty")
	}

	return token, chat, nil
}

// deliver sends m the way its images ask for.
func (c *Connector) deliver(ctx context.Context, token, chat string, m notify.Message) error {
	if len(m.Images) == 0 {
		body, err := json.Marshal(map[string]any{
			"chat_id":              chat,
			"text":                 render(m, maxMessage),
			"parse_mode":           "HTML",
			"link_preview_options": map[string]any{"is_disabled": true},
		})
		if err != nil {
			return err
		}

		return c.call(ctx, token, "sendMessage", "application/json", body, nil)
	}

	var err error
	if len(m.Images) == 1 {
		err = c.sendPhoto(ctx, token, chat, m)
	} else {
		err = c.sendAlbum(ctx, token, chat, m)
	}

	// One image Telegram cannot fetch (a dead or unsupported art URL) must not fail the delivery,
	// and with it pause the connection: the news goes as text.
	var bad *mediaError
	if errors.As(err, &bad) {
		m.Images = nil

		return c.deliver(ctx, token, chat, m)
	}

	return err
}

func (c *Connector) sendPhoto(ctx context.Context, token, chat string, m notify.Message) error {
	var form multipartForm

	form.field("chat_id", chat)
	form.field("caption", render(m, maxCaption))
	form.field("parse_mode", "HTML")
	form.image("photo", m.Images[0])

	body, contentType, err := form.finish()
	if err != nil {
		return err
	}

	return c.call(ctx, token, "sendPhoto", contentType, body, nil)
}

func (c *Connector) sendAlbum(ctx context.Context, token, chat string, m notify.Message) error {
	var form multipartForm

	form.field("chat_id", chat)

	images := m.Images[:min(len(m.Images), maxAlbum)]
	media := make([]map[string]any, 0, len(images))

	for i, img := range images {
		item := map[string]any{"type": "photo", "media": form.imageRef("p"+strconv.Itoa(i), img)}
		if i == 0 {
			item["caption"] = render(m, maxCaption)
			item["parse_mode"] = "HTML"
		}

		media = append(media, item)
	}

	list, err := json.Marshal(media)
	if err != nil {
		return err
	}

	form.field("media", string(list))

	body, contentType, err := form.finish()
	if err != nil {
		return err
	}

	return c.call(ctx, token, "sendMediaGroup", contentType, body, nil)
}

// render writes the message as Telegram's HTML: the title in bold, the escaped text and the link.
// limit is Telegram's, counted in characters after the markup is read, so the plain text is cut
// (a last resort: the service shortens lists itself) rather than the markup, which could leave a
// tag open.
func render(m notify.Message, limit int) string {
	title, text := []rune(m.Title), []rune(m.Text)

	label := m.LinkLabel
	if m.Link != "" && label == "" {
		label = m.Link
	}

	// The characters the message has besides the text and the title.
	fixed := 0
	if m.Link != "" {
		fixed += 1 + len([]rune(label))
	}

	if len(title) > 0 {
		fixed++
	}

	if excess := len(title) + len(text) + fixed - limit; excess > 0 {
		// An empty text has nothing to cut: an ellipsis would only add a line.
		if len(text) > 0 {
			keep := max(len(text)-excess-1, 0)
			text = append(text[:keep:keep], '…')
		}

		if len(title)+len(text)+fixed > limit {
			keep := max(limit-fixed-len(text)-1, 0)
			title = append(title[:keep:keep], '…')
		}
	}

	var b strings.Builder

	if len(title) > 0 {
		b.WriteString("<b>" + html.EscapeString(string(title)) + "</b>\n")
	}

	b.WriteString(html.EscapeString(string(text)))

	if m.Link != "" {
		b.WriteString("\n<a href=\"" + html.EscapeString(m.Link) + "\">" + html.EscapeString(label) + "</a>")
	}

	return b.String()
}

// multipartForm builds a multipart body, remembering the first error.
type multipartForm struct {
	buf bytes.Buffer
	w   *multipart.Writer
	err error
}

func (f *multipartForm) writer() *multipart.Writer {
	if f.w == nil {
		f.w = multipart.NewWriter(&f.buf)
	}

	return f.w
}

func (f *multipartForm) field(name, value string) {
	if f.err == nil {
		f.err = f.writer().WriteField(name, value)
	}
}

// image adds an image as field name: an upload for bytes, the URL itself for a public image.
func (f *multipartForm) image(name string, img notify.Image) {
	if ref := f.imageRef(name, img); len(img.Data) == 0 {
		f.field(name, ref)
	}
}

// imageRef returns how a media list or a field refers to the image: attach://name once its bytes
// are added as a file part, or its public URL.
func (f *multipartForm) imageRef(name string, img notify.Image) string {
	if len(img.Data) == 0 {
		return img.URL
	}

	if f.err == nil {
		filename := img.Name
		if filename == "" {
			filename = name + ".jpg"
		}

		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, name, filename))

		contentType := img.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		h.Set("Content-Type", contentType)

		var part io.Writer

		if part, f.err = f.writer().CreatePart(h); f.err == nil {
			_, f.err = part.Write(img.Data)
		}
	}

	return "attach://" + name
}

func (f *multipartForm) finish() ([]byte, string, error) {
	if f.err != nil {
		return nil, "", f.err
	}

	if err := f.writer().Close(); err != nil {
		return nil, "", err
	}

	return f.buf.Bytes(), f.w.FormDataContentType(), nil
}

// answer is the envelope of every Bot API answer.
type answer struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// call posts body to the bot's method and decodes the result into out (nil to ignore it). The
// errors it returns still hold the token (a transport error names the URL): callers redact them.
func (c *Connector) call(ctx context.Context, token, method, contentType string, body []byte, out any) error {
	endpoint := c.API.BaseURL + "/bot" + token + "/" + method

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.API.UserAgent)

	res, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	data, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	if err != nil {
		return err
	}

	var a answer

	decodeErr := json.Unmarshal(data, &a)

	switch {
	case res.StatusCode == http.StatusTooManyRequests:
		return &rateLimitError{
			msg:   "Telegram asks to slow down (" + describe(a.Description) + ")",
			after: time.Duration(a.Parameters.RetryAfter) * time.Second,
		}
	case res.StatusCode >= 500:
		return fmt.Errorf("the Telegram API is not answering (HTTP %d)", res.StatusCode)
	case res.StatusCode >= 400:
		return rejection(res.StatusCode, a.Description)
	case decodeErr != nil || !a.OK:
		return fmt.Errorf("unexpected answer from Telegram (HTTP %d): %s", res.StatusCode, describe(a.Description))
	}

	if out != nil {
		if err := json.Unmarshal(a.Result, out); err != nil {
			return fmt.Errorf("unexpected answer from Telegram: %w", err)
		}
	}

	return nil
}

// httpClient returns the HTTP client to send with, stopping at the first redirect.
func (c *Connector) httpClient() *http.Client {
	hc := *c.API.HTTP
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return &hc
}

// describe returns Telegram's description on one line, or a placeholder.
func describe(d string) string {
	d = strings.Join(strings.Fields(d), " ")
	if d == "" {
		return "no reason given"
	}

	return d
}

// rejection turns a 4xx answer into a permanent error that says what to do.
func rejection(status int, description string) error {
	d := describe(description)

	switch {
	case status == http.StatusUnauthorized || status == http.StatusNotFound:
		return permanent("Telegram rejected the bot token: create a new one with @BotFather (" + d + ")")
	case status == http.StatusBadRequest && badMediaDescription(d):
		return &mediaError{permanentError("Telegram could not use the image (" + d + ")")}
	case status == http.StatusBadRequest && strings.Contains(strings.ToLower(d), "chat not found"):
		return permanent("Telegram cannot find the chat: send a message to the bot, then press Test with the chat empty (" + d + ")")
	case status == http.StatusForbidden:
		return permanent("Telegram does not let the bot write to the chat: start the conversation with the bot again or add it back to the group (" + d + ")")
	case status == http.StatusConflict:
		return permanent("Telegram cannot give the bot's messages while a webhook is set on it: use a bot without one (" + d + ")")
	}

	return permanent(fmt.Sprintf("Telegram rejected the request (HTTP %d): %s", status, d))
}

// mediaError is Telegram refusing an image it could not fetch or read; the message itself may
// still be fine.
type mediaError struct {
	permanentError
}

// badMediaDescription reports whether a 400's description is about the image, not the chat.
func badMediaDescription(d string) bool {
	d = strings.ToLower(d)

	for _, frag := range []string{"wrong file identifier", "http url", "failed to get http url content", "wrong type of the web page content"} {
		if strings.Contains(d, frag) {
			return true
		}
	}

	return false
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

// rateLimitError is Telegram's flood limit; the dispatcher waits RetryAfter before the next try.
type rateLimitError struct {
	msg   string
	after time.Duration
}

// Error implements error.
func (e *rateLimitError) Error() string { return e.msg }

// RetryAfter returns how long Telegram asked to wait, 0 when it did not say.
func (e *rateLimitError) RetryAfter() time.Duration { return e.after }

// redacted is an error whose text lacks the bot token. It matches what the original matched
// (context.Canceled, the sentinels) but does not expose it to errors.As: a *url.Error in it names
// the URL, and so the token.
type redacted struct {
	msg      string
	original error
}

// Error implements error.
func (e *redacted) Error() string { return e.msg }

// Is reports whether the original error matches target.
func (e *redacted) Is(target error) bool { return errors.Is(e.original, target) }

// Kind returns the original's kind.
func (e *redacted) Kind() errkind.Kind { return errkind.Of(e.original) }

// As finds target in the original error, except the transport error, whose URL holds the token.
func (e *redacted) As(target any) bool {
	if _, ok := target.(**url.Error); ok {
		return false
	}

	return errors.As(e.original, target)
}

// redact returns err with the token replaced by "…" in its text, in both its raw and its
// path-escaped form. Errors without the token are returned as they are.
func redact(err error, token string) error {
	if err == nil {
		return nil
	}

	msg := err.Error()
	clean := strings.ReplaceAll(strings.ReplaceAll(msg, token, "…"), url.PathEscape(token), "…")

	if clean == msg {
		return err
	}

	return &redacted{
		msg:      clean,
		original: err,
	}
}
