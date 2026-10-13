package eansearch

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
	"gamevault/internal/domain/schema"
)

// fakeEANSearch answers like EAN-Search: a known code, an unknown one, a token it rejects and a
// token whose plan has no lookups left. It asks to slow down once, on the first lookup of the
// known code, as its short burst limit does.
func fakeEANSearch(t *testing.T) *httptest.Server {
	t.Helper()

	calls429 := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		w.Header().Set("Content-Type", "application/json")

		switch q.Get("token") {
		case "good":
		case "empty": // a token whose plan has no lookups left
			w.Header().Set("X-Credits-Remaining", "0")
		default:
			// The real answer to an unknown token (probed with a bogus one on 2026-10-13).
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`[{ "error" : "Invalid token" }]`))

			return
		}

		if w.Header().Get("X-Credits-Remaining") == "" {
			w.Header().Set("X-Credits-Remaining", "99")
		}

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
	t.Cleanup(srv.Close)

	return srv
}

func TestLookupAndTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN EAN-Search and a token with lookups left", func(t *testing.T) {
		p := testProvider(fakeEANSearch(t))
		good := schema.Settings{settingToken: "good"}

		t.Run("WHEN a known code is looked up and EAN-Search asks to slow down once", func(t *testing.T) {
			m, err := p.Lookup(ctx, "5030934110075", good)

			t.Run("THEN the retry names the game and its platform", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, m, 1)
				assert.Equal(t, "Dead Space 3", m[0].Title)
				assert.Equal(t, "Xbox 360", m[0].Platform)
			})
		})

		t.Run("WHEN an unknown code is looked up", func(t *testing.T) {
			m, err := p.Lookup(ctx, "2001234567893", good)

			t.Run("THEN it is no match, not an error", func(t *testing.T) {
				require.NoError(t, err)
				assert.Empty(t, m)
			})
		})

		t.Run("WHEN the token is tested", func(t *testing.T) {
			err := p.Test(ctx, good)

			t.Run("THEN it works", func(t *testing.T) {
				require.NoError(t, err)
			})
		})
	})

	t.Run("GIVEN a token EAN-Search rejects", func(t *testing.T) {
		p := testProvider(fakeEANSearch(t))
		bad := schema.Settings{settingToken: "bad"}

		t.Run("WHEN it is tested and used", func(t *testing.T) {
			testErr := p.Test(ctx, bad)
			_, lookupErr := p.Lookup(ctx, "5030934110075", bad)

			t.Run("THEN both are a settings problem, with the API's words", func(t *testing.T) {
				require.ErrorIs(t, testErr, provider.ErrBadSettings)
				assert.Contains(t, testErr.Error(), "Invalid token")
				require.ErrorIs(t, lookupErr, provider.ErrBadSettings)
			})
		})
	})

	t.Run("GIVEN a token whose plan has no lookups left", func(t *testing.T) {
		p := testProvider(fakeEANSearch(t))

		t.Run("WHEN it is tested", func(t *testing.T) {
			err := p.Test(ctx, schema.Settings{settingToken: "empty"})

			t.Run("THEN it is a used-up allowance", func(t *testing.T) {
				require.ErrorIs(t, err, ErrNoCredits)
				require.ErrorIs(t, err, provider.ErrQuotaExhausted)
			})
		})
	})
}

// TestLookup_transportErrorsHideTheToken checks that no answer at all (the connection dropped) is
// an outage whose text never shows the token, which travels in the query: the error ends up in
// the barcode warnings and in the logs.
func TestLookup_transportErrorsHideTheToken(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.(*net.TCPConn).SetLinger(0)
			conn.Close()
		}
	}))
	defer srv.Close()

	p := testProvider(srv)
	secret := schema.Settings{settingToken: "tok-s3cret-123"}

	t.Run("GIVEN EAN-Search closes the connection without answering", func(t *testing.T) {
		t.Run("WHEN a code is looked up and the token tested", func(t *testing.T) {
			_, lookupErr := p.Lookup(ctx, "5030934110075", secret)
			testErr := p.Test(ctx, secret)

			t.Run("THEN both fail as an outage whose text never shows the token", func(t *testing.T) {
				require.ErrorIs(t, lookupErr, provider.ErrUnavailable)
				assert.NotContains(t, lookupErr.Error(), "tok-s3cret-123")
				require.Error(t, testErr)
				assert.NotContains(t, testErr.Error(), "tok-s3cret-123")
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
