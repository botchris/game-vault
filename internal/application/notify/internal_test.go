package notify

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/event"
	"gamevault/internal/domain/game"
)

func TestFormatMoney(t *testing.T) {
	tests := []struct {
		amount   int64
		currency string
		lang     string
		want     string
	}{
		{999, "EUR", "en", "€9.99"},
		{999, "EUR", "es", "9,99 €"},
		{999, "USD", "en", "$9.99"},
		{999, "USD", "es", "9,99 $"},
		{1505, "GBP", "en", "15.05 GBP"},
		{1505, "GBP", "es", "15,05 GBP"},
		{5, "EUR", "en", "€0.05"},
		{1500, "JPY", "en", "1500 JPY"},
		{12345, "KWD", "es", "12,345 KWD"},
		{999, "EUR", "fr", "€9.99"},
		{999, "", "en", "9.99"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatMoney(event.Money{
				Amount:   tt.amount,
				Currency: tt.currency,
			}, tt.lang)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCodec_roundTrip(t *testing.T) {
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	payloads := map[event.Kind]any{
		event.KindCollectionAdded: event.CollectionAdded{
			Origin:     event.OriginSource,
			OriginName: "Example Store",
			Games: []event.GameRef{{
				ID:      "g-1",
				Title:   "Example Game",
				Systems: []string{"PC"},
			}},
			CopiesAdded: 1,
		},
		event.KindReleaseAnnounced: event.Release{
			Title:     "Example Sequel",
			Date:      "2026-11-20",
			Platforms: []string{"PC"},
			Because:   "g-1",
		},
		event.KindReleaseOut: event.Release{
			Title:     "Example Sequel",
			Platforms: []string{},
		},
		event.KindHistoricalLow: event.HistoricalLowOf(game.Deal{
			Title: "Example Game",
			Price: game.Money{
				Amount:   999,
				Currency: "EUR",
			},
			Expires: at.Add(48 * time.Hour),
		}),
		event.KindIssueOpened: event.Issue{
			Key:    "disk:config",
			Kind:   "disk",
			Reason: "disk_low",
			Params: map[string]string{"free": "2 GB"},
			Since:  at,
		},
		event.KindIssueResolved: event.Issue{
			Key:    "disk:config",
			Params: map[string]string{},
		},
		event.KindKeysExpiring: event.KeysExpiring{Keys: []event.ExpiringKey{{
			Game: event.GameRef{
				ID:      "g-1",
				Title:   "Example Game",
				Systems: []string{},
			},
			Store:    "Steam",
			RedeemBy: "2026-10-22",
			DaysLeft: 7,
		}}},
		event.KindFailedLogins: event.FailedLogins{
			Address: "192.0.2.7",
			Count:   3,
			First:   at,
			Last:    at.Add(time.Minute),
		},
	}

	for _, kind := range event.Kinds() {
		t.Run("GIVEN a "+string(kind)+" event", func(t *testing.T) {
			data, ok := payloads[kind]
			require.True(t, ok, "add a payload for %s", kind)

			e := event.New(kind, at, data)

			t.Run("WHEN it is encoded and decoded THEN it comes back the same", func(t *testing.T) {
				raw, err := EncodeEvent(e)
				require.NoError(t, err)

				got, err := DecodeEvent(raw)
				require.NoError(t, err)
				assert.Equal(t, e, got)
			})
		})
	}

	t.Run("GIVEN payloads with nil slices and maps WHEN encoded THEN they are empty lists, never null", func(t *testing.T) {
		for _, e := range []event.Event{
			event.New(event.KindCollectionAdded, at, event.CollectionAdded{Games: []event.GameRef{{ID: "g-1"}}}),
			event.New(event.KindCollectionAdded, at, event.CollectionAdded{}),
			event.New(event.KindReleaseOut, at, event.Release{}),
			event.New(event.KindIssueOpened, at, event.Issue{}),
			event.New(event.KindKeysExpiring, at, event.KeysExpiring{}),
			event.New(event.KindKeysExpiring, at, event.KeysExpiring{Keys: []event.ExpiringKey{{}}}),
		} {
			raw, err := EncodeEvent(e)
			require.NoError(t, err)
			assert.NotContains(t, raw, "null", e.Kind)
		}
	})

	t.Run("GIVEN the stored form THEN its names are id, kind, at and data", func(t *testing.T) {
		raw, err := EncodeEvent(event.Event{
			ID:   "e-1",
			Kind: event.KindFailedLogins,
			At:   at,
			Data: event.FailedLogins{
				Address: "192.0.2.7",
				Count:   3,
				First:   at,
				Last:    at,
			},
		})
		require.NoError(t, err)
		assert.JSONEq(t, `{"id":"e-1","kind":"auth.failed_logins","at":"2026-10-15T10:00:00Z",
			"data":{"address":"192.0.2.7","count":3,"first":"2026-10-15T10:00:00Z","last":"2026-10-15T10:00:00Z"}}`, raw)
	})

	t.Run("GIVEN a payload that does not match its kind WHEN encoded THEN it is refused", func(t *testing.T) {
		_, err := EncodeEvent(event.New(event.KindIssueOpened, at, event.FailedLogins{}))
		assert.Error(t, err)
	})

	t.Run("GIVEN an unknown kind WHEN decoded THEN it is refused", func(t *testing.T) {
		_, err := DecodeEvent(`{"id":"e-1","kind":"collection.removed","at":"2026-10-15T10:00:00Z","data":{}}`)
		assert.Error(t, err)

		_, err = decodeData("collection.removed", json.RawMessage(`{}`))
		assert.Error(t, err)
	})

	t.Run("GIVEN broken JSON WHEN decoded THEN it is refused", func(t *testing.T) {
		_, err := DecodeEvent(`{"id":`)
		assert.Error(t, err)

		_, err = decodeData(event.KindIssueOpened, json.RawMessage(`[1]`))
		assert.Error(t, err)
	})
}
