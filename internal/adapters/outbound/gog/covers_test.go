package gog

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

func TestCovers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/games/42" {
			http.NotFound(w, r)
			return
		}

		fmt.Fprint(w, `{"_links":{"boxArtImage":{"href":"https://images.gog-statics.com/box.jpg"},
		  "backgroundImage":{"href":"https://images.gog-statics.com/bg.jpg"}},"_embedded":{"product":{"title":"Gwent"}}}`)
	}))
	defer srv.Close()

	c := NewCovers()
	c.API.BaseURL = srv.URL

	q := media.CoverQuery{
		Title: "Gwent",
		Links: game.Links{"gog": "42"},
	}
	if !c.Applies(q) || c.Applies(media.CoverQuery{
		Title: "x",
		Links: game.Links{game.LinkSteam: "1"},
	}) {
		t.Fatal("applies only to games imported from GOG")
	}

	got, err := c.Covers(context.Background(), q, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].URL != "https://images.gog-statics.com/box.jpg" || got[0].Title != "Gwent" {
		t.Fatalf("candidates: %+v", got)
	}
}

func TestCovers_Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the GOG API knows the probe product", func(t *testing.T) {
		var path string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path

			fmt.Fprint(w, `{"_links":{"boxArtImage":{"href":"https://images.gog-statics.com/box.jpg"}},"_embedded":{"product":{"title":"The Witcher"}}}`)
		}))
		defer srv.Close()

		c := NewCovers()
		c.API.BaseURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it works after one product lookup", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "/v2/games/"+testProductID, path)
			})
		})
	})

	t.Run("GIVEN the GOG API answers 404 for the probe product", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		c := NewCovers()
		c.API.BaseURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it still works", func(t *testing.T) {
				require.NoError(t, err)
			})
		})
	})

	t.Run("GIVEN the GOG API rate-limits requests", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		c := NewCovers()
		c.API.BaseURL = srv.URL

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
		Title: "Gwent",
		Links: game.Links{"gog": "42"},
	}

	for status, want := range map[int]error{
		http.StatusTooManyRequests: provider.ErrRateLimited,
		http.StatusBadGateway:      provider.ErrUnavailable,
		0:                          provider.ErrUnavailable, // no answer at all
	} {
		t.Run(fmt.Sprintf("GIVEN the GOG API answers HTTP %d", status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			c := NewCovers()
			c.API.BaseURL = srv.URL

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
