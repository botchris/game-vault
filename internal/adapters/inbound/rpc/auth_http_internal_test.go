package rpc

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gamevault/internal/adapters/outbound/passwordhash"
	"gamevault/internal/adapters/outbound/sqlite"
	appauth "gamevault/internal/application/auth"
	"gamevault/internal/domain/auth"
	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// ignoreCerts ignores the certificate validation setting in tests.
type ignoreCerts struct{}

func (ignoreCerts) SetCertificateValidation(auth.CertificateValidation) {}

func TestAuthMiddleware_authServiceAllowlist(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN the middleware in front of handlers that never check the caller themselves", func(t *testing.T) {
		db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "gamevault.db"), "")
		require.NoError(t, err)
		t.Cleanup(func() { db.Close() })

		svc := appauth.NewService(sqlite.NewAuthRepository(db), sqlite.NewSettingsRepository(db), passwordhash.Bcrypt{Cost: 4},
			ignoreCerts{}, time.Now, slog.New(slog.NewTextHandler(io.Discard, nil)))

		var reached []string

		// The stand-in for an AuthService handler that forgot requirePrincipal: it answers anyone.
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = append(reached, r.URL.Path)
		})
		handler := authMiddleware(svc, next)

		// Every procedure the generated service descriptor declares, so a new RPC is covered the
		// day it is added to the proto.
		methods := pb.File_gamevault_v1_auth_proto.Services().ByName("AuthService").Methods()
		procedures := make([]string, 0, methods.Len())

		for i := range methods.Len() {
			procedures = append(procedures, "/"+gamevaultv1connect.AuthServiceName+"/"+string(methods.Get(i).Name()))
		}

		require.NotEmpty(t, procedures)

		// Requests through a proxy or with a host name are never trusted, so they carry no principal.
		untrusted := map[string]func(*http.Request){
			"proxied":   func(r *http.Request) { r.Header.Set("X-Forwarded-For", "203.0.113.9") },
			"host name": func(r *http.Request) { r.Host = "evil.example:8080" },
		}

		for name, setUp := range untrusted {
			t.Run("WHEN every AuthService procedure is called without a principal ("+name+")", func(t *testing.T) {
				reached = nil
				codes := map[string]int{}

				for _, proc := range procedures {
					req := httptest.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8080"+proc, strings.NewReader("{}"))
					req.RemoteAddr = "127.0.0.1:50000"
					req.Header.Set("Content-Type", "application/json")
					setUp(req)

					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					codes[proc] = rec.Code
				}

				t.Run("THEN only the public procedures reach their handler; the rest answer 401", func(t *testing.T) {
					for _, proc := range procedures {
						if slices.Contains(publicProcedures, proc) {
							assert.Contains(t, reached, proc, "public procedure %s", proc)
							continue
						}

						assert.Equal(t, http.StatusUnauthorized, codes[proc], "procedure %s", proc)
						assert.NotContains(t, reached, proc, "procedure %s", proc)
					}
				})
			})
		}
	})
}
