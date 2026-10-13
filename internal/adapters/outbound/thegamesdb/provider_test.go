package thegamesdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/provider"
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

	if len(got) != 2 || got[0].URL != "https://cdn.test/large/boxart/front/1-1.jpg" || got[0].Label != "Halo 3 · Microsoft Xbox 360 · 2007" {
		t.Fatalf("exact title match must come first with its front art, and no other game: %+v", got)
	}

	if got[1].Title != "Halo 3 (Platinum Hits)" {
		t.Fatalf("other releases of the same game follow: %+v", got)
	}

	// The cover picker browses: other games the search found follow, for the user to choose.
	browse := q
	browse.Browse = true

	got, err = p.Covers(context.Background(), browse, schema.Settings{settingAPIKey: "good"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 3 || got[0].Title != "Halo 3" || got[1].Title != "Halo 3 (Platinum Hits)" || got[2].Title != "Halo 3: ODST" {
		t.Fatalf("the picker lists other games after the game's own releases: %+v", got)
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

// testProvider points the provider at the fake server, without the production throttle's gap.
func testProvider(srv *httptest.Server) *Provider {
	p := New()
	p.API.BaseURL, p.API.HTTP = srv.URL, srv.Client()
	p.throttle = nil

	return p
}

func TestCovers_editionSystem(t *testing.T) {
	srv, calls := fakeServer(t)
	p := testProvider(srv)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the digital PS3 edition of a game linked to Steam", func(t *testing.T) {
		q := media.CoverQuery{
			Title:     "Halo 3",
			System:    "PS3",
			Platforms: []string{"PlayStation Store"},
			Links:     game.Links{game.LinkSteam: "1"},
		}

		t.Run("THEN it keeps its quota on the first pass and helps on the fallback pass", func(t *testing.T) {
			assert.False(t, p.Applies(q))
			q.Fallback = true
			assert.True(t, p.Applies(q))
		})

		t.Run("WHEN its covers are searched THEN the search is filtered to the edition's system", func(t *testing.T) {
			_, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "good"})
			require.NoError(t, err)
			assert.Contains(t, (*calls)[len(*calls)-1], "filter%5Bplatform%5D=12")
		})
	})
}

func TestProvider_transportErrorsHideTheKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN TheGamesDB closes the connection without answering", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
		}))
		t.Cleanup(srv.Close)

		p := testProvider(srv)
		s := schema.Settings{settingAPIKey: "s3cr3t-key"}

		t.Run("WHEN the key is tested and covers are searched", func(t *testing.T) {
			testErr := p.Test(ctx, s)
			_, coversErr := p.Covers(ctx, media.CoverQuery{
				Title:             "Halo 3",
				PhysicalPlatforms: []string{"Xbox 360"},
			}, s)

			t.Run("THEN no error carries the key, which TheGamesDB takes in the query", func(t *testing.T) {
				require.Error(t, testErr)
				require.Error(t, coversErr)
				assert.NotContains(t, testErr.Error(), "s3cr3t-key")
				assert.NotContains(t, coversErr.Error(), "s3cr3t-key")
			})
		})
	})
}

func TestProvider_providerLevelErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	q := media.CoverQuery{
		Title:             "Halo 3",
		PhysicalPlatforms: []string{"Xbox 360"},
	}

	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"a rejected key with TheGamesDB's message", http.StatusForbidden, `{"code":403,"status":"Invalid API key was provided."}`, provider.ErrBadSettings},
		{"a rejected key without a message", http.StatusUnauthorized, ``, provider.ErrBadSettings},
		{"a server error", http.StatusBadGateway, `<html>502 Bad Gateway</html>`, provider.ErrUnavailable},
	} {
		t.Run("GIVEN TheGamesDB answering "+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			t.Run("WHEN covers are searched", func(t *testing.T) {
				_, err := testProvider(srv).Covers(ctx, q, schema.Settings{settingAPIKey: "k"})

				t.Run("THEN it is a problem of the provider, not of the game", func(t *testing.T) {
					require.ErrorIs(t, err, tc.want)
				})
			})
		})
	}

	t.Run("GIVEN TheGamesDB closing the connection", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
		}))
		t.Cleanup(srv.Close)

		t.Run("WHEN covers are searched", func(t *testing.T) {
			_, err := testProvider(srv).Covers(ctx, q, schema.Settings{settingAPIKey: "k"})

			t.Run("THEN it is an outage", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrUnavailable)
			})
		})
	})
}

// TestCovers_editionTitles checks that an edition's title ("… GOTY") finds the game, whose name
// TheGamesDB lists without the tag, as IGDB, Wikidata, Steam and SteamGridDB do.
func TestCovers_editionTitles(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN TheGamesDB, which knows the game by its name only", func(t *testing.T) {
		var names []string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1.1/Games/ByGameName" {
				w.Write([]byte(`{"data":{}}`))
				return
			}

			names = append(names, r.URL.Query().Get("name"))
			if r.URL.Query().Get("name") != "Example West" {
				w.Write([]byte(`{"data":{"count":0,"games":[]},"include":{"boxart":{"base_url":{},"data":{}}}}`))
				return
			}

			w.Write([]byte(`{"data":{"count":1,"games":[{"id":7,"game_title":"Example West","platform":1,"overview":"A western."}]},
			 "include":{"boxart":{"base_url":{"large":"https://cdn.test/large/"},
				"data":{"7":[{"side":"front","filename":"boxart/front/7-1.jpg"}]}}}}`))
		}))
		defer srv.Close()

		p := testProvider(srv)
		q := media.CoverQuery{Title: "Example West GOTY"}

		t.Run("WHEN the covers of its GOTY edition are searched", func(t *testing.T) {
			names = nil
			got, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "k"})
			require.NoError(t, err)

			t.Run("THEN the game's name is searched first and its box found", func(t *testing.T) {
				assert.Equal(t, []string{"Example West"}, names)
				require.Len(t, got, 1)
				assert.Equal(t, "https://cdn.test/large/boxart/front/7-1.jpg", got[0].URL)
			})
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			names = nil
			got, err := NewDetails(p).Details(ctx, q, "en", schema.Settings{settingAPIKey: "k"})
			require.NoError(t, err)

			t.Run("THEN the game's name finds it too", func(t *testing.T) {
				assert.Equal(t, []string{"Example West"}, names)
				require.NotNil(t, got)
				assert.Equal(t, "A western.", got.Summary)
			})
		})

		t.Run("WHEN a title only TheGamesDB's raw search could find is searched", func(t *testing.T) {
			names = nil
			_, err := p.Covers(ctx, media.CoverQuery{Title: "Unknown Quest Deluxe"}, schema.Settings{settingAPIKey: "k"})
			require.NoError(t, err)

			t.Run("THEN the raw title gets a second search", func(t *testing.T) {
				assert.Equal(t, []string{"Unknown Quest", "Unknown Quest Deluxe"}, names)
			})
		})
	})
}
