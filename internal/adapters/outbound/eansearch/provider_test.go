package eansearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gamevault/internal/domain/schema"
)

func TestLookupAndTest(t *testing.T) {
	calls429 := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("token") != "good" {
			w.Write([]byte(`[{"error":"Invalid token"}]`))
			return
		}

		w.Header().Set("X-Credits-Remaining", "99")

		switch {
		case q.Get("op") == "verify-checksum":
			w.Write([]byte(`[{"ean":"5030934110075","valid":"1"}]`))
		case q.Get("ean") == "5030934110075":
			if calls429 == 0 { // API asks to slow down once
				calls429++

				w.WriteHeader(http.StatusTooManyRequests)

				return
			}
			// Real answer for Dead Space 3 (Xbox 360, PAL).
			w.Write([]byte(`[{"ean":"5030934110075","name":"DEAD SPACE 3 X360","categoryId":"0","categoryName":"Unknown","issuingCountry":"UK"}]`))
		default:
			w.Write([]byte(`[{"ean":"` + q.Get("ean") + `","error":"Barcode not found"}]`))
		}
	}))
	defer srv.Close()

	p := &Provider{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}
	good := schema.Settings{settingToken: "good"}

	m, err := p.Lookup(context.Background(), "5030934110075", good)
	if err != nil || len(m) != 1 || m[0].Title != "Dead Space 3" || m[0].Platform != "Xbox 360" {
		t.Fatalf("lookup: %+v %v", m, err)
	}

	if m, err := p.Lookup(context.Background(), "5026555255042", good); err != nil || len(m) != 0 {
		t.Fatalf("unknown code must be no match: %+v %v", m, err)
	}

	if err := p.Test(context.Background(), good); err != nil {
		t.Fatalf("test: %v", err)
	}

	if err := p.Test(context.Background(), schema.Settings{settingToken: "bad"}); err == nil || err.Error() != "Invalid token" {
		t.Fatalf("bad token must fail with the API message, got %v", err)
	}
}
