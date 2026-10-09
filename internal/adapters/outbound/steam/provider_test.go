package steam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/source"
)

// fakeSteamAPI answers like the Steam Web API (2026-10): a wrong key is an HTTP 403, a private
// profile an empty response, GetOwnedGames without include_appinfo only counts.
func fakeSteamAPI(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("key") != "good" {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`<html><head><title>Forbidden</title></head></html>`))

			return
		}

		switch r.URL.Path {
		case "/ISteamUser/ResolveVanityURL/v1/":
			if q.Get("vanityurl") == "gaben" {
				w.Write([]byte(`{"response":{"steamid":"76561197960287930","success":1}}`))
				return
			}

			w.Write([]byte(`{"response":{"success":42,"message":"No match"}}`))
		case "/IPlayerService/GetOwnedGames/v1/":
			if q.Get("include_appinfo") == "1" {
				t.Errorf("a connection test must not download the library")
			}

			if q.Get("steamid") == "76561197960287930" {
				w.Write([]byte(`{"response":{"game_count":679}}`))
				return
			}

			w.Write([]byte(`{"response":{}}`)) // private game details
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestTest_countsWithoutDownloading(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv := fakeSteamAPI(t)
	p := NewProvider()
	p.API.BaseURL, p.API.HTTP = srv.URL, srv.Client()

	t.Run("GIVEN a valid key and a public profile URL", func(t *testing.T) {
		settings := source.Settings{settingAPIKey: "good", settingProfile: "https://steamcommunity.com/id/gaben/"}

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := p.Test(ctx, settings)

			t.Run("THEN it works without downloading the library", func(t *testing.T) {
				require.NoError(t, err)
			})
		})
	})

	t.Run("GIVEN a profile whose game details are private", func(t *testing.T) {
		settings := source.Settings{settingAPIKey: "good", settingProfile: "76561198000000000"}

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := p.Test(ctx, settings)

			t.Run("THEN it explains the privacy setting", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrPrivateProfile)
			})
		})
	})

	t.Run("GIVEN a key Steam rejects", func(t *testing.T) {
		settings := source.Settings{settingAPIKey: "bad", settingProfile: "gaben"}

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := p.Test(ctx, settings)

			t.Run("THEN it says the key was rejected", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "rejected the API key")
			})
		})
	})
}
