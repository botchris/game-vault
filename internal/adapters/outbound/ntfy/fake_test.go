package ntfy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// ntfy's answers, with invented values. The success body follows docs.ntfy.sh/publish of
// 2026-10-15 (the published message as JSON), the failures its error format: a JSON object with
// code, http and error.
const (
	published    = `{"id":"hwQ2YpKdmg","time":1790000000,"expires":1790043200,"event":"message","topic":"vault-test","message":"hi"}`
	unauthorized = `{"code":40101,"http":401,"error":"unauthorized"}`
	forbidden    = `{"code":40301,"http":403,"error":"forbidden"}`
	notFound     = `{"code":40401,"http":404,"error":"page not found"}`
	tooLarge     = `{"code":41301,"http":413,"error":"attachment too large"}`
	limited      = `{"code":42908,"http":429,"error":"limit reached: too many requests"}`
)

// call is one request the fake received.
type call struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// fakeNtfy reproduces the publish endpoint.
type fakeNtfy struct {
	srv *httptest.Server

	mu    sync.Mutex
	calls []call

	// status and body, when set, answer every request.
	status int
	body   string

	// retryAfter is sent as the Retry-After header of a failure.
	retryAfter string

	// failWithImage answers requests that upload a file (they carry a Filename header) with 413.
	failWithImage bool
}

func newFakeNtfy(t *testing.T) *fakeNtfy {
	t.Helper()

	f := &fakeNtfy{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeNtfy) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, call{
		method: r.Method,
		path:   r.URL.EscapedPath(),
		header: r.Header.Clone(),
		body:   data,
	})

	w.Header().Set("Content-Type", "application/json")

	if f.failWithImage && r.Header.Get("Filename") != "" {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(tooLarge))

		return
	}

	if f.status != 0 {
		if f.retryAfter != "" {
			w.Header().Set("Retry-After", f.retryAfter)
		}

		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(f.body))

		return
	}

	_, _ = w.Write([]byte(published))
}

func (f *fakeNtfy) fail(status int, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.status, f.body = status, body
}

func (f *fakeNtfy) received() []call {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]call(nil), f.calls...)
}

// connector returns a connector whose default server is the fake.
func (f *fakeNtfy) connector() *Connector {
	c := NewConnector()
	c.API.BaseURL = f.srv.URL
	c.API.HTTP = f.srv.Client()

	return c
}
