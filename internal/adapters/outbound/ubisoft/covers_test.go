package ubisoft

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
)

func TestCovers(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/space-with-art/spaceCardAsset/boxArt_mobile.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
	})
	mux.HandleFunc("POST /search", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Algolia-API-Key") != storeSearchKey {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		fmt.Fprint(w, `{"hits":[
		  {"title":"Assassin's Creed Origins - Gold Edition","short_title":"Assassin's Creed Origins","Edition":"Gold Edition","product_type":"Games","dlcType":null,
		   "image_groups":[{"images":[{"dis_base_link":"https://store.ubisoft.com/gold.jpg"}]}]},
		  {"title":"Assassin's Creed Origins","short_title":"Assassin's Creed Origins","Edition":"Standard Edition","product_type":"Games","dlcType":null,
		   "image_groups":[{"images":[{"dis_base_link":"https://store.ubisoft.com/standard.jpg"}]}]},
		  {"title":"Assassin's Creed Origins - Helix Credits","short_title":"Assassin's Creed Origins","Edition":"7400 Helix","product_type":"DLCs","dlcType":"currency",
		   "image_groups":[{"images":[{"dis_base_link":"https://store.ubisoft.com/helix.jpg"}]}]},
		  {"title":"Assassin's Creed Odyssey","short_title":"Assassin's Creed Odyssey","Edition":"Standard Edition","product_type":"Games","dlcType":null,
		   "image_groups":[{"images":[{"dis_base_link":"https://store.ubisoft.com/odyssey.jpg"}]}]}]}`)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewCovers()
	c.CDNURL, c.Search.BaseURL = srv.URL, srv.URL+"/search"

	if !c.Applies(media.CoverQuery{
		Title:     "x",
		Platforms: []string{"Ubisoft Connect"},
	}) || c.Applies(media.CoverQuery{
		Title: "x",
		Links: game.Links{game.LinkSteam: "1"},
	}) {
		t.Fatal("applies to games imported from Ubisoft or with a Ubisoft Connect copy")
	}

	got, err := c.Covers(context.Background(), media.CoverQuery{
		Title: "Assassin's Creed® Origins",
		Links: game.Links{"ubisoft": "space-with-art"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	urls := make([]string, 0, len(got))
	for _, g := range got {
		urls = append(urls, g.URL)
	}

	want := fmt.Sprint([]string{srv.URL + "/space-with-art/spaceCardAsset/boxArt_mobile.jpg", "https://store.ubisoft.com/standard.jpg", "https://store.ubisoft.com/gold.jpg"})
	if fmt.Sprint(urls) != want {
		t.Fatalf("candidates:\n got %v\nwant %s", urls, want)
	}
}

func TestCovers_Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a store search that answers with hits", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"hits":[{"title":"Assassin's Creed Origins","short_title":"Assassin's Creed Origins","Edition":"Standard Edition","product_type":"Games","dlcType":null,"image_groups":[]}]}`)
		}))
		defer srv.Close()

		c := NewCovers()
		c.Search.BaseURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			t.Run("THEN it succeeds", func(t *testing.T) {
				require.NoError(t, c.Test(ctx, nil))
			})
		})
	})

	t.Run("GIVEN a store search that is rate limited", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer srv.Close()

		c := NewCovers()
		c.Search.BaseURL = srv.URL

		t.Run("WHEN the provider is tested", func(t *testing.T) {
			err := c.Test(ctx, nil)

			t.Run("THEN it fails and says what to do", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "HTTP 429")
				assert.Contains(t, err.Error(), "try again later")
			})
		})
	})
}
