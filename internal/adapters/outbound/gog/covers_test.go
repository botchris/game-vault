package gog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gamevault/internal/application/media"
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
	c.APIURL = srv.URL

	q := media.CoverQuery{Title: "Gwent", ExternalIDs: []string{"gog:42", "gog:7"}}
	if !c.Applies(q) || c.Applies(media.CoverQuery{Title: "x", SteamAppID: 1}) {
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
