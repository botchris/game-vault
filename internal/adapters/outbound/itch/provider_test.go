package itch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// The pages follow GET /profile/owned-keys as documented on https://itch.io/docs/api/serverside
// (2026-10-10; fields the importer does not read left out, ids changed). The first page is shorter
// than per_page and still not the last one, which the documentation warns about. The rejections are
// the real answers to a bogus key (HTTP 403) and to no key at all (HTTP 401), probed on 2026-10-10.
const (
	firstPage = `{"page":1,"per_page":500,"owned_keys":[
 {"id":1001,"game_id":341190,"purchase_id":3923754,"downloads":4,"created_at":"2019-03-06T18:52:02Z","updated_at":"2019-03-06T18:52:02Z",
  "game":{"id":341190,"title":"Pikuniku","classification":"game","type":"default","url":"https://sectordub.itch.io/pikuniku"}},
 {"id":1002,"game_id":500001,"downloads":0,"created_at":"2020-06-11T09:00:00Z",
  "game":{"id":500001,"title":"Pixel Font Pack","classification":"assets","type":"default"}},
 {"id":1003,"game_id":500002,"created_at":"2020-06-11T09:00:00Z",
  "game":{"id":500002,"title":"A Short Hike","type":"default"}}
]}`

	secondPage = `{"page":2,"per_page":500,"owned_keys":[
 {"id":1004,"game_id":341190,"created_at":"2018-01-02 10:00:00",
  "game":{"id":341190,"title":"Pikuniku","classification":"game"}},
 {"id":1005,"game_id":500003,"created_at":"2020-06-11T09:00:00Z",
  "game":{"id":500003,"title":"Celeste OST","classification":"soundtrack"}}
]}`

	emptyPage = `{"page":3,"per_page":500,"owned_keys":[]}`
)

// fakeItch reproduces api.itch.io's owned keys endpoint and its failures.
func fakeItch(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch auth := r.Header.Get("Authorization"); {
		case r.URL.Path != "/profile/owned-keys":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":["invalid route"]}`))
		case auth == "":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"errors":["authentication required"]}`))
		case auth == "Bearer scoped-key":
			w.Write([]byte(`{"errors":["invalid scope: profile:owned"]}`))
		case auth != "Bearer good-key":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":["invalid key"]}`))
		case r.URL.Query().Get("page") == "1":
			w.Write([]byte(firstPage))
		case r.URL.Query().Get("page") == "2":
			w.Write([]byte(secondPage))
		default:
			w.Write([]byte(emptyPage))
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()

	srv := fakeItch(t)
	p := NewProvider()
	p.API.BaseURL = srv.URL
	p.API.HTTP = srv.Client()

	return p
}

func TestProvider_Fetch(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN an account whose keys span two pages and an empty one", func(t *testing.T) {
		p := newTestProvider(t)

		t.Run("WHEN it is scanned", func(t *testing.T) {
			copies, warnings, err := p.Fetch(ctx, source.Settings{settingAPIKey: " good-key "})
			require.NoError(t, err)

			t.Run("THEN every game is one library copy, linked to itch.io", func(t *testing.T) {
				require.Len(t, copies, 2)
				assert.Equal(t, game.ImportedCopy{
					ExternalID: "itch:341190",
					Title:      "Pikuniku",
					Links:      game.Links{"itch": "341190"},
					Details: game.CopyDetails{
						Kind:       game.KindLibrary,
						Platform:   "itch.io",
						Status:     game.StatusOwned,
						Origin:     "itch.io bundle, claim or gift",
						AcquiredOn: "2018-01-02",
					},
				}, copies[0])

				t.Run("AND a game held twice is dated by its earliest key", func(t *testing.T) {
					assert.Equal(t, game.Date("2018-01-02"), copies[0].Details.AcquiredOn)
				})

				t.Run("AND a game without a classification is still imported", func(t *testing.T) {
					assert.Equal(t, "A Short Hike", copies[1].Title)
					assert.Equal(t, "itch.io bundle, claim or gift", copies[1].Details.Origin)
				})
			})

			t.Run("AND assets and soundtracks are skipped with a warning", func(t *testing.T) {
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], "2 itch.io items")
			})
		})
	})
}

func TestProvider_errors(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the itch.io API", func(t *testing.T) {
		p := newTestProvider(t)

		t.Run("WHEN the key works THEN the test passes", func(t *testing.T) {
			assert.NoError(t, p.Test(ctx, source.Settings{settingAPIKey: "good-key"}))
		})

		t.Run("WHEN no key is set THEN it asks for one without calling the API", func(t *testing.T) {
			assert.ErrorIs(t, p.Test(ctx, source.Settings{settingAPIKey: "  "}), ErrNoKey)
		})

		t.Run("WHEN itch.io rejects the key THEN it says to create a new one", func(t *testing.T) {
			assert.ErrorIs(t, p.Test(ctx, source.Settings{settingAPIKey: "revoked-key"}), ErrBadKey)

			_, _, err := p.Fetch(ctx, source.Settings{settingAPIKey: "revoked-key"})
			assert.ErrorIs(t, err, ErrBadKey)
		})

		t.Run("WHEN itch.io answers 200 with an errors list THEN the scan fails with its message", func(t *testing.T) {
			_, _, err := p.Fetch(ctx, source.Settings{settingAPIKey: "scoped-key"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid scope: profile:owned")
		})
	})
}
