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

func TestCovers_platformFilterAndRanking(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN TheGamesDB with three Halo 3 releases on Xbox 360", func(t *testing.T) {
		srv, calls := fakeServer(t)
		p := testProvider(srv)
		good := schema.Settings{settingAPIKey: "good"}

		q := media.GameQuery{
			Title:             "Halo 3",
			PhysicalPlatforms: []string{"Xbox 360"},
		}

		t.Run("THEN it applies to physical copies and to the fallback pass, keeping the quota otherwise", func(t *testing.T) {
			assert.True(t, p.Applies(q))
			assert.True(t, p.Applies(media.GameQuery{
				Title:    "World of Warcraft",
				Links:    game.Links{"battlenet": "1"},
				Fallback: true,
			}), "on the fallback pass (no store had art) TheGamesDB helps")
			assert.False(t, p.Applies(media.GameQuery{
				Title: "Control",
				Links: game.Links{"epic": "abc"},
			}), "games imported from Epic or GOG have store art")
			assert.False(t, p.Applies(media.GameQuery{
				Title: "Hades",
				Links: game.Links{game.LinkSteam: "1145360"},
			}), "Steam-only games have store art")
		})

		t.Run("WHEN the covers of the Xbox 360 copy are searched", func(t *testing.T) {
			got, err := p.Covers(ctx, q, good)
			require.NoError(t, err)

			t.Run("THEN the exact title comes first with its front art, then the game's other releases", func(t *testing.T) {
				require.Len(t, got, 2)
				assert.Equal(t, "https://cdn.test/large/boxart/front/1-1.jpg", got[0].URL)
				assert.Equal(t, "Halo 3 · Microsoft Xbox 360 · 2007", got[0].Label)
				assert.Equal(t, "Halo 3 (Platinum Hits)", got[1].Title)
			})

			t.Run("AND the search is filtered to Xbox 360", func(t *testing.T) {
				assert.Contains(t, (*calls)[len(*calls)-1], "filter%5Bplatform%5D=15")
			})
		})

		t.Run("WHEN the cover picker browses", func(t *testing.T) {
			browse := q
			browse.Browse = true

			got, err := p.Covers(ctx, browse, good)
			require.NoError(t, err)

			t.Run("THEN other games the search found follow the game's own releases", func(t *testing.T) {
				titles := make([]string, 0, len(got))
				for _, c := range got {
					titles = append(titles, c.Title)
				}

				assert.Equal(t, []string{"Halo 3", "Halo 3 (Platinum Hits)", "Halo 3: ODST"}, titles)
			})
		})

		t.Run("WHEN covers are searched once more", func(t *testing.T) {
			_, err := p.Covers(ctx, q, good)
			require.NoError(t, err)

			t.Run("THEN the platform list was fetched once in all", func(t *testing.T) {
				assert.Equal(t, 1, countCalls(*calls, "/v1/Platforms"))
			})
		})
	})
}

// countCalls counts the calls whose path starts with prefix.
func countCalls(calls []string, prefix string) int {
	n := 0

	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}

	return n
}

