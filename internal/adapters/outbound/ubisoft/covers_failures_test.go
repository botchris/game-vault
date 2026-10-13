package ubisoft

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/apiclient/apiclienttest"
	"gamevault/internal/application/media"
	"gamevault/internal/domain/game"
)

// TestCovers_failureModes checks each way the Ubisoft Store search (with no box art on the CDN) can fail: a rate limit, an outage, no answer and a web page are
// passing troubles (the chain asks again later instead of remembering the cover as missing), and an answer
// that is not the JSON expected fails the lookup.
func TestCovers_failureModes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, f := range apiclienttest.Failures() {
		t.Run("GIVEN the Ubisoft Store search (with no box art on the CDN) answers with "+f.Name, func(t *testing.T) {
			srv := httptest.NewServer(f.Serve)
			t.Cleanup(srv.Close)

			c := NewCovers()
			c.CDN.BaseURL, c.Search.BaseURL = srv.URL, srv.URL

			t.Run("WHEN covers are asked", func(t *testing.T) {
				_, err := c.Covers(ctx, media.GameQuery{
					Title: "Far Cry 5",
					Links: game.Links{"ubisoft": "space-1"},
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
