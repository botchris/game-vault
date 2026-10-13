package ebay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient/apiclienttest"
	"gamevault/internal/domain/schema"
)

// TestLookup_failureModes checks each way eBay's search (with keys it accepts) can fail: a rate limit, an outage, no answer and a web page are
// passing troubles (the chain asks again later instead of remembering the code as unknown), and an answer
// that is not the JSON expected fails the lookup.
func TestLookup_failureModes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, f := range apiclienttest.Failures() {
		t.Run("GIVEN eBay's search (with keys it accepts) answers with "+f.Name, func(t *testing.T) {
			token := func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"access_token":"tok","expires_in":7200}`)
			}
			srv := httptest.NewServer(apiclienttest.FailOn(http.HandlerFunc(token), "/buy/browse/v1/item_summary/search", f.Serve))
			t.Cleanup(srv.Close)

			p := testProvider(srv)

			t.Run("WHEN a barcode is looked up", func(t *testing.T) {
				_, err := p.Lookup(ctx, "5030934110075", schema.Settings{settingClientID: "id", settingClientSecret: "secret", settingMarketplaces: "EBAY_GB"})

				t.Run("THEN the lookup fails, as a passing trouble when it is one", func(t *testing.T) {
					require.Error(t, err)

					if f.Want != nil {
						assert.ErrorIs(t, err, f.Want)
					}
				})
			})
		})
	}
}
