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

func TestLookupAndTest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

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
		case q.Get("ean") == "4006381333931": // asks to slow down every time
			w.WriteHeader(http.StatusTooManyRequests)
		case q.Get("ean") == "4006381333948":
			w.WriteHeader(http.StatusBadGateway)
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

	m, err := p.Lookup(ctx, "5030934110075", good)
	require.NoError(t, err)
	require.Len(t, m, 1)
	assert.Equal(t, "Dead Space 3", m[0].Title)
	assert.Equal(t, "Xbox 360", m[0].Platform)

	m, err = p.Lookup(ctx, "2001234567893", good)
	require.NoError(t, err, "an unknown code is no match, not an error")
	assert.Empty(t, m)

	require.NoError(t, p.Test(ctx, good))

	t.Run("a rejected token is a settings problem with the API's words", func(t *testing.T) {
		err := p.Test(ctx, schema.Settings{settingToken: "bad"})
		require.ErrorIs(t, err, provider.ErrBadSettings)
		assert.Contains(t, err.Error(), "Invalid token")

		_, err = p.Lookup(ctx, "5030934110075", schema.Settings{settingToken: "bad"})
		require.ErrorIs(t, err, provider.ErrBadSettings)
	})

	t.Run("a used-up plan is a used-up allowance", func(t *testing.T) {
		err := p.Test(ctx, schema.Settings{settingToken: "empty"})
		require.ErrorIs(t, err, ErrNoCredits)
		require.ErrorIs(t, err, provider.ErrQuotaExhausted)
	})

	t.Run("a 429 after the retry is a rate limit", func(t *testing.T) {
		_, err := p.Lookup(ctx, "4006381333931", good)
		require.ErrorIs(t, err, provider.ErrRateLimited)
		assert.NotErrorIs(t, err, provider.ErrQuotaExhausted)
	})

	t.Run("a 5xx is an outage", func(t *testing.T) {
		_, err := p.Lookup(ctx, "4006381333948", good)
		require.ErrorIs(t, err, provider.ErrUnavailable)
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

	p := &Provider{
		BaseURL: srv.URL,
		Client:  srv.Client(),
	}

	_, err := p.Lookup(ctx, "5030934110075", schema.Settings{settingToken: "tok-s3cret-123"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "tok-s3cret-123")
	require.ErrorIs(t, err, provider.ErrUnavailable)

	err = p.Test(ctx, schema.Settings{settingToken: "tok-s3cret-123"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "tok-s3cret-123")
}
