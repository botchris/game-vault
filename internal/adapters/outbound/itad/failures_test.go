package itad

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient/apiclienttest"
	"gamevault/internal/application/home"
	"gamevault/internal/domain/provider"
)

// TestDeals_failures checks each way ITAD can fail, on the deals list and on the Steam lookup: a
// rate limit, an outage, no answer, a web page and a truncated answer all fail the list with what
// happened, and none is taken for a rejected key, which would tell the user to fix their settings.
func TestDeals_failures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, path := range []string{"/deals/v2", "/lookup/shop/61/id/v1"} {
		for _, fail := range apiclienttest.Failures() {
			t.Run("GIVEN ITAD's "+path+" answers with "+fail.Name, func(t *testing.T) {
				f := newFakeITAD(t)
				srv := httptest.NewServer(apiclienttest.FailOn(f.srv.Config.Handler, path, fail.Serve))
				t.Cleanup(srv.Close)

				d := f.provider()
				d.API.BaseURL = srv.URL
				d.API.HTTP = srv.Client()

				t.Run("WHEN the featured deals are asked for", func(t *testing.T) {
					_, err := d.Deals(ctx, goodSettings, home.DealQuery{
						List:  home.DealsFeatured,
						Limit: 10,
					})

					t.Run("THEN the list fails, and not as a rejected key", func(t *testing.T) {
						require.Error(t, err)
						assert.NotErrorIs(t, err, provider.ErrBadSettings)

						if fail.Want != nil {
							assert.ErrorIs(t, err, fail.Want)
						}
					})
				})
			})
		}
	}
}

// TestDeals_forbiddenPage checks that only ITAD's own answer about the key is a rejected key: a
// 403 web page, as a CDN or a firewall in front of ITAD serves, is a passing trouble, and must not
// tell the user to fix a key that works.
func TestDeals_forbiddenPage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	forbiddenPage := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("<!DOCTYPE html><html><body><h1>403 Forbidden</h1></body></html>"))
	}

	for _, path := range []string{"/deals/v2", "/lookup/shop/61/id/v1"} {
		t.Run("GIVEN ITAD's "+path+" answers with a 403 web page", func(t *testing.T) {
			f := newFakeITAD(t)
			srv := httptest.NewServer(apiclienttest.FailOn(f.srv.Config.Handler, path, forbiddenPage))
			t.Cleanup(srv.Close)

			d := f.provider()
			d.API.BaseURL = srv.URL
			d.API.HTTP = srv.Client()

			t.Run("WHEN the featured deals are asked for", func(t *testing.T) {
				_, err := d.Deals(ctx, goodSettings, home.DealQuery{
					List:  home.DealsFeatured,
					Limit: 10,
				})

				t.Run("THEN it is a passing trouble, not a rejected key", func(t *testing.T) {
					require.Error(t, err)
					assert.ErrorIs(t, err, provider.ErrUnavailable)
					assert.NotErrorIs(t, err, ErrBadKey)
					assert.NotErrorIs(t, err, provider.ErrBadSettings)
				})
			})
		})
	}
}
