package upcitemdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

func TestLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("upc") {
		case "3307215643006": // real answer for Assassin's Creed III, PS3 PAL
			w.Write([]byte(`{"code":"OK","total":1,"items":[{"title":"Assassin's Creed Iii Ed. Special Ps3(sp)","brand":"Sony","images":[]}]}`))
		case "2001234567893":
			w.Write([]byte(`{"code":"OK","total":0,"items":[]}`))
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"code":"TOO_FAST","message":"Exceed rate limit"}`))
		}
	}))
	defer srv.Close()

	p := testProvider(srv)

	m, err := p.Lookup(context.Background(), "3307215643006", schema.Settings{})
	if err != nil || len(m) != 1 || m[0].Title != "Assassin's Creed III" || m[0].Platform != "PS3" || m[0].Edition != "Special Edition" {
		t.Fatalf("lookup: %+v %v", m, err)
	}

	if m, err := p.Lookup(context.Background(), "2001234567893", schema.Settings{}); err != nil || len(m) != 0 {
		t.Fatalf("unknown code must be no match, not an error: %+v %v", m, err)
	}

	_, err = p.Lookup(context.Background(), "0000000000000", schema.Settings{})
	require.ErrorIs(t, err, provider.ErrRateLimited, "a burst limit (TOO_FAST) is a rate limit")
	assert.NotErrorIs(t, err, provider.ErrQuotaExhausted, "a burst limit is not the daily allowance")
}

// TestLookup_failures checks that every failure says what kind it is, so the provider's status
// shows it and the barcode chain moves on: the daily allowance, an outage, no answer.
func TestLookup_failures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	cases := map[string]struct {
		status int
		body   string
		want   error
	}{
		// UPCitemdb's documented error codes: TOO_FAST per minute, EXCEED_LIMIT per day.
		"the daily allowance is used up": {http.StatusTooManyRequests, `{"code":"EXCEED_LIMIT","message":"Exceed limit"}`, provider.ErrQuotaExhausted},
		"the service is down":            {http.StatusBadGateway, ``, provider.ErrUnavailable},
		"nothing answers":                {0, ``, provider.ErrUnavailable},
		"a web page instead of JSON":     {http.StatusOK, `<html><body>Checking your browser…</body></html>`, provider.ErrUnavailable},
	}
	for name, c := range cases {
		t.Run("GIVEN "+name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.status)
				w.Write([]byte(c.body))
			}))
			defer srv.Close()

			p := testProvider(srv)

			if c.status == 0 {
				srv.Close()
			}

			t.Run("WHEN a code is looked up", func(t *testing.T) {
				_, err := p.Lookup(ctx, "5030934110075", schema.Settings{})

				t.Run("THEN the error says what kind of failure it is", func(t *testing.T) {
					require.ErrorIs(t, err, c.want)
				})
			})
		})
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

		p := testProvider(srv)

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

			t.Run("THEN it fails with a settings problem and the service's message", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrBadSettings)
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

		p := testProvider(srv)

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{})

			t.Run("THEN it fails with a used-up allowance and suggests a user key", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrQuotaExhausted)
				assert.Contains(t, err.Error(), "user key")
			})
		})
	})
}

// testProvider is the provider pointed at the fake server.
func testProvider(srv *httptest.Server) *Provider {
	p := New()
	p.API.BaseURL, p.API.HTTP = srv.URL, srv.Client()

	return p
}
