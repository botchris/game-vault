package thegamesdb

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

func fakeServer(t *testing.T) (*httptest.Server, *[]string) {
	var calls []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path+"?"+r.URL.RawQuery)

		key := r.URL.Query().Get("apikey")
		switch {
		case r.URL.Path == "/v1/API/Limit" && key == "good":
			w.Write([]byte(`{"remaining_monthly_allowance": 2990, "extra_allowance": 10, "allowance_refresh_timer": 86400}`))
			return
		case r.URL.Path == "/v1/API/Limit": // real behavior: 200 even for unknown keys
			w.Write([]byte(`{"remaining_monthly_allowance":0,"allowance_refresh_timer":0}`))
			return
		case key != "good":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"code":403,"status":"Invalid API key was provided.","remaining_monthly_allowance":0}`))

			return
		}

		switch r.URL.Path {
		case "/v1/Platforms":
			w.Write([]byte(`{"data":{"platforms":{"15":{"id":15,"name":"Microsoft Xbox 360","alias":"microsoft-xbox-360"},"12":{"id":12,"name":"Sony Playstation 3","alias":"sony-playstation-3"}}}}`))
		case "/v1.1/Games/ByGameName":
			w.Write([]byte(`{"data":{"games":[
				{"id":2,"game_title":"Halo 3: ODST","release_date":"2009-09-22","platform":15},
				{"id":3,"game_title":"Halo 3 (Platinum Hits)","release_date":"2008-09-25","platform":15},
				{"id":1,"game_title":"Halo 3","release_date":"2007-09-25","platform":15}]},
			 "include":{"boxart":{"base_url":{"large":"https://cdn.test/large/","thumb":"https://cdn.test/thumb/"},
				"data":{"1":[{"side":"back","filename":"boxart/back/1-1.jpg"},{"side":"front","filename":"boxart/front/1-1.jpg"}],
				        "2":[{"side":"front","filename":"boxart/front/2-1.jpg"}],
				        "3":[{"side":"front","filename":"boxart/front/3-1.jpg"}]}},
			 "platform":{"data":{"15":{"name":"Microsoft Xbox 360"}}}}}`))
		}
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

func TestCoversPlatformFilterAndRanking(t *testing.T) {
	srv, calls := fakeServer(t)
	p := testProvider(srv)

	q := media.CoverQuery{
		Title:             "Halo 3",
		PhysicalPlatforms: []string{"Xbox 360"},
	}
	if p.Applies(media.CoverQuery{
		Title: "Control",
		Links: game.Links{"epic": "abc"},
	}) {
		t.Fatal("games imported from Epic or GOG have store art: keep the quota")
	}

	if !p.Applies(media.CoverQuery{
		Title:    "World of Warcraft",
		Links:    game.Links{"battlenet": "1"},
		Fallback: true,
	}) {
		t.Fatal("on the fallback pass (no store had art) TheGamesDB helps")
	}

	if !p.Applies(q) || p.Applies(media.CoverQuery{
		Title: "Hades",
		Links: game.Links{game.LinkSteam: "1145360"},
	}) {
		t.Fatal("must apply to physical copies and skip Steam-only games")
	}

	got, err := p.Covers(context.Background(), q, schema.Settings{settingAPIKey: "good"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 || got[0].URL != "https://cdn.test/large/boxart/front/1-1.jpg" || got[0].Label != "Halo 3 · Microsoft Xbox 360 · 2007" {
		t.Fatalf("exact title match must come first with its front art: %+v", got)
	}

	if got[1].Title != "Halo 3 (Platinum Hits)" || got[2].Title != "Halo 3: ODST" {
		t.Fatalf("other releases of the same game must come before other games: %+v", got)
	}

	if !strings.Contains((*calls)[len(*calls)-1], "filter%5Bplatform%5D=15") {
		t.Fatalf("search must be filtered to Xbox 360: %v", *calls)
	}

	// The platform list is fetched once per process.
	p.Covers(context.Background(), q, schema.Settings{settingAPIKey: "good"})

	n := 0

	for _, c := range *calls {
		if strings.HasPrefix(c, "/v1/Platforms") {
			n++
		}
	}

	if n != 1 {
		t.Fatalf("platforms fetched %d times", n)
	}
}

func TestTestChecksTheKey(t *testing.T) {
	srv, _ := fakeServer(t)
	p := testProvider(srv)

	if err := p.Test(context.Background(), schema.Settings{settingAPIKey: "good"}); err != nil {
		t.Fatalf("a key with allowance must work: %v", err)
	}

	if err := p.Test(context.Background(), schema.Settings{settingAPIKey: "bad"}); err != ErrUnknownKey {
		t.Fatalf("a bad key must fail with ErrUnknownKey, got %v", err)
	}

	_, err := p.Covers(context.Background(), media.CoverQuery{Title: "Halo 3"}, schema.Settings{settingAPIKey: "bad"})
	if err == nil || !strings.Contains(err.Error(), "Invalid API key was provided.") {
		t.Fatalf("search errors must carry TheGamesDB's message, got %v", err)
	}
}

// testProvider points the provider at the fake server.
func testProvider(srv *httptest.Server) *Provider {
	p := New()
	p.API.BaseURL, p.API.HTTP = srv.URL, srv.Client()

	return p
}
