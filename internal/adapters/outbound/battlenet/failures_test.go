package battlenet

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

// TestFetch_failures checks each way Battle.net's games overview can fail: a rate limit, an outage, no answer and an
// answer that is not the JSON expected all fail the scan with what happened, and none of them is
// taken for a refused session, which would ask the user to sign in again for nothing.
func TestFetch_failures(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	for _, f := range apiclienttest.Failures() {
		t.Run("GIVEN Battle.net's games overview answers with "+f.Name, func(t *testing.T) {
			srv := httptest.NewServer(apiclienttest.FailOn(fakeAccount(t).Config.Handler, "/api/games-and-subs", f.Serve))
			t.Cleanup(srv.Close)

			p := NewProvider()
			p.Account.BaseURL = srv.URL

			t.Run("WHEN the library is scanned", func(t *testing.T) {
				_, _, err := p.Fetch(ctx, schema.Settings{settingCookies: "SESSION=fresh"})

				if f.SignInPage {
					t.Run("THEN it is the sign-in page Battle.net serves to a refused cookie", func(t *testing.T) {
						require.ErrorIs(t, err, ErrSignedOut)
					})

					return
				}

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
