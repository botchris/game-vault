package example

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/application/media"
	"gamevault/internal/application/plugin/plugintest"
	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// libraryAnswer is what the store's API answers (with a real store, paste a real answer here with
// the values changed, and say where it came from). The rejection is the store's answer to a bad
// token: plain text, HTTP 401.
const libraryAnswer = `{"items": [
 {"id": 101, "title": "Tunic", "type": "game", "owned": true},
 {"id": 102, "title": "Tunic Soundtrack", "type": "soundtrack", "owned": true},
 {"id": 103, "title": "Hades", "type": "game", "owned": false}
]}`

const productAnswer = `{"title": "Tunic", "art": {
 "portrait": "https://images.example-store.invalid/101/portrait.jpg",
 "wide": "https://images.example-store.invalid/101/wide.jpg"}}`

// fakeStore reproduces the store's API, including its failures.
func fakeStore(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/library":
			if r.Header.Get("Authorization") != "Bearer good-token" {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}

			w.Write([]byte(libraryAnswer))
		case "/v1/products/101", "/v1/products/1":
			w.Write([]byte(productAnswer))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestPlugin_isComplete(t *testing.T) {
	t.Run("GIVEN the plugin and the texts it would add to the UI", func(t *testing.T) {
		// A registered plugin is checked against web/src/i18n/locales by cmd/gamevault's test. This one
		// is not registered, so its texts live here instead of in the UI's files.
		keys := map[string]bool{
			"sources.example.description": true, "sources.example.token": true, "sources.example.tokenHelp": true,
			"providers.exampleCovers.description": true,
		}

		t.Run("THEN it passes the checks every plugin passes", func(t *testing.T) {
			assert.Empty(t, plugintest.Problems(Plugin(), plugintest.Translations{"en": keys, "es": keys}))
		})
	})
}

func TestSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv := fakeStore(t)
	p := NewProvider()
	p.API.BaseURL, p.API.HTTP = srv.URL, srv.Client()

	t.Run("GIVEN an account with a game, a soundtrack and a refunded game", func(t *testing.T) {
		settings := source.Settings{settingToken: "good-token"}

		t.Run("WHEN it is scanned", func(t *testing.T) {
			copies, warnings, err := p.Fetch(ctx, settings)
			require.NoError(t, err)

			t.Run("THEN only the owned game is imported, linked to the store", func(t *testing.T) {
				require.Len(t, copies, 1)
				assert.Equal(t, "example:101", copies[0].ExternalID)
				assert.Equal(t, game.Links{"example": "101"}, copies[0].Links)
				assert.Equal(t, game.KindLibrary, copies[0].Details.Kind)
			})

			t.Run("AND the other two are counted in a warning", func(t *testing.T) {
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], "2 Example Store items")
			})
		})
	})

	t.Run("GIVEN a token the store rejects", func(t *testing.T) {
		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := p.Test(ctx, source.Settings{settingToken: "old-token"})

			t.Run("THEN it says what to do", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrSignedOut)
			})
		})
	})

	t.Run("GIVEN no token", func(t *testing.T) {
		t.Run("THEN nothing is asked to the store", func(t *testing.T) {
			assert.ErrorIs(t, p.Test(ctx, source.Settings{}), ErrNoToken)
		})
	})
}

func TestCovers(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv := fakeStore(t)
	c := NewCovers()
	c.API.BaseURL, c.API.HTTP = srv.URL, srv.Client()

	t.Run("GIVEN a game linked to the store, by a scan or by hand", func(t *testing.T) {
		q := media.CoverQuery{
			Title: "Tunic",
			Links: game.Links{"example": "101"},
		}

		t.Run("WHEN its covers are asked", func(t *testing.T) {
			got, err := c.Covers(ctx, q, nil)
			require.NoError(t, err)

			t.Run("THEN the portrait comes first, then the banner", func(t *testing.T) {
				require.Len(t, got, 2)
				assert.Contains(t, got[0].URL, "portrait")
				assert.Equal(t, CoverProviderID, got[0].Provider)
			})
		})
	})

	t.Run("GIVEN a game the store's catalog does not know", func(t *testing.T) {
		got, err := c.Covers(ctx, media.CoverQuery{Links: game.Links{"example": "999"}}, nil)

		t.Run("THEN there are no candidates and no error", func(t *testing.T) {
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("GIVEN a game not linked to the store", func(t *testing.T) {
		t.Run("THEN the provider does not apply", func(t *testing.T) {
			assert.False(t, c.Applies(media.CoverQuery{
				Title: "Tunic",
				Links: game.Links{"steam": "553420"},
			}))
		})
	})
}