func TestProvider_Test(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN TheGamesDB, which answers API/Limit with 200 even for an unknown key", func(t *testing.T) {
		srv, _ := fakeServer(t)
		p := testProvider(srv)

		t.Run("WHEN a key with allowance left is tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingAPIKey: "good"})

			t.Run("THEN it works", func(t *testing.T) {
				require.NoError(t, err)
			})
		})

		t.Run("WHEN an unknown key is tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingAPIKey: "bad"})

			t.Run("THEN it is the provider's settings to fix, as a lookup with that key says", func(t *testing.T) {
				require.ErrorIs(t, err, ErrUnknownKey)
				assert.ErrorIs(t, err, provider.ErrBadSettings)
			})
		})

		t.Run("WHEN covers are searched with the unknown key", func(t *testing.T) {
			_, err := p.Covers(ctx, media.GameQuery{Title: "Halo 3"}, schema.Settings{settingAPIKey: "bad"})

			t.Run("THEN the error carries TheGamesDB's message", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrBadSettings)
				assert.Contains(t, err.Error(), "Invalid API key was provided.")
			})
		})
	})

	t.Run("GIVEN a key that used up this month's allowance, renewed in an hour", func(t *testing.T) {
		srv := newAllowanceServer(t)
		srv.set(true, 0, 3600)

		clock := &fakeClock{now: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
		p := allowanceProvider(srv, clock)

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingAPIKey: "good"})

			t.Run("THEN it is a used-up quota that says when it renews, as lookups get", func(t *testing.T) {
				require.ErrorIs(t, err, ErrNoAllowance)
				assert.ErrorIs(t, err, provider.ErrQuotaExhausted)

				var resumer provider.Resumer
				require.ErrorAs(t, err, &resumer)
				assert.Equal(t, clock.now.Add(time.Hour), resumer.ResumesAt())
			})
		})
	})
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
		q := media.GameQuery{
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
			_, coversErr := p.Covers(ctx, media.GameQuery{
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

	q := media.GameQuery{
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
		q := media.GameQuery{Title: "Example West GOTY"}

		t.Run("WHEN the covers of its GOTY edition are searched", func(t *testing.T) {
			names = nil
			got, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "k"})
			require.NoError(t, err)

			t.Run("THEN the game's name is searched first and its box found", func(t *testing.T) {
				// The raw title is searched too, for an exact title, and finds nothing better.
				assert.Equal(t, []string{"Example West", "Example West GOTY"}, names)
				require.Len(t, got, 1)
				assert.Equal(t, "https://cdn.test/large/boxart/front/7-1.jpg", got[0].URL)
			})
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			names = nil
			got, err := NewDetails(p).Details(ctx, q, "en", schema.Settings{settingAPIKey: "k"})
			require.NoError(t, err)

			t.Run("THEN the game's name finds it too", func(t *testing.T) {
				assert.Equal(t, []string{"Example West", "Example West GOTY"}, names)
				require.NotNil(t, got)
				assert.Equal(t, "A western.", got.Summary)
			})
		})

		t.Run("WHEN a title only TheGamesDB's raw search could find is searched", func(t *testing.T) {
			names = nil
			_, err := p.Covers(ctx, media.GameQuery{Title: "Unknown Quest Deluxe"}, schema.Settings{settingAPIKey: "k"})
			require.NoError(t, err)

			t.Run("THEN the raw title gets a second search", func(t *testing.T) {
				assert.Equal(t, []string{"Unknown Quest", "Unknown Quest Deluxe"}, names)
			})
		})
	})
}

// TestCovers_strippingWentTooFar checks that a title whose tags hide part of the name ("Gold Rush!"
// searched as "Rush!") is not given another game that only shares the MatchKey ("Rush"): the raw
// title is searched too, and the game named exactly like the copy wins.
func TestCovers_strippingWentTooFar(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN TheGamesDB with both Rush and Gold Rush!", func(t *testing.T) {
		var names []string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1.1/Games/ByGameName" {
				w.Write([]byte(`{"data":{}}`))
				return
			}

			name := r.URL.Query().Get("name")
			names = append(names, name)

			switch name {
			case "Rush!":
				w.Write([]byte(`{"data":{"count":1,"games":[{"id":1,"game_title":"Rush","platform":1,"overview":"Another game."}]},
				 "include":{"boxart":{"base_url":{"large":"https://cdn.test/large/"},
					"data":{"1":[{"side":"front","filename":"boxart/front/1-1.jpg"}]}}}}`))
			case "Gold Rush!":
				w.Write([]byte(`{"data":{"count":1,"games":[{"id":2,"game_title":"Gold Rush!","platform":1,"overview":"The game."}]},
				 "include":{"boxart":{"base_url":{"large":"https://cdn.test/large/"},
					"data":{"2":[{"side":"front","filename":"boxart/front/2-1.jpg"}]}}}}`))
			default:
				w.Write([]byte(`{"data":{"count":0,"games":[]},"include":{"boxart":{"base_url":{},"data":{}}}}`))
			}
		}))
		defer srv.Close()

		p := testProvider(srv)
		q := media.GameQuery{Title: "Gold Rush!"}

		t.Run("WHEN its covers are searched", func(t *testing.T) {
			names = nil
			got, err := p.Covers(ctx, q, schema.Settings{settingAPIKey: "k"})
			require.NoError(t, err)

			t.Run("THEN the raw title is searched too and its own box wins", func(t *testing.T) {
				assert.Equal(t, []string{"Rush!", "Gold Rush!"}, names)
				require.NotEmpty(t, got)
				assert.Equal(t, "https://cdn.test/large/boxart/front/2-1.jpg", got[0].URL)
			})
		})

		t.Run("WHEN its sheet is asked", func(t *testing.T) {
			names = nil
			got, err := NewDetails(p).Details(ctx, q, "en", schema.Settings{settingAPIKey: "k"})
			require.NoError(t, err)

			t.Run("THEN it is the sheet of the game named exactly so", func(t *testing.T) {
				assert.Equal(t, []string{"Rush!", "Gold Rush!"}, names)
				require.NotNil(t, got)
				assert.Equal(t, "The game.", got.Summary)
			})
		})
	})
}
