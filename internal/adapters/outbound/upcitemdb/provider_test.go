package upcitemdb

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	p := &Provider{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}

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

func TestProviderTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a service with a trial endpoint and a keyed endpoint", func(t *testing.T) {
		var gotPath, gotKey string

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath, gotKey = r.URL.Path, r.Header.Get("user_key")

			switch {
			case r.URL.Query().Get("upc") != "5030934110075":
				w.WriteHeader(http.StatusBadRequest)
			case gotKey == "rejected":
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"code":"INVALID_KEY","message":"Invalid user key"}`))
			case gotKey == "" && r.URL.Path != "/prod/trial/lookup":
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"code":"TOO_FAST","message":"Exceed rate limit"}`))
			default:
				w.Write([]byte(`{"code":"OK","total":0,"items":[]}`))
			}
		}))
		defer srv.Close()

		p := &Provider{
			BaseURL: srv.URL,
			Client:  srv.Client(),
		}

		t.Run("WHEN it is tested without a key and the EAN is unknown", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{})

			t.Run("THEN it passes through the trial endpoint", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "/prod/trial/lookup", gotPath)
			})
		})

		t.Run("WHEN it is tested with a valid key", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingUserKey: "good"})

			t.Run("THEN it passes through the keyed endpoint", func(t *testing.T) {
				require.NoError(t, err)
				assert.Equal(t, "/prod/v1/lookup", gotPath)
			})
		})

		t.Run("WHEN it is tested with a rejected key", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingUserKey: "rejected"})

			t.Run("THEN it fails with the service's message", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "Invalid user key")
			})
		})
	})

	t.Run("GIVEN a service that rate-limits the trial endpoint", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"code":"EXCEED_LIMIT","message":"Exceed limit"}`))
		}))
		defer srv.Close()

		p := &Provider{
			BaseURL: srv.URL,
			Client:  srv.Client(),
		}

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{})

			t.Run("THEN it fails and suggests a user key", func(t *testing.T) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "user key")
			})
		})
	})
}
