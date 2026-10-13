package epic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/source"
)

func TestCovers(t *testing.T) {
	var tokens int

	mux := http.NewServeMux()
	mux.HandleFunc("POST /account/api/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()

		if r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "want client credentials", http.StatusBadRequest)
			return
		}

		tokens++

		fmt.Fprint(w, `{"access_token":"app","expires_at":"2099-01-01T00:00:00.000Z"}`)
	})
	mux.HandleFunc("GET /catalog/api/shared/bulk/items", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "bearer app" || r.URL.Query().Get("id") != "abc" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		fmt.Fprint(w, `{"abc":{"title":"Control","keyImages":[
		  {"type":"DieselGameBox","url":"https://cdn1.epicgames.com/wide.jpg"},
		  {"type":"DieselGameBoxTall","url":"https://cdn1.epicgames.com/tall.jpg"},
		  {"type":"DieselGameBoxLogo","url":"https://cdn1.epicgames.com/logo.png"}]}}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewCovers()
	c.OAuth.BaseURL, c.Catalog.BaseURL = srv.URL, srv.URL

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.GameQuery{
		Title: "Control",
		Links: game.Links{"steam": "1", "epic": "abc"},
	}

	t.Run("GIVEN Epic's catalog with a tall, a wide and a logo image of a game", func(t *testing.T) {
		t.Run("THEN it applies only to games imported from Epic", func(t *testing.T) {
			assert.True(t, c.Applies(q))
			assert.False(t, c.Applies(media.GameQuery{
				Title: "x",
				Links: game.Links{"gog": "1"},
			}))
		})

		t.Run("WHEN covers are asked twice", func(t *testing.T) {
			for range 2 {
				got, err := c.Covers(ctx, q, nil)
				require.NoError(t, err)

				t.Run("THEN the box art comes first, then the wide image, and no logo", func(t *testing.T) {
					require.Len(t, got, 2)
					assert.Equal(t, "https://cdn1.epicgames.com/tall.jpg", got[0].URL)
					assert.Equal(t, "https://cdn1.epicgames.com/wide.jpg", got[1].URL)
				})
			}

			t.Run("AND the application token is reused", func(t *testing.T) {
				assert.Equal(t, 1, tokens)
			})
		})
	})
}

func TestCovers_Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN Epic's sign-in endpoint grants application tokens", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/account/api/oauth/token" || r.FormValue("grant_type") != "client_credentials" {
				http.Error(w, "unexpected request", http.StatusBadRequest)
				return
			}

			fmt.Fprint(w, `{"access_token":"app","expires_at":"2099-01-01T00:00:00.000Z"}`)
		}))
		defer srv.Close()

		c := NewCovers()
		c.OAuth.BaseURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it works", func(t *testing.T) {
				require.NoError(t, err)
			})
		})
	})

	t.Run("GIVEN Epic's sign-in endpoint fails with HTTP 503", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		c := NewCovers()
		c.OAuth.BaseURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it reports what happened", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "HTTP 503")
			})
		})
	})
}

// TestCovers_passingTroubles checks that a rate limit and an outage, of the token endpoint or of
// the catalog, are told apart from "no cover": the chain asks again later instead of marking the
// edition's cover missing.
func TestCovers_passingTroubles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.GameQuery{
		Title: "Control",
		Links: game.Links{"epic": "abc"},
	}

	for _, endpoint := range []string{"token", "catalog"} {
		for status, want := range map[int]error{
			http.StatusTooManyRequests: provider.ErrRateLimited,
			http.StatusBadGateway:      provider.ErrUnavailable,
		} {
			t.Run(fmt.Sprintf("GIVEN Epic's %s endpoint answers HTTP %d", endpoint, status), func(t *testing.T) {
				mux := http.NewServeMux()
				mux.HandleFunc("POST /account/api/oauth/token", func(w http.ResponseWriter, _ *http.Request) {
					if endpoint == "token" {
						w.WriteHeader(status)
						return
					}

					fmt.Fprint(w, `{"access_token":"app","expires_at":"2099-01-01T00:00:00.000Z"}`)
				})
				mux.HandleFunc("GET /catalog/api/shared/bulk/items", func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(status)
				})

				srv := httptest.NewServer(mux)
				defer srv.Close()

				c := NewCovers()
				c.OAuth.BaseURL, c.Catalog.BaseURL = srv.URL, srv.URL

				t.Run("WHEN covers are asked", func(t *testing.T) {
					_, err := c.Covers(ctx, q, nil)

					t.Run("THEN the error says it is a passing trouble, not a signed-out session", func(t *testing.T) {
						require.ErrorIs(t, err, want)
						assert.NotErrorIs(t, err, source.ErrSignedOut)
					})
				})
			})
		}
	}
}

// TestCovers_refusedToken checks that the catalog refusing the application token renews it once
// and then reports a plain error: the covers have no user session, so it is never the source's
// "sign in again".
func TestCovers_refusedToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN Epic's catalog refuses every application token with HTTP 401", func(t *testing.T) {
		var tokens int

		mux := http.NewServeMux()
		mux.HandleFunc("POST /account/api/oauth/token", func(w http.ResponseWriter, _ *http.Request) {
			tokens++

			fmt.Fprint(w, `{"access_token":"app","expires_at":"2099-01-01T00:00:00.000Z"}`)
		})
		mux.HandleFunc("GET /catalog/api/shared/bulk/items", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})

		srv := httptest.NewServer(mux)
		defer srv.Close()

		c := NewCovers()
		c.OAuth.BaseURL, c.Catalog.BaseURL = srv.URL, srv.URL

		t.Run("WHEN covers are asked", func(t *testing.T) {
			_, err := c.Covers(ctx, media.GameQuery{
				Title: "Control",
				Links: game.Links{"epic": "abc"},
			}, nil)

			t.Run("THEN it fails without signing a source out", func(t *testing.T) {
				require.Error(t, err)
				assert.NotErrorIs(t, err, source.ErrSignedOut)

				t.Run("AND the token was renewed once", func(t *testing.T) {
					assert.Equal(t, 2, tokens)
				})
			})
		})
	})
}
