package fanatical

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// keysAnswer has the shape of GET /api/user/keys as a real account returned it on 2026-10-09
// (fake ids, fields the importer does not read left out): keys bought on their own or split from a
// bundle (bundleName), and the bundle purchase itself, "fulfilled" and without a serial. The
// rejections are the real answers to a bad session (plain text, HTTP 401).
const keysAnswer = `[
 {"_id":"k1","name":"Tunic","type":"game","status":"unrevealed","serialId":"s1","drm":{"steam":true,"gog":false},
  "serialExpiry":"2027-03-01T00:00:00.000Z","purchased":"2025-11-20T10:00:00.000Z","order":{"_id":"o1"}},
 {"_id":"k2","name":"Alan Wake","type":"game","status":"revealed","serialId":"s2","key":"AAAAA-BBBBB-CCCCC","drm":{"epicgames":true},"serialExpiry":null},
 {"_id":"k3","name":"Tunic - Soundtrack","type":"audio","serialId":"s3","drm":{"redeem":true}},
 {"_id":"k4","name":"Overlord: Raising Hell DLC","type":"dlc","status":"revealed","serialId":"s4","drm":{"steam":true},
  "bundleName":"Overlord: Ultimate Evil Collection","bid":"b1"},
 {"_id":"k5","name":"Photo Editor","type":"software","serialId":"s5","drm":{"magix":true}},
 {"_id":"k6","name":"Overlord II","type":"game","status":"revealed","serialId":"s6","drm":{"steam":true},
  "bundleName":"Overlord: Ultimate Evil Collection","bid":"b1","purchased":"2021-07-11T19:09:57.915Z"},
 {"_id":"b1","name":"Overlord: Ultimate Evil Collection","type":"game","status":"fulfilled","drm":{"steam":true},
  "bundles":[],"payment":{"total":89},"purchased":"2020-10-05T08:39:43.558Z"}
]`

func fakeFanatical(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")

		switch {
		case r.URL.Path != "/api/user/keys":
			http.NotFound(w, r)
		case auth == "":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Missing authorization header"))
		case auth != "eyJgood.token.sig":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Invalid authorization header"))
		default:
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(keysAnswer))
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestFetch_readsTheKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv := fakeFanatical(t)
	p := &Provider{BaseURL: srv.URL, Client: srv.Client()}

	t.Run("GIVEN the whole bsauth value pasted from Local Storage", func(t *testing.T) {
		settings := source.Settings{
			settingConsent: schema.ConsentGiven,
			settingSession: `{"authenticated":true,"token":"eyJgood.token.sig","email":"x@example.com"}`,
		}

		t.Run("WHEN the account is scanned", func(t *testing.T) {
			copies, warnings, err := p.Fetch(ctx, settings)
			require.NoError(t, err)

			t.Run("THEN each game key is a copy on the store it is for", func(t *testing.T) {
				require.Len(t, copies, 4)
				assert.Equal(t, "fanatical:k1", copies[0].ExternalID)
				assert.Equal(t, "Tunic", copies[0].Title)
				assert.Equal(t, game.KindKey, copies[0].Details.Kind)
				assert.Equal(t, "Steam", copies[0].Details.Platform)
				assert.Equal(t, "Epic Games", copies[1].Details.Platform)
			})

			t.Run("AND the deadline, purchase date and state come from the key", func(t *testing.T) {
				assert.Equal(t, game.Date("2027-03-01"), copies[0].Details.RedeemBy)
				assert.Equal(t, game.Date("2025-11-20"), copies[0].Details.AcquiredOn)
				assert.Equal(t, game.StatusUnrevealed, copies[0].Details.Status)
				assert.Equal(t, game.StatusRevealed, copies[1].Details.Status)
				assert.Empty(t, copies[0].Details.Key)
				assert.Equal(t, "AAAAA-BBBBB-CCCCC", copies[1].Details.Key, "a revealed key is copied")
			})

			t.Run("AND a key from a bundle names it in its origin", func(t *testing.T) {
				assert.Equal(t, "Fanatical", copies[0].Details.Origin)
				assert.Equal(t, "Overlord II", copies[2].Title)
				assert.Equal(t, "Fanatical – Overlord: Ultimate Evil Collection", copies[2].Details.Origin)
			})

			t.Run("AND the bundle purchase is not a key: it is withdrawn, so an earlier import of it goes", func(t *testing.T) {
				assert.Equal(t, "fanatical:b1", copies[3].ExternalID)
				assert.True(t, copies[3].Withdrawn)
			})

			t.Run("AND DLC, software and audio are left out with a warning", func(t *testing.T) {
				require.Len(t, warnings, 1)
				assert.Contains(t, warnings[0], "3 Fanatical items")
			})
		})
	})

	t.Run("GIVEN a session Fanatical no longer accepts", func(t *testing.T) {
		settings := source.Settings{settingConsent: schema.ConsentGiven, settingSession: "eyJexpired"}

		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := p.Test(ctx, settings)

			t.Run("THEN it asks to sign in again", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrSignedOut)
			})
		})
	})

	t.Run("GIVEN nothing pasted", func(t *testing.T) {
		t.Run("WHEN the connection is tested", func(t *testing.T) {
			err := p.Test(ctx, source.Settings{settingConsent: schema.ConsentGiven, settingSession: "  "})

			t.Run("THEN it says what to paste, without asking Fanatical", func(t *testing.T) {
				assert.ErrorIs(t, err, ErrNoSession)
			})
		})
	})
}

func TestDescriptor_needsConsent(t *testing.T) {
	d := NewProvider().Descriptor()

	t.Run("GIVEN settings with a session but the risk not accepted", func(t *testing.T) {
		err := d.Validate(source.Settings{settingSession: "eyJgood.token.sig"})

		t.Run("THEN they are refused before any request", func(t *testing.T) {
			require.Error(t, err)
			assert.Contains(t, err.Error(), "accept the risk")
		})

		t.Run("AND new sources default to manual scans", func(t *testing.T) {
			assert.True(t, d.ManualScans)
		})
	})
}
