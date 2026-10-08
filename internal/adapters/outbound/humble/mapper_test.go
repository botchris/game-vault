package humble

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gamevault/internal/domain/game"
	"gamevault/internal/domain/source"
)

const order = `{"gamekey":"AAA","created":"2023-05-01T10:00:00.000000","product":{"human_name":"Indie Bundle"},
 "tpkd_dict":{"all_tpks":[
  {"human_name":"Hades","key_type":"steam","redeemed_key_val":"ABCDE-FGHIJ","steam_app_id":1145360,"keyindex":0},
  {"human_name":"Celeste","key_type":"steam","keyindex":1,"custom_instructions_html":"Please redeem by March 3, 2027."},
  {"human_name":"Old","key_type":"origin","keyindex":2,"is_expired":true},
  {"human_name":"Gift","key_type":"steam","keyindex":3,"redeemed_key_val":"https://www.humblebundle.com/gift?key=x"},
  {"human_name":"Soon","key_type":"steam","keyindex":4,"num_days_until_expired":10}]}}`

func TestMapOrders(t *testing.T) {
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	copies, warnings := MapOrders([]json.RawMessage{json.RawMessage(order)}, now)
	if len(warnings) > 0 || len(copies) != 5 {
		t.Fatalf("got %d copies, warnings %v", len(copies), warnings)
	}

	want := []struct {
		status   game.Status
		platform string
		redeemBy game.Date
	}{
		{game.StatusRevealed, "Steam", ""},
		{game.StatusUnrevealed, "Steam", "2027-03-03"},
		{game.StatusExpired, "EA App", ""},
		{game.StatusGifted, "Steam", ""},
		{game.StatusUnrevealed, "Steam", "2026-10-17"},
	}
	for i, w := range want {
		d := copies[i].Details
		if d.Status != w.status || d.Platform != w.platform || d.RedeemBy != w.redeemBy {
			t.Errorf("copy %d = %+v, want %+v", i, d, w)
		}
	}

	if c := copies[0]; c.ExternalID != "humble:AAA:0" || c.SteamAppID != 1145360 || c.Details.AcquiredOn != "2023-05-01" {
		t.Errorf("unexpected first copy %+v", c)
	}
}

func TestFetchAgainstFakeServer(t *testing.T) {
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

	p := &Provider{BaseURL: srv.URL, Client: srv.Client()}

	copies, _, err := p.Fetch(context.Background(), source.Settings{settingSession: "good"})
	if err != nil || len(copies) != 5 {
		t.Fatalf("fetch: %d copies, err %v", len(copies), err)
	}

	if _, _, err := p.Fetch(context.Background(), source.Settings{settingSession: "bad"}); err != ErrUnauthorized {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}

	// Test only lists orders, and tolerates the cookie pasted as "_simpleauth_sess=...;" with quotes.
	ordersRequests = 0

	res, err := p.Test(context.Background(), source.Settings{settingSession: ` _simpleauth_sess="good"; `})
	if err != nil || res.Count != 1 || res.Unit != "orders" || ordersRequests != 0 {
		t.Fatalf("test: %+v err=%v ordersRequests=%d", res, err, ordersRequests)
	}
}

var ordersRequests int
