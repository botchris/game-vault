package amazon

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient/apiclienttest"
	"gamevault/internal/domain/schema"
	"gamevault/internal/domain/source"
)

// TestFetch_failures checks each way Amazon's entitlements service can fail: a rate limit, an outage, no answer and an
// answer that is not the JSON expected all fail the scan with what happened, and none of them is
// taken for a refused session, which would ask the user to sign in again for nothing.
func TestFetch_failures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, f := range apiclienttest.Failures() {
		t.Run("GIVEN Amazon's entitlements service answers with "+f.Name, func(t *testing.T) {
			srv := httptest.NewServer(apiclienttest.FailOn(&fakeAmazon{}, "/api/distribution/entitlements", f.Serve))
			t.Cleanup(srv.Close)

			p := NewProvider()
			p.API.BaseURL, p.Gaming.BaseURL = srv.URL, srv.URL

			t.Run("WHEN the library is scanned", func(t *testing.T) {
				_, _, err := p.Fetch(ctx, schema.Settings{settingSession: `{"refresh_token":"Atnr|good","serial":"SERIAL"}`})

				t.Run("THEN the scan fails without signing the user out", func(t *testing.T) {
					require.Error(t, err)
					assert.NotErrorIs(t, err, source.ErrSignedOut)

					if f.Want != nil {
						assert.ErrorIs(t, err, f.Want)
					}
				})
			})
		})
	}
}
