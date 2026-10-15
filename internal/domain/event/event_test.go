package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

// TestKinds_catalog pins the stored kind names and their groups: subscriptions and the outbox keep
// these strings, so a rename would silently drop what users chose.
func TestKinds_catalog(t *testing.T) {
	want := map[event.Kind]event.Group{
		"collection.added":    event.GroupActivity,
		"release.announced":   event.GroupActivity,
		"release.out":         event.GroupActivity,
		"deal.historical_low": event.GroupActivity,
		"issue.opened":        event.GroupAttention,
		"issue.resolved":      event.GroupAttention,
		"keys.expiring":       event.GroupAttention,
		"auth.failed_logins":  event.GroupSecurity,
	}

	got := map[event.Kind]event.Group{}
	for _, k := range event.Kinds() {
		got[k] = event.GroupOf(k)
		assert.True(t, event.Known(k), k)
	}

	assert.Equal(t, want, got)
	assert.False(t, event.Known("collection.removed"))
}

// TestPayloads_jsonNames pins the JSON names of the payloads: the queue stores them and the webhook
// sends them, so a renamed field would break stored deliveries and the receivers' parsers.
func TestPayloads_jsonNames(t *testing.T) {
	t.Run("GIVEN a collection.added payload", func(t *testing.T) {
		added := event.CollectionAdded{
			Origin:     event.OriginSource,
			OriginName: "Example Store",
			Games: []event.GameRef{{
				ID:      "g-1",
				Title:   "Example Game",
				Systems: []string{"PC"},
			}},
			CopiesAdded: 2,
		}

		t.Run("WHEN it is marshaled", func(t *testing.T) {
			raw, err := json.Marshal(added)
			require.NoError(t, err)

			t.Run("THEN it uses the lower-camel names", func(t *testing.T) {
				assert.JSONEq(t, `{"origin":"source","originName":"Example Store","copiesAdded":2,
					"games":[{"id":"g-1","title":"Example Game","systems":["PC"]}]}`, string(raw))
			})
		})
	})

	t.Run("GIVEN a deal at its historical low", func(t *testing.T) {
		low := event.HistoricalLowOf(game.Deal{
			ProviderGameID: "p-1",
			Title:          "Example Game",
			SteamAppID:     "10",
			Shop:           "Example Shop",
			Price: game.Money{
				Amount:   999,
				Currency: "EUR",
			},
			Regular: game.Money{
				Amount:   1999,
				Currency: "EUR",
			},
			Cut:           50,
			HistoricalLow: true,
			URL:           "https://deals.example/g",
			ImageURL:      "https://img.example/g.jpg",
		})

		t.Run("WHEN it is marshaled", func(t *testing.T) {
			raw, err := json.Marshal(low)
			require.NoError(t, err)

			t.Run("THEN it uses its own names and leaves out an unknown end", func(t *testing.T) {
				assert.JSONEq(t, `{"providerGameId":"p-1","title":"Example Game","shop":"Example Shop",
					"price":{"amount":999,"currency":"EUR"},"regular":{"amount":1999,"currency":"EUR"},
					"cut":50,"historicalLow":true,"url":"https://deals.example/g",
					"imageUrl":"https://img.example/g.jpg"}`, string(raw))
			})
		})

		t.Run("WHEN the deal has an end THEN it is kept", func(t *testing.T) {
			end := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
			low := event.HistoricalLowOf(game.Deal{Expires: end})
			assert.Equal(t, end, low.Expires)
		})
	})
}
