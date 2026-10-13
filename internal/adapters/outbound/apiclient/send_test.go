package apiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
)

// fakeSignIn mimics the endpoints a store's sign-in uses: an OAuth token endpoint that takes a form
// and the client's credentials in basic auth, an authorize page that redirects to the app with a
// code, and an account page that sends a signed-out visitor to its login page.
func fakeSignIn(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		_ = r.ParseForm()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"type":   r.Header.Get("Content-Type"),
			"grant":  r.PostForm.Get("grant_type"),
			"code":   r.PostForm.Get("code"),
			"client": user + ":" + pass,
		})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "com.example.app://redirect?code=one-time", http.StatusFound)
	})
	mux.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login?next=%2Faccount", http.StatusFound)
	})
	mux.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>Sign in</html>"))
	})
	mux.HandleFunc("GET /huge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"games":["` + strings.Repeat("x", 2048) + `"]}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestClient_formsAndRedirects(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv := fakeSignIn(t)

	t.Run("GIVEN an OAuth token endpoint", func(t *testing.T) {
		c := New(srv.URL)
		c.HTTP = srv.Client()

		t.Run("WHEN a form is posted with the client's credentials", func(t *testing.T) {
			var out map[string]string

			err := c.Do(ctx, Request{
				Method: http.MethodPost,
				Path:   "/oauth/token",
				Form: url.Values{
					"grant_type": {"authorization_code"},
					"code":       {"abc"},
				},
				Header: http.Header{"Authorization": {BasicAuth("app", "secret")}},
			}, &out)
			require.NoError(t, err)

			t.Run("THEN the form arrives encoded, with the credentials", func(t *testing.T) {
				assert.Equal(t, "application/x-www-form-urlencoded", out["type"])
				assert.Equal(t, "authorization_code", out["grant"])
				assert.Equal(t, "abc", out["code"])
				assert.Equal(t, "app:secret", out["client"])
			})
		})
	})

	t.Run("GIVEN a client that does not follow redirects", func(t *testing.T) {
		c := New(srv.URL)
		c.HTTP = srv.Client() // replacing the HTTP client keeps the redirect rule
		c.NoRedirects = true

		t.Run("WHEN an authorize page redirects to the app", func(t *testing.T) {
			res, err := c.Send(ctx, Request{Path: "/authorize"})
			require.NoError(t, err)

			t.Run("THEN the redirect itself is the answer, with its Location", func(t *testing.T) {
				assert.Equal(t, http.StatusFound, res.Status)
				assert.Equal(t, "com.example.app://redirect?code=one-time", res.Header.Get("Location"))

				t.Run("AND it is not a success", func(t *testing.T) {
					assert.True(t, IsStatus(res.Err(), http.StatusFound))
				})
			})
		})
	})

	t.Run("GIVEN a client that follows redirects", func(t *testing.T) {
		c := New(srv.URL)
		c.HTTP = srv.Client()

		t.Run("WHEN a signed-out visitor opens an account page", func(t *testing.T) {
			res, err := c.Send(ctx, Request{Path: "/account"})
			require.NoError(t, err)

			t.Run("THEN the answer says where the redirects ended", func(t *testing.T) {
				assert.Equal(t, http.StatusOK, res.Status)
				assert.Equal(t, "/login", res.URL.Path)
				require.NoError(t, res.Err())

				t.Run("AND its web page is not decoded as JSON", func(t *testing.T) {
					assert.ErrorIs(t, res.Decode(&struct{}{}), ErrNotJSON)
				})
			})
		})
	})
}

func TestClient_tooLarge(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv := fakeSignIn(t)

	t.Run("GIVEN a client that reads at most 1 KiB", func(t *testing.T) {
		c := New(srv.URL)
		c.HTTP = srv.Client()
		c.MaxBody = 1 << 10

		t.Run("WHEN the answer is larger", func(t *testing.T) {
			err := c.Get(ctx, "/huge", nil, &struct{}{})

			t.Run("THEN the error says it is too large, not that the service changed", func(t *testing.T) {
				require.ErrorIs(t, err, ErrTooLarge)
				assert.NotContains(t, err.Error(), "may have changed")
				assert.Contains(t, err.Error(), "/huge")

				t.Run("AND it is not a passing trouble", func(t *testing.T) {
					assert.NotErrorIs(t, ClassifyError(ctx, err), provider.ErrRateLimited)
				})
			})
		})

		t.Run("WHEN the answer is exactly the limit", func(t *testing.T) {
			c.MaxBody = int64(len(`{"games":["`) + 2048 + len(`"]}`))

			var out struct {
				Games []string `json:"games"`
			}

			t.Run("THEN it is read whole", func(t *testing.T) {
				require.NoError(t, c.Get(ctx, "/huge", nil, &out))
				assert.Len(t, out.Games, 1)
			})
		})
	})
}

func TestTransient(t *testing.T) {
	for _, s := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
		assert.True(t, Transient(s), s)
	}

	for _, s := range []int{http.StatusOK, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		assert.False(t, Transient(s), s)
	}
}
