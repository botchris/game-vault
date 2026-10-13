package steam

import (
	"cmp"
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
	s.throttle = nil // tests send at once; ratelimit tests the throttle

	return s
}

// titleSearchStore is a Steam store whose search answers items for a term, and whose browse API
// knows the art of every app (legacy paths otherwise).
func titleSearchStore(t *testing.T, items map[string]string, status int) (*Store, *[]string) {
	t.Helper()

	var searched []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/storesearch/":
			searched = append(searched, r.URL.Query().Get("term"))

			if status != 0 {
				w.WriteHeader(status)
				return
			}
			// Real answer shape of the store search (values invented).
			w.Write([]byte(`{"total":2,"items":` + cmp.Or(items[r.URL.Query().Get("term")], "[]") + `}`))
		default:
			w.WriteHeader(http.StatusInternalServerError) // browse API down: legacy paths
		}
	}))
	t.Cleanup(srv.Close)

	s := testStore(srv)
	s.CDNURL = "https://legacy.test"

	return s, &searched
}

func TestUniqueApp_otherScripts(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a store search whose only app is another game titled in a script MatchKey drops", func(t *testing.T) {
		s, _ := titleSearchStore(t, map[string]string{
			"ファイナルクエスト": `[{"type":"app","name":"ドラゴンクエスト","id":777}]`,
		}, 0)

		t.Run("WHEN the app of a game titled in such a script is looked up, loosely", func(t *testing.T) {
			id, err := s.uniqueApp(ctx, "ファイナルクエスト", "ファイナルクエスト", true)

			t.Run("THEN none is found: two empty match keys are not the same game", func(t *testing.T) {
				require.NoError(t, err)
				assert.Zero(t, id)
			})
		})
	})
}

