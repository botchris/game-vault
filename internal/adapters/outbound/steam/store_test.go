package steam

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
			w.Write([]byte(`{"total":2,"items":` + items[r.URL.Query().Get("term")] + `}`))
		default:
			w.WriteHeader(http.StatusInternalServerError) // browse API down: legacy paths
		}
	}))
	t.Cleanup(srv.Close)

	s := testStore(srv)
	s.CDNURL = "https://legacy.test"

	return s, &searched
}

func TestCovers_titleSearch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	pcKey := media.CoverQuery{
		Title:     "Example Quest™",
		System:    game.SystemPC,
		Platforms: []string{"Epic Games"},
		Fallback:  true,
	}

	t.Run("GIVEN a PC key without a Steam AppID, of a game the store has under the same title", func(t *testing.T) {
		s, searched := titleSearchStore(t, map[string]string{
			"Example Quest™": `[{"type":"app","name":"Example Quest","id":4242,"tiny_image":"https://img.test/4242.jpg"},
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
				assert.Equal(t, []string{"Example Quest™"}, *searched)
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

	t.Run("GIVEN titles the store has only inexactly, or twice", func(t *testing.T) {
		s, _ := titleSearchStore(t, map[string]string{
			"Example Quest™": `[{"type":"app","name":"Example Quest 2","id":4244}]`,
			"Twin Quest": `[{"type":"app","name":"Twin Quest","id":1},
				{"type":"app","name":"TWIN QUEST (1998)","id":2}]`,
		}, 0)

		for name, title := range map[string]string{"an inexact title": "Example Quest™", "an ambiguous title": "Twin Quest"} {
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

	t.Run("GIVEN the store search asks to slow down", func(t *testing.T) {
		s, _ := titleSearchStore(t, nil, http.StatusTooManyRequests)

		t.Run("WHEN covers are searched by title", func(t *testing.T) {
			_, err := s.Covers(ctx, pcKey, schema.Settings{})

			t.Run("THEN it is a rate limit, so no miss is remembered", func(t *testing.T) {
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
