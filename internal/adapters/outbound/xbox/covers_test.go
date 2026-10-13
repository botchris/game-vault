package xbox

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
)

func TestCovers_Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the Store catalog answers with a product", func(t *testing.T) {
		var ids string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ids = r.URL.Query().Get("bigIds")

			fmt.Fprint(w, `{"Products":[{"ProductId":"`+testProductID+`","LocalizedProperties":[{"ProductTitle":"Minecraft",
			  "Images":[{"ImagePurpose":"Poster","Uri":"//store-images.s-microsoft.com/p.jpg"}]}]}]}`)
		}))
		defer srv.Close()

		c := NewCovers()
		c.CatalogURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it works after asking for one product", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, testProductID, ids)
			})
		})
	})

	t.Run("GIVEN the Store catalog does not know the product", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"Products":[]}`)
		}))
		defer srv.Close()

		c := NewCovers()
		c.CatalogURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it still works", func(t *testing.T) {
				require.NoError(t, err)
			})
		})
	})

	t.Run("GIVEN the Store catalog rate-limits requests", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		c := NewCovers()
		c.CatalogURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it reports a rate limit", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Contains(t, err.Error(), "HTTP 429")
			})
		})
	})
}

// TestCovers_passingTroubles checks that a rate limit and an outage are told apart from "no cover":
// the chain asks again later instead of marking the edition's cover missing.
func TestCovers_passingTroubles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.CoverQuery{
		Title: "Minecraft",
		Links: game.Links{LinkedStore.Key: testProductID},
	}

	for status, want := range map[int]error{
		http.StatusTooManyRequests: provider.ErrRateLimited,
		http.StatusBadGateway:      provider.ErrUnavailable,
		0:                          provider.ErrUnavailable, // no answer at all
	} {
		t.Run(fmt.Sprintf("GIVEN the Store catalog answers HTTP %d", status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			c := NewCovers()
			c.CatalogURL = srv.URL

			if status == 0 {
				srv.Close()
			}

			t.Run("WHEN covers are asked", func(t *testing.T) {
				_, err := c.Covers(ctx, q, nil)

				t.Run("THEN the error says it is a passing trouble", func(t *testing.T) {
					require.ErrorIs(t, err, want)
				})
			})
		})
	}
}
