package apiclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeAPI(t *testing.T) *Client {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/echo":
			var body map[string]any

			_ = json.NewDecoder(r.Body).Decode(&body)

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"method": r.Method, "page": r.URL.Query().Get("page"), "agent": r.UserAgent(),
				"key": r.Header.Get("X-Key"), "extra": r.Header.Get("X-Extra"), "body": body,
			})
		case "/private":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Invalid authorization header\n"))
		case "/maintenance":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>Back soon</html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL + "/")
	c.HTTP = srv.Client()
	c.Header.Set("X-Key", "k")

	return c
}

func TestClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := fakeAPI(t)

	t.Run("GIVEN a JSON API", func(t *testing.T) {
		t.Run("WHEN a page is read", func(t *testing.T) {
			var out map[string]any

			err := c.Get(ctx, "/echo", url.Values{"page": {"2"}}, &out)
			require.NoError(t, err)

			t.Run("THEN the query, the User-Agent and the client's headers are sent", func(t *testing.T) {
				assert.Equal(t, "GET", out["method"])
				assert.Equal(t, "2", out["page"])
				assert.Equal(t, UserAgent, out["agent"])
				assert.Equal(t, "k", out["key"])
			})
		})

		t.Run("WHEN a body is posted with a header of its own", func(t *testing.T) {
			var out map[string]any

			err := c.Do(ctx, Request{Method: http.MethodPost, Path: "/echo", Body: map[string]int{"n": 1},
				Header: http.Header{"X-Extra": {"e"}}}, &out)
			require.NoError(t, err)

			t.Run("THEN it arrives as JSON with both headers", func(t *testing.T) {
				assert.Equal(t, "POST", out["method"])
				assert.Equal(t, map[string]any{"n": float64(1)}, out["body"])
				assert.Equal(t, "e", out["extra"])
				assert.Equal(t, "k", out["key"])
			})
		})

		t.Run("WHEN it refuses the credentials", func(t *testing.T) {
			err := c.Get(ctx, "/private", nil, &struct{}{})

			t.Run("THEN the status can be matched and the answer explains why", func(t *testing.T) {
				assert.True(t, IsStatus(err, http.StatusUnauthorized, http.StatusForbidden))
				assert.False(t, IsStatus(err, http.StatusNotFound))

				var se *StatusError
				require.True(t, errors.As(err, &se))
				assert.Equal(t, "Invalid authorization header", se.Body)
			})
		})

		t.Run("WHEN it answers with an HTML page", func(t *testing.T) {
			err := c.Get(ctx, "/maintenance", nil, &struct{}{})

			t.Run("THEN it is not decoded as JSON", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrNotJSON)
			})
		})
	})
}