func TestCovers_titleSearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	pcKey := media.CoverQuery{
		Title:     "Example Quest™ GOTY",
		System:    game.SystemPC,
		Platforms: []string{"Epic Games"},
		Fallback:  true,
	}

	t.Run("GIVEN a PC key without a Steam AppID, of a game the store has under the same title", func(t *testing.T) {
		s, searched := titleSearchStore(t, map[string]string{
			"Example Quest": `[{"type":"app","name":"Example Quest","id":4242,"tiny_image":"https://img.test/4242.jpg"},
				{"type":"app","name":"Example Quest: Soundtrack","id":4243}]`,
		}, 0)

		t.Run("WHEN its covers are asked on the fallback pass", func(t *testing.T) {
			require.True(t, s.Applies(pcKey))
			got, err := s.Covers(ctx, pcKey, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN the exact match's art is proposed", func(t *testing.T) {
				require.Len(t, got, 2)
				assert.Equal(t, "https://legacy.test/steam/apps/4242/library_600x900.jpg", got[0].URL)
				assert.Equal(t, CoverProviderID, got[0].Provider)
				assert.Equal(t, []string{"Example Quest"}, *searched, "the game's name, without the edition tag")
			})
		})

		t.Run("WHEN it is the first pass", func(t *testing.T) {
			first := pcKey
			first.Fallback = false

			t.Run("THEN Steam does not apply: the search is kept for the fallback pass", func(t *testing.T) {
				assert.False(t, s.Applies(first))
			})
		})
	})

	t.Run("GIVEN a title with ' - ' where the store names the game with ': '", func(t *testing.T) {
		s, _ := titleSearchStore(t, map[string]string{
			"Example - A Wild Adventure": `[{"type":"app","name":"Example: A Wild Adventure","id":4300,"tiny_image":"https://img.test/4300.jpg"}]`,
		}, 0)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			q := pcKey
			q.Title = "Example - A Wild Adventure"
			got, err := s.Covers(ctx, q, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN it is the same title: only the punctuation differs", func(t *testing.T) {
				require.NotEmpty(t, got)
				assert.Equal(t, "https://legacy.test/steam/apps/4300/library_600x900.jpg", got[0].URL)
			})
		})
	})

	t.Run("GIVEN titles the store has only inexactly, or twice", func(t *testing.T) {
		s, _ := titleSearchStore(t, map[string]string{
			"Example Quest": `[{"type":"app","name":"Example Quest 2","id":4244}]`,
			"Twin Quest": `[{"type":"app","name":"Twin Quest","id":1},
				{"type":"app","name":"TWIN QUEST","id":2}]`,
			"Remake Quest": `[{"type":"app","name":"Remake Quest: Definitive Edition","id":3},
				{"type":"app","name":"Remake Quest (2016)","id":4}]`,
		}, 0)

		for name, title := range map[string]string{
			"an inexact title":                           "Example Quest™ GOTY",
			"a title two apps have exactly":              "Twin Quest",
			"a title only editions have, more than once": "Remake Quest",
		} {
			t.Run("WHEN the covers of "+name+" are asked", func(t *testing.T) {
				q := pcKey
				q.Title = title
				got, err := s.Covers(ctx, q, schema.Settings{})

				t.Run("THEN there are none rather than another game's", func(t *testing.T) {
					require.NoError(t, err)
					assert.Empty(t, got)
				})
			})
		}
	})

	t.Run("GIVEN a title whose tag-like word is part of the name", func(t *testing.T) {
		s, searched := titleSearchStore(t, map[string]string{
			"Rush!":      `[]`,
			"Gold Rush!": `[{"type":"app","name":"Gold Rush!","id":5050}]`,
		}, 0)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			q := pcKey
			q.Title = "Gold Rush!"
			got, err := s.Covers(ctx, q, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN the raw title is searched after the name found no match", func(t *testing.T) {
				require.NotEmpty(t, got)
				assert.Equal(t, "https://legacy.test/steam/apps/5050/library_600x900.jpg", got[0].URL)
				assert.Equal(t, []string{"Rush!", "Gold Rush!"}, *searched)
			})
		})
	})

	t.Run("GIVEN a trademarked title without tags", func(t *testing.T) {
		s, searched := titleSearchStore(t, map[string]string{"Plain Quest": `[]`}, 0)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			q := pcKey
			q.Title = "Plain Quest™"
			_, err := s.Covers(ctx, q, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN it is searched once", func(t *testing.T) {
				assert.Equal(t, []string{"Plain Quest"}, *searched)
			})
		})
	})

	t.Run("GIVEN a title the store has exactly, and also as a remake", func(t *testing.T) {
		s, _ := titleSearchStore(t, map[string]string{
			"Mafia": `[{"type":"app","name":"Mafia: Definitive Edition","id":1030840},
				{"type":"app","name":"Mafia","id":40990}]`,
		}, 0)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			q := pcKey
			q.Title = "Mafia"
			got, err := s.Covers(ctx, q, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN the exact title wins over the remake that only matches loosely", func(t *testing.T) {
				require.NotEmpty(t, got)
				assert.Equal(t, "https://legacy.test/steam/apps/40990/library_600x900.jpg", got[0].URL)
			})
		})
	})

	for name, status := range map[string]int{"asks to slow down": http.StatusTooManyRequests, "is down": http.StatusBadGateway} {
		t.Run("GIVEN the store search "+name, func(t *testing.T) {
			s, _ := titleSearchStore(t, nil, status)

			t.Run("WHEN covers are searched by title", func(t *testing.T) {
				_, err := s.Covers(ctx, pcKey, schema.Settings{})

				t.Run("THEN it counts as limited, so the edition is not marked missing for days", func(t *testing.T) {
					require.ErrorIs(t, err, provider.ErrRateLimited)
					assert.NotContains(t, strings.ToLower(err.Error()), "steam", "the caller names the provider")
				})
			})
		})
	}

	t.Run("GIVEN the same title searched twice in a row (Battle.net's covers, then Steam's)", func(t *testing.T) {
		s, searched := titleSearchStore(t, map[string]string{"Example Quest": `[]`}, 0)

		t.Run("WHEN both searches run", func(t *testing.T) {
			_, err := s.SearchLinks(ctx, "Example Quest")
			require.NoError(t, err)

			_, err = s.Covers(ctx, pcKey, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN the store is asked once for that name (then for the raw title, as nothing matched)", func(t *testing.T) {
				assert.Equal(t, []string{"Example Quest", "Example Quest GOTY"}, *searched)
			})
		})
	})

	t.Run("GIVEN a title with a year, which Battle.net searches without it", func(t *testing.T) {
		s, searched := titleSearchStore(t, map[string]string{"Year Quest": `[]`}, 0)

		t.Run("WHEN Battle.net's search and Steam's title search run", func(t *testing.T) {
			_, err := s.SearchLinks(ctx, "Year Quest")
			require.NoError(t, err)

			q := pcKey
			q.Title = "  year quest (2016) "
			_, err = s.Covers(ctx, q, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN they share one request", func(t *testing.T) {
				assert.Equal(t, []string{"Year Quest"}, *searched)
			})
		})
	})

	t.Run("GIVEN a store host that answers HTTP 429", func(t *testing.T) {
		s, searched := titleSearchStore(t, nil, http.StatusTooManyRequests)
		s.throttle = newThrottle(0, nil, nil)

		t.Run("WHEN two titles are searched one after the other", func(t *testing.T) {
			_, first := s.SearchLinks(ctx, "Example Quest")
			_, second := s.SearchLinks(ctx, "Other Quest")

			t.Run("THEN both are rate-limited", func(t *testing.T) {
				require.ErrorIs(t, first, provider.ErrRateLimited)
				require.ErrorIs(t, second, provider.ErrRateLimited)

				t.Run("AND the second is not sent: the store host is left alone for a while", func(t *testing.T) {
					assert.Equal(t, []string{"Example Quest"}, *searched)
				})
			})
		})
	})

	t.Run("GIVEN the store host's requests are already queued past the longest wait", func(t *testing.T) {
		s, searched := titleSearchStore(t, nil, 0)
		s.throttle = newThrottle(time.Hour, nil, nil)

		for range storeBurst {
			require.NoError(t, s.throttle.Do(ctx, func() error { return nil }))
		}

		t.Run("WHEN a linked game's covers are asked", func(t *testing.T) {
			got, err := s.Covers(ctx, media.CoverQuery{
				Title:  "Portal 2",
				System: game.SystemPC,
				Links:  game.Links{game.LinkSteam: "620"},
			}, schema.Settings{})

			t.Run("THEN the art lookup is not held up: the Web API has its own, higher limits", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, got, 2)
				assert.Equal(t, "https://legacy.test/steam/apps/620/library_600x900.jpg", got[0].URL, "browse API down in this fake: the guessed URLs, as before")
			})
		})

		t.Run("WHEN a title is searched", func(t *testing.T) {
			_, err := s.SearchLinks(ctx, "Example Quest")

			t.Run("THEN the store host's throttle answers rate-limited without a request", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
				assert.Empty(t, *searched)
			})
		})

		t.Run("WHEN a Steam sheet is asked", func(t *testing.T) {
			_, err := NewDetails(s).Details(ctx, media.CoverQuery{Links: game.Links{game.LinkSteam: "620"}}, "en", schema.Settings{})

			t.Run("THEN the store page waits its turn too: rate-limited, so the sheet is not cached", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrRateLimited)
			})
		})
	})

	t.Run("GIVEN a game linked to Steam", func(t *testing.T) {
		s, searched := titleSearchStore(t, nil, 0)

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := s.Covers(ctx, media.CoverQuery{
				Title:  "Portal 2",
				System: game.SystemPC,
				Links:  game.Links{game.LinkSteam: "620"},
			}, schema.Settings{})
			require.NoError(t, err)

			t.Run("THEN its AppID is used, without a search, as before", func(t *testing.T) {
				require.Len(t, got, 2)
				assert.Equal(t, "https://legacy.test/steam/apps/620/library_600x900.jpg", got[0].URL)
				assert.Empty(t, *searched)
			})
		})
	})
}

