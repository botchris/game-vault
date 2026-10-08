package epic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gamevault/internal/application/media"
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
	c.p.OAuthURL, c.p.CatalogURL = srv.URL, srv.URL

	q := media.CoverQuery{Title: "Control", ExternalIDs: []string{"steam:1", "epic:abc"}}
	if !c.Applies(q) || c.Applies(media.CoverQuery{Title: "x", ExternalIDs: []string{"gog:1"}}) {
		t.Fatal("applies only to games imported from Epic")
	}

	for range 2 {
		got, err := c.Covers(context.Background(), q, nil)
		if err != nil {
			t.Fatal(err)
		}

		if len(got) != 2 || got[0].URL != "https://cdn1.epicgames.com/tall.jpg" || got[1].URL != "https://cdn1.epicgames.com/wide.jpg" {
			t.Fatalf("candidates: %+v", got)
		}
	}

	if tokens != 1 {
		t.Fatalf("the app token should be reused, got %d", tokens)
	}
}
