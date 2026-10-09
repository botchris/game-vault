package steam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
)

func TestCoversResolveHashedAssetPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("input_json"), `"appid":3598130`) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		// Real answer shape for Big Rigs: Over the Road Racing (a recent app with hashed paths).
		w.Write([]byte(`{"response":{"store_items":[{"appid":3598130,"assets":{
			"asset_url_format":"steam/apps/3598130/${FILENAME}?t=1782888957",
			"library_capsule":"2f9218e19c6da2a053d03217a68c40809c6e6bf1/library_600x900.jpg",
			"header":"9246a1154a6af6af07a3eac9dd6e7dbf07504b0d/header.jpg"}}]}}`))
	}))
	defer srv.Close()

	s := testStore(srv)
	s.CDNURL, s.AssetsURL = "https://legacy.test", "https://assets.test/store_item_assets/"

	got, err := s.Covers(context.Background(), media.CoverQuery{
		Title: "Big Rigs",
		Links: game.Links{game.LinkSteam: "3598130"},
	}, schema.Settings{})
	if err != nil || len(got) != 2 {
		t.Fatalf("covers: %+v %v", got, err)
	}

	want := "https://assets.test/store_item_assets/steam/apps/3598130/2f9218e19c6da2a053d03217a68c40809c6e6bf1/library_600x900.jpg?t=1782888957"
	if got[0].URL != want || !strings.HasSuffix(strings.Split(got[1].URL, "?")[0], "/9246a1154a6af6af07a3eac9dd6e7dbf07504b0d/header.jpg") {
		t.Fatalf("hashed paths not used: %+v", got)
	}

	// If the API fails, the legacy predictable URLs are still offered.
	got, _ = s.Covers(context.Background(), media.CoverQuery{
		Title: "Portal 2",
		Links: game.Links{game.LinkSteam: "620"},
	}, schema.Settings{})
	if len(got) != 2 || got[0].URL != "https://legacy.test/steam/apps/620/library_600x900.jpg" {
		t.Fatalf("legacy fallback: %+v", got)
	}
}

// testStore points both of the store's clients at the fake server.
func testStore(srv *httptest.Server) *Store {
	s := NewStore()
	s.Site.BaseURL, s.Site.HTTP = srv.URL, srv.Client()
	s.API.BaseURL, s.API.HTTP = srv.URL, srv.Client()

	return s
}
