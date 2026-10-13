package upcitemdb

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient/apiclienttest"
	"gamevault/internal/domain/schema"
)

// TestLookup_failureModes checks each way UPCitemdb can fail: a rate limit, an outage, no answer and a web page are
// passing troubles (the chain asks again later instead of remembering the code as unknown), and an answer
// that is not the JSON expected fails the lookup.
func TestLookup_failureModes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, f := range apiclienttest.Failures() {
		t.Run("GIVEN UPCitemdb answers with "+f.Name, func(t *testing.T) {
			srv := httptest.NewServer(f.Serve)
			t.Cleanup(srv.Close)

			p := New()
			p.API.BaseURL = srv.URL

			t.Run("WHEN a barcode is looked up", func(t *testing.T) {
				_, err := p.Lookup(ctx, "5030934110075", schema.Settings{})

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
