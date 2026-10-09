package rpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/inbound/rpc"
)

func TestCORS_photoUploads(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a server that allows a UI hosted on another origin", func(t *testing.T) {
		srv := httptest.NewServer(rpc.NewHTTPHandler(rpc.Handlers{}, rpc.Options{CORSOrigins: []string{"https://ui.example.test"}}))
		t.Cleanup(srv.Close)

		preflight := func(origin string) *http.Response {
			req, err := http.NewRequestWithContext(ctx, http.MethodOptions, srv.URL+"/media/photos", nil)
			require.NoError(t, err)
			req.Header.Set("Origin", origin)
			req.Header.Set("Access-Control-Request-Method", http.MethodPost)
			req.Header.Set("Access-Control-Request-Headers", "x-gamevault-upload")

			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			res.Body.Close()

			return res
		}

		t.Run("WHEN that UI asks to upload a photo", func(t *testing.T) {
			res := preflight("https://ui.example.test")

			t.Run("THEN the upload header is allowed", func(t *testing.T) {
				assert.Contains(t, strings.ToLower(res.Header.Get("Access-Control-Allow-Headers")), "x-gamevault-upload")
			})
		})

		t.Run("WHEN any other site asks", func(t *testing.T) {
			res := preflight("https://evil.example.test")

			t.Run("THEN nothing is allowed", func(t *testing.T) {
				assert.Empty(t, res.Header.Get("Access-Control-Allow-Origin"))
				assert.Empty(t, res.Header.Get("Access-Control-Allow-Headers"))
			})
		})
	})
}
