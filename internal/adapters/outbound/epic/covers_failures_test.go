package epic

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
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

// TestCovers_failureModes checks each way Epic's catalog can fail: a rate limit, an outage, no answer and a web page are
// passing troubles (the chain asks again later instead of remembering the cover as missing), and an answer
// that is not the JSON expected fails the lookup.
func TestCovers_failureModes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, f := range apiclienttest.Failures() {
		t.Run("GIVEN Epic's catalog answers with "+f.Name, func(t *testing.T) {
			token := func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, `{"access_token":"app","expires_at":"2099-01-01T00:00:00.000Z"}`)
			}
			srv := httptest.NewServer(apiclienttest.FailOn(http.HandlerFunc(token), "/catalog/api/shared/bulk/items", f.Serve))
			t.Cleanup(srv.Close)

			c := NewCovers()
			c.OAuth.BaseURL, c.Catalog.BaseURL = srv.URL, srv.URL

			t.Run("WHEN covers are asked", func(t *testing.T) {
				_, err := c.Covers(ctx, media.GameQuery{
					Title: "Control",
					Links: game.Links{"epic": "abc"},
				}, nil)

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