func TestStore_findLinks(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	disc := media.CoverQuery{
		Title:     "Example Quest",
		Platforms: []string{"PS4"},
	}

	t.Run("GIVEN a store with exactly that title", func(t *testing.T) {
		s, _ := titleSearchStore(t, map[string]string{"Example Quest": `[{"type":"app","name":"Example Quest","id":4242}]`}, 0)

		t.Run("THEN its AppID is the game's Steam link", func(t *testing.T) {
			got, err := s.FindLinks(ctx, disc, schema.Settings{})
			require.NoError(t, err)
			assert.Equal(t, game.Links{game.LinkSteam: "4242"}, got)
		})
	})

	t.Run("GIVEN a store with that title twice", func(t *testing.T) {
		s, _ := titleSearchStore(t, map[string]string{"Example Quest": `[{"type":"app","name":"Example Quest","id":1},
			{"type":"app","name":"EXAMPLE QUEST","id":2}]`}, 0)

		t.Run("THEN no link is guessed", func(t *testing.T) {
			got, err := s.FindLinks(ctx, disc, schema.Settings{})
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	// Cases the review raised (a remake's 2005 namesake for a PS2 disc, a 1993 game for a PS1 one, a
	// remaster found by MatchKey), with invented titles.
	t.Run("GIVEN a PS2 disc whose title the store has exactly", func(t *testing.T) {
		s, searched := titleSearchStore(t, map[string]string{"Example Horror 4": `[{"type":"app","name":"Example Horror 4","id":7}]`}, 0)

		t.Run("THEN no link is saved, nor searched: Steam sells the PC game, often a later one", func(t *testing.T) {
			got, err := s.FindLinks(ctx, media.CoverQuery{
				Title:             "Example Horror 4",
				PhysicalPlatforms: []string{"PS2"},
				Platforms:         []string{"PS2"},
			}, schema.Settings{})
			require.NoError(t, err)
			assert.Empty(t, got)
			assert.Empty(t, *searched)
		})
	})

	t.Run("GIVEN a PS1 disc, and handhelds", func(t *testing.T) {
		s, searched := titleSearchStore(t, map[string]string{"EXAMPLE DOOM": `[{"type":"app","name":"EXAMPLE DOOM","id":8}]`}, 0)

		t.Run("THEN no link is saved for any of them", func(t *testing.T) {
			for _, platform := range []string{"PS1", "DS", "3DS", "PSP", "PS Vita", "Xbox 360", "Wii"} {
				got, err := s.FindLinks(ctx, media.CoverQuery{
					Title:     "EXAMPLE DOOM",
					Platforms: []string{platform},
				}, schema.Settings{})
				require.NoError(t, err)
				assert.Empty(t, got, platform)
			}

			assert.Empty(t, *searched)
		})
	})

	t.Run("GIVEN a PS4 disc the store has only as a remaster", func(t *testing.T) {
		s, _ := titleSearchStore(t, map[string]string{"Example Mafia": `[{"type":"app","name":"Example Mafia: Definitive Edition","id":9}]`}, 0)

		t.Run("THEN no link is saved: only an exact title counts for a saved link", func(t *testing.T) {
			got, err := s.FindLinks(ctx, media.CoverQuery{
				Title:     "Example Mafia",
				Platforms: []string{"PS4"},
			}, schema.Settings{})
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("GIVEN a game linked to Steam already", func(t *testing.T) {
		s, searched := titleSearchStore(t, nil, 0)

		t.Run("THEN nothing is searched", func(t *testing.T) {
			q := disc
			q.Links = game.Links{game.LinkSteam: "620"}
			got, err := s.FindLinks(ctx, q, schema.Settings{})
			require.NoError(t, err)
			assert.Empty(t, got)
			assert.Empty(t, *searched)
		})
	})
}
