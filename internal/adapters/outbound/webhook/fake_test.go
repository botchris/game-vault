package webhook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const testSecret = "s3cret-value-123"

// call is one request the fake received.
type call struct {
	method string
	path   string
	header http.Header
	body   []byte
}

// fakeHook is a receiver of webhooks.
type fakeHook struct {
	srv *httptest.Server

	mu    sync.Mutex
	calls []call

	// status answers every request (200 when zero).
	status int

	// header is added to every answer.
	header http.Header

	// echo makes the answer's body repeat the Authorization-like header the request carried, as
	// some receivers do in their error pages.
	echo string
}

func newFakeHook(t *testing.T) *fakeHook {
	t.Helper()

	f := &fakeHook{}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

func (f *fakeHook) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, call{
		method: r.Method,
		path:   r.URL.Path,
		header: r.Header.Clone(),
		body:   data,
	})

	for k, v := range f.header {
		w.Header()[k] = v
	}

	if f.status != 0 {
		w.WriteHeader(f.status)
	}

	if f.echo != "" {
		_, _ = w.Write([]byte("rejected " + r.Header.Get(f.echo)))
	}
}

func (f *fakeHook) received() []call {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]call(nil), f.calls...)
}

// url returns the receiver's address.
func (f *fakeHook) url() string { return f.srv.URL + "/hook" }

// connector returns a connector using the fake's client.
func (f *fakeHook) connector() *Connector {
	c := NewConnector()
	c.API.HTTP = f.srv.Client()

	return c
}
