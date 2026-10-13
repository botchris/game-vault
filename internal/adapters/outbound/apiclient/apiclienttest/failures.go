// Package apiclienttest reproduces the failures every external service has, for the fake servers
// of plugin tests: docs/plugins.md asks each plugin to show what it does when its service limits
// the rate, is down, does not answer, or answers something that is not the JSON it expects.
package apiclienttest

import (
	"net/http"

	"gamevault/internal/domain/provider"
)

// Failure is one way a service fails to answer a request.
type Failure struct {
	// Name says what the service does, for a test's GIVEN ("HTTP 429").
	Name string

	// Serve answers a request the failing way.
	Serve http.HandlerFunc

	// Want is the provider sentinel the plugin's error must match: provider.ErrRateLimited or
	// provider.ErrUnavailable for a passing trouble, nil for a malformed answer, which only has to
	// fail (without being taken for a refused session).
	Want error

	// SignInPage reports that the answer is a web page with HTTP 200: a site that keeps its session
	// in cookies serves its sign-in page that way, so a source of such a site may read it as a
	// refused session.
	SignInPage bool
}

// Failures returns the failures every plugin is tested against: a rate limit, an outage with a web
// page, no answer at all, a web page with HTTP 200 (a bot check, a maintenance notice) and a
// truncated JSON answer.
func Failures() []Failure {
	return []Failure{
		{
			Name: "HTTP 429",
			Serve: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
			},
			Want: provider.ErrRateLimited,
		},
		{
			Name: "HTTP 503 with a web page",
			Serve: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("<html><body>Service Unavailable</body></html>"))
			},
			Want: provider.ErrUnavailable,
		},
		{
			Name:  "no answer (the connection is closed)",
			Serve: CloseConnection,
			Want:  provider.ErrUnavailable,
		},
		{
			Name: "a web page with HTTP 200",
			Serve: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Just a moment...</body></html>"))
			},
			Want:       provider.ErrUnavailable,
			SignInPage: true,
		},
		{
			Name: "truncated JSON",
			Serve: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[{"id":`))
			},
		},
	}
}

// CloseConnection closes the connection without answering, as a service that crashed or a network
// that dropped the request does.
func CloseConnection(w http.ResponseWriter, _ *http.Request) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close() // nothing to report: closing is the point
	}
}

// FailOn returns a handler that answers the requests whose path is path with fail, and every other
// request with next: the rest of a fake service keeps working (sign-in, tokens) while one endpoint
// fails.
func FailOn(next http.Handler, path string, fail http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			fail(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}
