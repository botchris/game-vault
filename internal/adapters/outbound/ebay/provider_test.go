package ebay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gamevault/internal/domain/schema"
)

func TestLookupConsensusAndMarketplaceFallback(t *testing.T) {
	tokens := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/identity/v1/oauth2/token":
			if u, p, ok := r.BasicAuth(); !ok || u != "id" || p != "secret" {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"invalid_client","error_description":"client authentication failed"}`))

				return
			}

			tokens++

			w.Write([]byte(`{"access_token":"tok","expires_in":7200}`))
		case "/buy/browse/v1/item_summary/search":
			if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Query().Get("gtin") != "5030934110075" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			if r.Header.Get("X-EBAY-C-MARKETPLACE-ID") == "EBAY_ES" {
				w.Write([]byte(`{"total":0}`)) // nothing on eBay Spain: falls back to the UK
				return
			}

			w.Write([]byte(`{"total":4,"itemSummaries":[
				{"title":"Dead Space 3 Xbox 360 PAL Complete with Manual","image":{"imageUrl":"https://i.ebayimg.test/1.jpg"}},
				{"title":"Dead Space 3 (Xbox 360) - Very Good Condition"},
				{"title":"DEAD SPACE 3 XBOX 360 GAME PAL"},
				{"title":"Dead Space 3 Limited Edition Xbox 360 sealed"}]}`))
		}
	}))
	defer srv.Close()

	p := &Provider{BaseURL: srv.URL, Client: srv.Client(), tokens: map[string]token{}}
	s := schema.Settings{settingClientID: "id", settingClientSecret: "secret", settingMarketplaces: "EBAY_ES, EBAY_GB"}

	m, err := p.Lookup(context.Background(), "5030934110075", s)
	if err != nil || len(m) != 1 {
		t.Fatalf("lookup: %+v %v", m, err)
	}

	if m[0].Title != "Dead Space 3" || m[0].Platform != "Xbox 360" {
		t.Fatalf("consensus title/platform: %+v", m[0])
	}

	p.Lookup(context.Background(), "5030934110075", s)

	if tokens != 1 {
		t.Fatalf("token must be cached, fetched %d times", tokens)
	}

	if _, err := p.Test(context.Background(), schema.Settings{settingClientID: "id", settingClientSecret: "wrong"}); err == nil {
		t.Fatal("bad keys must fail the test")
	}
}
