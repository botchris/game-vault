package upcitemdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"gamevault/internal/domain/schema"
)

func TestLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("upc") {
		case "3307215643006": // real answer for Assassin's Creed III, PS3 PAL
			w.Write([]byte(`{"code":"OK","total":1,"items":[{"title":"Assassin's Creed Iii Ed. Special Ps3(sp)","brand":"Sony","images":[]}]}`))
		case "5026555255042":
			w.Write([]byte(`{"code":"OK","total":0,"items":[]}`))
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"code":"TOO_FAST","message":"Exceed rate limit"}`))
		}
	}))
	defer srv.Close()
	p := &Provider{BaseURL: srv.URL, Client: srv.Client()}

	m, err := p.Lookup(context.Background(), "3307215643006", schema.Settings{})
	if err != nil || len(m) != 1 || m[0].Title != "Assassin's Creed III" || m[0].Platform != "PS3" || m[0].Edition != "Special Edition" {
		t.Fatalf("lookup: %+v %v", m, err)
	}
	if m, err := p.Lookup(context.Background(), "5026555255042", schema.Settings{}); err != nil || len(m) != 0 {
		t.Fatalf("unknown code must be no match, not an error: %+v %v", m, err)
	}
	if _, err := p.Lookup(context.Background(), "0000000000000", schema.Settings{}); err == nil {
		t.Fatal("rate limit must be an error")
	}
}
