package eaapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

func TestSlugCandidates(t *testing.T) {
	got := slugCandidates("Mass Effect™ 2 (2010)", []string{"en-us_mass-effect-2-base-game-bundle_VideoGameProduct_es_pc"})
	for _, want := range []string{"mass-effect-2-2010", "mass-effect-2", "mass-effect-2-base-game-bundle", "mass-effect"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q in %v", want, got)
		}
	}

	if s := slugify("Command & Conquer Red Alert™ 2 and Yuri’s Revenge™"); s != "command-and-conquer-red-alert-2-and-yuris-revenge" {
		t.Errorf("slugify: %s", s)
	}
}

func TestCovers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Query string }
		json.NewDecoder(r.Body).Decode(&body)
		// Answer only the aliases whose slug the catalog knows.
		var parts []string

		for i := 0; strings.Contains(body.Query, fmt.Sprintf(" g%d:", i)); i++ {
			alias := fmt.Sprintf("g%d", i)
			switch {
			case strings.Contains(body.Query, alias+`: game(slug: "battlefield-1")`):
				parts = append(parts, `"`+alias+`":{"slug":"battlefield-1","title":"Battlefield 1","packArt":{"aspect9x16Image":{"path":"https://app-images.ea.com/bf1-9x16.jpg"},"aspect5x7Image":null},"keyArt":{"aspect1x1Image":null,"aspect16x9Image":{"path":"https://app-images.ea.com/bf1-16x9.jpg"}}}`)
			case strings.Contains(body.Query, alias+`: game(slug: "battlefield")`):
				parts = append(parts, `"`+alias+`":{"slug":"battlefield","title":"Battlefield franchise","packArt":null,"keyArt":null}`)
			default:
				parts = append(parts, `"`+alias+`":null`)
			}
		}

		fmt.Fprintf(w, `{"data":{%s}}`, strings.Join(parts, ","))
	}))
	defer srv.Close()

	c := NewCovers()
	c.GraphQLURL = srv.URL

	q := media.CoverQuery{
		Title: "Battlefield™ 1",
		Links: game.Links{"ea": "en-us_battlefield-1-standard-edition-pc-row-juno-3pdd_VideoGameProduct_es_pc"},
	}
	if !c.Applies(q) || c.Applies(media.CoverQuery{
		Title: "x",
		Links: game.Links{"gog": "1"},
	}) {
		t.Fatal("applies only to games imported from EA")
	}

	got, err := c.Covers(context.Background(), q, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].URL != "https://app-images.ea.com/bf1-9x16.jpg" || got[1].URL != "https://app-images.ea.com/bf1-16x9.jpg" {
		t.Fatalf("candidates: %+v", got)
	}
}

func TestCovers_Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	run := func(h http.HandlerFunc) error {
		srv := httptest.NewServer(h)
		defer srv.Close()

		c := NewCovers()
		c.GraphQLURL = srv.URL

		return c.Test(ctx, nil)
	}

	t.Run("GIVEN a catalog that knows the game", func(t *testing.T) {
		err := run(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":{"g0":{"slug":"battlefield-1","title":"Battlefield 1"}}}`)
		})

		t.Run("THEN the test passes", func(t *testing.T) { require.NoError(t, err) })
	})

	t.Run("GIVEN a catalog that answers null for the slug", func(t *testing.T) {
		err := run(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"data":{"g0":null}}`)
		})

		t.Run("THEN the test still passes", func(t *testing.T) { require.NoError(t, err) })
	})

	t.Run("GIVEN a catalog that fails with HTTP 503", func(t *testing.T) {
		err := run(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})

		t.Run("THEN the test fails", func(t *testing.T) {
			require.Error(t, err)
			assert.Contains(t, err.Error(), "HTTP 503")
		})
	})

	t.Run("GIVEN a catalog that rejects the query with HTTP 200", func(t *testing.T) {
		err := run(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"errors":[{"message":"PersistedQueryNotFound"}]}`)
		})

		t.Run("THEN the test fails", func(t *testing.T) {
			require.Error(t, err)
			assert.Contains(t, err.Error(), "PersistedQueryNotFound")
		})
	})
}
