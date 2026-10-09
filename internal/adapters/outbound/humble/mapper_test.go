package humble

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

// order has the shape of /api/v1/orders?all_tpkds=true: keyindex numbers repeated copies of the
// same game and is 0 for almost every key, so only machine_name tells the keys of an order apart.
// Two keys for Hades (keyindex 0 and 1) are what makes keyindex non-zero.
const order = `{"gamekey":"AAA","created":"2023-05-01T10:00:00.000000","product":{"human_name":"Indie Bundle"},
 "tpkd_dict":{"all_tpks":[
  {"machine_name":"hades_steam","human_name":"Hades","key_type":"steam","redeemed_key_val":"ABCDE-FGHIJ","steam_app_id":1145360,"keyindex":0},
  {"machine_name":"hades_steam","human_name":"Hades","key_type":"steam","steam_app_id":1145360,"keyindex":1},
  {"machine_name":"celeste_steam","human_name":"Celeste","key_type":"steam","keyindex":0,"custom_instructions_html":"Please redeem by March 3, 2027."},
  {"machine_name":"old_origin","human_name":"Old","key_type":"origin","keyindex":0,"is_expired":true},
  {"machine_name":"gift_steam","human_name":"Gift","key_type":"steam","keyindex":0,"redeemed_key_val":"https://www.humblebundle.com/gift?key=x"},
  {"machine_name":"soon_steam","human_name":"Soon","key_type":"steam","keyindex":0,"num_days_until_expired":10}]}}`

func TestMapOrders_eachKeyIsOneCopy(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	t.Run("GIVEN an order whose keys all have keyindex 0 except a second copy of one game", func(t *testing.T) {
		t.Run("WHEN it is mapped", func(t *testing.T) {
			copies, _, warnings := MapOrders([]json.RawMessage{json.RawMessage(order)}, now)
			require.Empty(t, warnings)
			require.Len(t, copies, 6)

			t.Run("THEN every key gets its own external id, with the old one as previous", func(t *testing.T) {
				ids := map[string]bool{}
				for _, c := range copies {
					ids[c.ExternalID] = true
				}

				assert.Len(t, ids, 6)
				assert.Equal(t, "humble:AAA:hades_steam:0", copies[0].ExternalID)
				assert.Equal(t, "humble:AAA:hades_steam:1", copies[1].ExternalID)
				assert.Equal(t, "humble:AAA:0", copies[0].PreviousExternalID)
			})

			t.Run("THEN status, platform and redeem-by come from each key", func(t *testing.T) {
				want := []struct {
					status   game.Status
					platform string
					redeemBy game.Date
				}{
					{game.StatusRevealed, "Steam", ""},
					{game.StatusUnrevealed, "Steam", ""},
					{game.StatusUnrevealed, "Steam", "2027-03-03"},
					{game.StatusExpired, "EA App", ""},
					{game.StatusGifted, "Steam", ""},
					{game.StatusUnrevealed, "Steam", "2026-10-17"},
				}
				for i, w := range want {
					d := copies[i].Details
					assert.Equal(t, w.status, d.Status, "copy %d", i)
					assert.Equal(t, w.platform, d.Platform, "copy %d", i)
					assert.Equal(t, w.redeemBy, d.RedeemBy, "copy %d", i)
				}

				t.Run("AND the order's date and the Steam app id are kept", func(t *testing.T) {
					assert.Equal(t, game.Links{game.LinkSteam: "1145360"}, copies[0].Links)
					assert.Equal(t, game.Date("2023-05-01"), copies[0].Details.AcquiredOn)
				})
			})
		})
	})

	t.Run("GIVEN a key without machine_name", func(t *testing.T) {
		raw := `{"gamekey":"BBB","tpkd_dict":{"all_tpks":[{"human_name":"Mystery","key_type":"steam","keyindex":0}]}}`

		t.Run("WHEN it is mapped", func(t *testing.T) {
			copies, _, _ := MapOrders([]json.RawMessage{json.RawMessage(raw)}, now)

			t.Run("THEN it keeps the old id format", func(t *testing.T) {
				require.Len(t, copies, 1)
				assert.Equal(t, "humble:BBB:0", copies[0].ExternalID)
			})
		})
	})
}

// mixed has the kinds of non-game keys found in real bundles: software, a store coupon and in-game
// items, next to console game keys.
const mixed = `{"gamekey":"CCC","product":{"human_name":"Stand with Ukraine Bundle"},
 "tpkd_dict":{"all_tpks":[
  {"machine_name":"ashampoo_photooptimizer7","human_name":"Ashampoo Photo Optimizer 7","key_type":"generic","key_type_human_name":"Ashampoo","keyindex":0},
  {"machine_name":"sfv_coupon","human_name":"45% off Street Fighter V PS Store Coupon","key_type":"generic","key_type_human_name":"PS Store","keyindex":0},
  {"machine_name":"duelyst_orbs","human_name":"Duelyst - 20 Spirit Orbs","key_type":"generic","key_type_human_name":"Duelyst","keyindex":0},
  {"machine_name":"strider_ps4","human_name":"Strider","key_type":"generic","key_type_human_name":"PS4","keyindex":0},
  {"machine_name":"tunic_steam","human_name":"Tunic","key_type":"steam","keyindex":0}]}}`

func TestMapOrders_nonGameKeys(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	t.Run("GIVEN a bundle with software, a coupon and in-game items next to games", func(t *testing.T) {
		t.Run("WHEN it is mapped", func(t *testing.T) {
			copies, skipped, warnings := MapOrders([]json.RawMessage{json.RawMessage(mixed)}, now)
			require.Empty(t, warnings)
			require.Len(t, copies, 5)

			t.Run("THEN only the console and store game keys are copies", func(t *testing.T) {
				var games []string

				for _, c := range copies {
					if !c.Withdrawn {
						games = append(games, c.Title+" ("+c.Details.Platform+")")
					}
				}

				assert.Equal(t, []string{"Strider (PS4)", "Tunic (Steam)"}, games)
			})

			t.Run("AND the others are withdrawn and listed as skipped", func(t *testing.T) {
				assert.Equal(t, []string{"Ashampoo Photo Optimizer 7", "45% off Street Fighter V PS Store Coupon", "Duelyst - 20 Spirit Orbs"}, skipped)
				assert.True(t, copies[0].Withdrawn)
				assert.Equal(t, "humble:CCC:ashampoo_photooptimizer7:0", copies[0].ExternalID)
			})
		})
	})
}

func TestFetchAgainstFakeServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, _ := r.Cookie("_simpleauth_sess"); c == nil || c.Value != "good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/api/v1/user/order":
			w.Write([]byte(`[{"gamekey":"AAA"}]`))
		case "/api/v1/orders":
			ordersRequests++

			w.Write([]byte(`{"AAA":` + order + `}`))
		}
	}))
	defer srv.Close()

	p := &Provider{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}

	copies, _, err := p.Fetch(ctx, source.Settings{settingSession: "good"})
	if err != nil || len(copies) != 6 {
		t.Fatalf("fetch: %d copies, err %v", len(copies), err)
	}

	if _, _, err := p.Fetch(ctx, source.Settings{settingSession: "bad"}); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}

	// Test only lists orders, and tolerates the cookie pasted as "_simpleauth_sess=...;" with quotes.
	ordersRequests = 0

	if err := p.Test(ctx, source.Settings{settingSession: ` _simpleauth_sess="good"; `}); err != nil || ordersRequests != 0 {
		t.Fatalf("test: err=%v ordersRequests=%d", err, ordersRequests)
	}
}

var ordersRequests int
