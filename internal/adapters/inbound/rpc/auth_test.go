package rpc_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// hostTransport sends requests with another Host header and extra headers: a site's own name
// pointing at this machine (DNS rebinding), or a request through a proxy.
type hostTransport struct {
	host    string
	headers map[string]string
}

func (t hostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if t.host != "" {
		r.Host = t.host
	}

	for k, v := range t.headers {
		r.Header.Set(k, v)
	}

	return http.DefaultTransport.RoundTrip(r)
}

func clientWith(host string, headers map[string]string) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Transport: hostTransport{host: host, headers: headers}, Jar: jar}
}

func TestAccessControl(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	listGames := func(hc *http.Client) error {
		_, err := gamevaultv1connect.NewGameServiceClient(hc, c.baseURL).ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))

		return err
	}

	// local is this computer; rebinding reaches it through a site's own name (DNS rebinding),
	// proxied through a proxy. Neither of the last two may ever be trusted.
	local := gamevaultv1connect.NewAuthServiceClient(http.DefaultClient, c.baseURL)
	rebinding := clientWith("evil.example:8080", nil)
	proxied := clientWith("", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	rebindingAuth := gamevaultv1connect.NewAuthServiceClient(rebinding, c.baseURL)

	t.Run("GIVEN a new server with the default settings", func(t *testing.T) {
		t.Run("WHEN this computer asks for its status", func(t *testing.T) {
			st, err := local.GetAuthStatus(ctx, connect.NewRequest(&pb.GetAuthStatusRequest{}))
			require.NoError(t, err)

			t.Run("THEN it is trusted without signing in and may create the user", func(t *testing.T) {
				assert.Equal(t, "trusted", st.Msg.Principal.GetMethod())
				assert.True(t, st.Msg.CanSetup)
				assert.True(t, st.Msg.Trusted)
			})
		})

		t.Run("WHEN a request uses a host name or comes through a proxy", func(t *testing.T) {
			t.Run("THEN the API refuses it", func(t *testing.T) {
				for name, hc := range map[string]*http.Client{"rebinding": rebinding, "proxied": proxied} {
					assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(listGames(hc)), name)
				}

				t.Run("AND media is refused too", func(t *testing.T) {
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/media/covers/x", nil)
					require.NoError(t, err)

					res, err := rebinding.Do(req)
					require.NoError(t, err)

					defer res.Body.Close()

					assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
				})

				t.Run("AND it cannot create the user", func(t *testing.T) {
					_, err := rebindingAuth.Setup(ctx, connect.NewRequest(&pb.SetupRequest{Username: "admin", Password: "correct horse"}))
					assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
				})
			})
		})

		settings, err := local.GetAuthSettings(ctx, connect.NewRequest(&pb.GetAuthSettingsRequest{}))
		require.NoError(t, err)
		require.False(t, settings.Msg.HasUser)
		require.Equal(t, "127.0.0.1", settings.Msg.ClientAddress)

		s := settings.Msg.Settings

		t.Run("WHEN authentication is required before a user exists", func(t *testing.T) {
			s.Authentication = "required"
			_, err := local.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: s}))

			t.Run("THEN it is refused, because it would lock the owner out", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
			})
		})

		t.Run("WHEN this computer creates the user", func(t *testing.T) {
			_, err := local.Setup(ctx, connect.NewRequest(&pb.SetupRequest{Username: "admin", Password: "correct horse"}))
			require.NoError(t, err)

			t.Run("THEN a wrong password is refused from anywhere", func(t *testing.T) {
				_, err := rebindingAuth.Login(ctx, connect.NewRequest(&pb.LoginRequest{Username: "admin", Password: "wrong password"}))
				assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
			})

			t.Run("THEN the right password signs in from anywhere", func(t *testing.T) {
				login, err := rebindingAuth.Login(ctx, connect.NewRequest(&pb.LoginRequest{Username: "admin", Password: "correct horse"}))
				require.NoError(t, err)
				assert.Equal(t, "session", login.Msg.Principal.Method)
				assert.Contains(t, login.Header().Get("Set-Cookie"), "HttpOnly")

				t.Run("AND the session cookie authenticates", func(t *testing.T) {
					assert.NoError(t, listGames(rebinding))
				})
			})

			t.Run("WHEN authentication becomes required", func(t *testing.T) {
				s.Authentication = "required"
				_, err := local.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: s}))
				require.NoError(t, err)

				t.Run("THEN even this computer must sign in", func(t *testing.T) {
					assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(listGames(http.DefaultClient)))
				})
			})

			// The signed-in session changes the settings from now on.
			signedIn := gamevaultv1connect.NewAuthServiceClient(rebinding, c.baseURL)

			t.Run("WHEN only another network is trusted", func(t *testing.T) {
				s.Authentication, s.TrustedNetworks = "trusted_networks", []string{"10.0.0.0/8"}
				updated, err := signedIn.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: s}))
				require.NoError(t, err)
				require.Equal(t, []string{"10.0.0.0/8"}, updated.Msg.Settings.TrustedNetworks)

				t.Run("THEN this computer must sign in", func(t *testing.T) {
					assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(listGames(http.DefaultClient)))
				})

				t.Run("AND invalid networks are refused", func(t *testing.T) {
					_, err := signedIn.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: &pb.AuthSettings{
						Authentication: "trusted_networks", TrustedNetworks: []string{"not a network"},
					}}))
					assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				})
			})

			t.Run("WHEN a trusted computer resets the forgotten password", func(t *testing.T) {
				s.TrustedNetworks = []string{"127.0.0.0/8"}
				_, err := signedIn.UpdateAuthSettings(ctx, connect.NewRequest(&pb.UpdateAuthSettingsRequest{Settings: s}))
				require.NoError(t, err)

				_, err = local.ChangePassword(ctx, connect.NewRequest(&pb.ChangePasswordRequest{NewPassword: "another horse"}))
				require.NoError(t, err)

				t.Run("THEN every old session is closed", func(t *testing.T) {
					assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(listGames(rebinding)))
				})

				t.Run("AND the new password works", func(t *testing.T) {
					_, err := rebindingAuth.Login(ctx, connect.NewRequest(&pb.LoginRequest{Username: "admin", Password: "another horse"}))
					assert.NoError(t, err)
				})
			})
		})
	})
}
