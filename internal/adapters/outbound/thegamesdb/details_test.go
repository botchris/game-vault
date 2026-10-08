package thegamesdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/schema"
)

func TestPickGamePrefersCompleteEntry(t *testing.T) {
	games := []tgdbGame{
		{ID: 100167, Title: "Red Dead Redemption", Overview: "Short.", Rating: "Not Rated"},
		{ID: 191, Title: "Red Dead Redemption", Overview: "Western epic.", YouTube: "abc", Genres: []int64{1}, Publishers: []int64{2}, Rating: "M - Mature"},
		{ID: 5, Title: "Red Dead Redemption: Undead Nightmare", Overview: "Zombies.", YouTube: "x", Genres: []int64{1}},
	}
	g, ok := pickGame(games, "Red Dead Redemption")
	if !ok || g.ID != 191 {
		t.Fatalf("want the complete exact-title entry 191, got %+v", g)
	}
	if _, ok := pickGame(games, "Halo 3"); ok {
		t.Fatal("other games must not be used")
	}
	if cleanRating("Not Rated") != "" {
		t.Fatal("'Not Rated' is no rating")
	}
}

func TestDetailsResolvesNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/Platforms":
			w.Write([]byte(`{"data":{"count":1,"platforms":{"15":{"id":15,"name":"Microsoft Xbox 360","alias":"microsoft-xbox-360"}}}}`))
		case "/v1.1/Games/ByGameName":
			w.Write([]byte(`{"data":{"count":1,"games":[{"id":191,"game_title":"Red Dead Redemption","release_date":"2010-05-18","platform":15,
				"overview":"Western epic.","players":4,"coop":"Yes","rating":"M - Mature 17+","youtube":"https://youtu.be/-o7rES_3ymA",
				"developers":[7283],"genres":[1,2],"publishers":[17]}]}}`))
		case "/v1/Genres": // real shape: a count next to the map
			w.Write([]byte(`{"data":{"count":2,"genres":{"1":{"id":1,"name":"Action"},"2":{"id":2,"name":"Adventure"}}}}`))
		case "/v1/Developers":
			w.Write([]byte(`{"data":{"count":1,"developers":{"7283":{"id":7283,"name":"Rockstar San Diego"}}}}`))
		case "/v1/Publishers":
			w.Write([]byte(`{"data":{"count":1,"publishers":{"17":{"id":17,"name":"Rockstar Games"}}}}`))
		case "/v1/Games/Images":
			w.Write([]byte(`{"data":{"base_url":{"medium":"https://cdn.test/m/","original":"https://cdn.test/o/"},"images":{"191":[{"filename":"screenshots/191-1.jpg"}]}}}`))
		}
	}))
	defer srv.Close()
	d := NewDetails(&Provider{BaseURL: srv.URL, Client: srv.Client()})
	got, err := d.Details(context.Background(), media.CoverQuery{Title: "Red Dead Redemption", PhysicalPlatforms: []string{"Xbox 360"}}, "es", schema.Settings{settingAPIKey: "k"})
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if len(got.Genres) != 2 || got.Developers[0] != "Rockstar San Diego" || got.Publishers[0] != "Rockstar Games" ||
		got.Players != "4 · co-op" || got.Videos[0].YouTubeID != "-o7rES_3ymA" || len(got.Screenshots) != 1 {
		t.Fatalf("details: %+v", got)
	}
}
