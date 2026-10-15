package telegram

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Telegram's answers, with invented values. The error bodies follow the Bot API's documentation
// (core.telegram.org/bots/api) of 2026-10-15: every answer carries ok, and failures an error_code
// and a description, plus parameters.retry_after for a flood limit.
const (
	testToken = "123456:TEST-token"

	unauthorized = `{"ok":false,"error_code":401,"description":"Unauthorized"}`
	chatNotFound = `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`
	blocked      = `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`
	flood        = `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`
	webhookSet   = `{"ok":false,"error_code":409,"description":"Conflict: can't use getUpdates method while webhook is active"}`
	sentOK       = `{"ok":true,"result":{"message_id":10,"date":1790000000,"chat":{"id":1001,"type":"private"}}}`
	noUpdates    = `{"ok":true,"result":[]}`
	twoUpdates   = `{"ok":true,"result":[
 {"update_id":1,"message":{"message_id":1,"date":1790000000,"chat":{"id":1001,"type":"private","first_name":"Ana"},"text":"/start"}},
 {"update_id":2,"message":{"message_id":2,"date":1790000100,"chat":{"id":-100777,"type":"supergroup","title":"Friends"},"text":"hi"}}]}`
)

// call is one request the fake received.
type call struct {
	method      string
	contentType string
	json        map[string]any
	fields      map[string]string
	files       map[string][]byte
}

// fakeTelegram reproduces the Bot API's endpoints, with its token check in the path.
type fakeTelegram struct {
	srv *httptest.Server

	mu    sync.Mutex
	calls []call

	// status and body, when set, replace the answer of every request with a valid token.
	status int
	body   string

	// updates answers getUpdates.
	updates string
}

func newFakeTelegram(t *testing.T) *fakeTelegram {
	t.Helper()

	f := &fakeTelegram{updates: twoUpdates}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeTelegram) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")

	token, method, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
	if !ok || token != testToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(unauthorized))

		return
	}

	c := call{
		method:      method,
		contentType: r.Header.Get("Content-Type"),
		fields:      map[string]string{},
		files:       map[string][]byte{},
	}

	mt, params, _ := mime.ParseMediaType(c.contentType)
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(r.Body, params["boundary"])

		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}

			data, _ := io.ReadAll(p)
			if p.FileName() != "" {
				c.files[p.FormName()] = data
			} else {
				c.fields[p.FormName()] = string(data)
			}
		}
	} else {
		_ = json.NewDecoder(r.Body).Decode(&c.json)
	}

	f.calls = append(f.calls, c)

	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))

		return
	}

	if method == "getUpdates" {
		_, _ = w.Write([]byte(f.updates))

		return
	}

	_, _ = w.Write([]byte(sentOK))
}

// fail makes every request with a valid token answer status and body.
func (f *fakeTelegram) fail(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.status, f.body = status, body
}

// received returns the requests so far.
func (f *fakeTelegram) received() []call {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]call(nil), f.calls...)
}

// connector returns a connector pointed at the fake.
func (f *fakeTelegram) connector() *Connector {
	c := NewConnector()
	c.API.BaseURL = f.srv.URL
	c.API.HTTP = f.srv.Client()

	return c
}
