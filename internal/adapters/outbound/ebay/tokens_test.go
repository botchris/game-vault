package ebay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/domain/provider"
)

// testProvider is the barcode provider pointed at the fake server.
func testProvider(srv *httptest.Server) *Provider {
	p := New()
	p.API.BaseURL, p.API.HTTP = srv.URL, srv.Client()

	return p
}

// fakeTokens is eBay's token endpoint and Browse API, issuing tok1, tok2… and refusing the tokens
// in revoked with HTTP 401 (the shape of eBay's answer, values invented).
type fakeTokens struct {
	mu        sync.Mutex
	issued    int
	expiresIn string
	revoked   map[string]bool
	refuseAll bool
}

func (f *fakeTokens) serve(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.URL.Path {
		case "/identity/v1/oauth2/token":
			f.issued++
			fmt.Fprintf(w, `{"access_token":"tok%d","expires_in":%s,"token_type":"Application Access Token"}`, f.issued, f.expiresIn)
		case "/buy/browse/v1/item_summary/search":
			tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if f.refuseAll || f.revoked[tok] {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"errors":[{"errorId":1001,"domain":"OAuth","category":"REQUEST","message":"Invalid access token"}]}`))

				return
			}

			w.Write([]byte(usedListings))
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func (f *fakeTokens) tokens() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.issued
}

func TestTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the eBay plugin, its barcode and price providers on one fake eBay", func(t *testing.T) {
		f := &fakeTokens{expiresIn: "7200"}
		srv := f.serve(t)
		pl := Plugin()
		barcodes := pl.Barcodes[0].(*Provider)
		barcodes.API.BaseURL, barcodes.API.HTTP = srv.URL, srv.Client()

		t.Run("WHEN a code is looked up and then priced", func(t *testing.T) {
			_, err := barcodes.Lookup(ctx, "5030934110075", keys)
			require.NoError(t, err)

			_, err = pl.Valuations[0].Estimate(ctx, keys, "5030934110075")
			require.NoError(t, err)

			t.Run("THEN both use one token: the providers share one client", func(t *testing.T) {
				assert.Equal(t, 1, f.tokens())
			})
		})
	})

	t.Run("GIVEN a token endpoint that announces no lifetime", func(t *testing.T) {
		f := &fakeTokens{expiresIn: "0"}
		p := testProvider(f.serve(t))

		t.Run("WHEN two codes are looked up", func(t *testing.T) {
			_, err := p.Lookup(ctx, "5030934110075", keys)
			require.NoError(t, err)
			_, err = p.Lookup(ctx, "5030934110082", keys)
			require.NoError(t, err)

			t.Run("THEN the token is kept, not asked for again on every lookup", func(t *testing.T) {
				assert.Equal(t, 1, f.tokens())
			})
		})
	})

	t.Run("GIVEN a token eBay revokes before its time", func(t *testing.T) {
		f := &fakeTokens{
			expiresIn: "7200",
			revoked:   map[string]bool{"tok1": true},
		}
		p := testProvider(f.serve(t))

		t.Run("WHEN a code is looked up", func(t *testing.T) {
			got, err := p.Lookup(ctx, "5030934110075", keys)

			t.Run("THEN a new token is asked for once and the search sent again", func(t *testing.T) {
				require.NoError(t, err)
				require.Len(t, got, 1)
				assert.Equal(t, 2, f.tokens())
			})
		})
	})

	t.Run("GIVEN keys whose tokens eBay always refuses", func(t *testing.T) {
		f := &fakeTokens{
			expiresIn: "7200",
			refuseAll: true,
		}
		p := testProvider(f.serve(t))

		t.Run("WHEN a code is looked up", func(t *testing.T) {
			_, err := p.Lookup(ctx, "5030934110075", keys)

			t.Run("THEN it is a settings problem, after one renewal", func(t *testing.T) {
				require.ErrorIs(t, err, provider.ErrBadSettings)
				assert.Equal(t, 2, f.tokens())
			})
		})
	})
}
