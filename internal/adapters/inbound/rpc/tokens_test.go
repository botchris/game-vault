package rpc_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

// bearer returns an HTTP client that sends the access token, with that Host (empty: the server's
// address) and extra headers.
func bearer(secret, host string, headers map[string]string) *http.Client {
	h := map[string]string{"Authorization": "Bearer " + secret}
	maps.Copy(h, headers)

	return clientWith(host, h)
}

func TestAccessTokens_endToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	local := gamevaultv1connect.NewAuthServiceClient(http.DefaultClient, c.baseURL)
	_, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: "Lantern Keep",
		Copies: []*pb.CopyDetails{{
			Kind:     pb.CopyKind_COPY_KIND_LIBRARY,
			Platform: "Steam",
		}},
	}))
	require.NoError(t, err)

	check := func(hc *http.Client) (*connect.Response[pb.CheckOwnershipResponse], error) {
		return gamevaultv1connect.NewOwnershipServiceClient(hc, c.baseURL).CheckOwnership(ctx,
			connect.NewRequest(&pb.CheckOwnershipRequest{Items: []*pb.OwnershipItem{{Title: "Lantern Keep"}}}))
	}

	t.Run("GIVEN a token created from this computer", func(t *testing.T) {
		created, err := local.CreateAccessToken(ctx, connect.NewRequest(&pb.CreateAccessTokenRequest{Name: "Extension, laptop"}))
		require.NoError(t, err)

		secret := created.Msg.Secret
		require.True(t, strings.HasPrefix(secret, "gvt_"))

		t.Run("THEN the list shows it without its secret, never used", func(t *testing.T) {
			list, err := local.ListAccessTokens(ctx, connect.NewRequest(&pb.ListAccessTokensRequest{}))
			require.NoError(t, err)
			require.Len(t, list.Msg.Tokens, 1)
			assert.Equal(t, "Extension, laptop", list.Msg.Tokens[0].Name)
			assert.Nil(t, list.Msg.Tokens[0].LastUsedAt)
		})

		t.Run("WHEN a browser elsewhere checks ownership with it, through a host name or a proxy", func(t *testing.T) {
			for name, hc := range map[string]*http.Client{
				"host name": bearer(secret, "gamevault.example:8080", nil),
				"proxied":   bearer(secret, "", map[string]string{"X-Forwarded-For": "203.0.113.9"}),
			} {
				res, err := check(hc)
				require.NoError(t, err, name)
				assert.Equal(t, "owned", res.Msg.Results[0].Status, name)
			}

			t.Run("THEN its last use shows in the list", func(t *testing.T) {
				list, err := local.ListAccessTokens(ctx, connect.NewRequest(&pb.ListAccessTokensRequest{}))
				require.NoError(t, err)
				assert.NotNil(t, list.Msg.Tokens[0].LastUsedAt)
			})
		})

		t.Run("WHEN the token calls anything else, even from this trusted computer", func(t *testing.T) {
			hc := bearer(secret, "", nil)
			_, listErr := gamevaultv1connect.NewGameServiceClient(hc, c.baseURL).ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))
			_, settingsErr := gamevaultv1connect.NewAuthServiceClient(hc, c.baseURL).GetAuthSettings(ctx, connect.NewRequest(&pb.GetAuthSettingsRequest{}))
			_, mintErr := gamevaultv1connect.NewAuthServiceClient(hc, c.baseURL).CreateAccessToken(ctx,
				connect.NewRequest(&pb.CreateAccessTokenRequest{Name: "another"}))

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/media/covers/x", nil)
			require.NoError(t, err)

			res, err := hc.Do(req)
			require.NoError(t, err)
			res.Body.Close()

			t.Run("THEN it is refused as unauthenticated", func(t *testing.T) {
				for name, err := range map[string]error{"ListGames": listErr, "GetAuthSettings": settingsErr, "CreateAccessToken": mintErr} {
					assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err), name)
				}

				assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
			})

			t.Run("AND no second token was made", func(t *testing.T) {
				list, err := local.ListAccessTokens(ctx, connect.NewRequest(&pb.ListAccessTokensRequest{}))
				require.NoError(t, err)
				assert.Len(t, list.Msg.Tokens, 1)
			})
		})

		t.Run("WHEN a proxy in front of Game Vault adds its own credentials", func(t *testing.T) {
			for _, header := range []string{"Basic YWRtaW46c2VjcmV0", "Bearer eyJhbGciOiJSUzI1NiJ9.e30.sig"} {
				hc := clientWith("", map[string]string{"Authorization": header})
				_, err := gamevaultv1connect.NewGameServiceClient(hc, c.baseURL).ListGames(ctx, connect.NewRequest(&pb.ListGamesRequest{}))

				t.Run("THEN they are ignored and this computer is still trusted: "+header[:6], func(t *testing.T) {
					assert.NoError(t, err)
				})
			}
		})

		t.Run("WHEN it is revoked", func(t *testing.T) {
			list, err := local.ListAccessTokens(ctx, connect.NewRequest(&pb.ListAccessTokensRequest{}))
			require.NoError(t, err)

			_, err = local.RevokeAccessToken(ctx, connect.NewRequest(&pb.RevokeAccessTokenRequest{Id: list.Msg.Tokens[0].Id}))
			require.NoError(t, err)

			_, err = check(bearer(secret, "gamevault.example:8080", nil))

			t.Run("THEN the very next request is refused", func(t *testing.T) {
				assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
			})

			t.Run("AND revoking it again is not found", func(t *testing.T) {
				_, err := local.RevokeAccessToken(ctx, connect.NewRequest(&pb.RevokeAccessTokenRequest{Id: list.Msg.Tokens[0].Id}))
				assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
			})
		})
	})
}

func TestAccessTokens_guessing(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	local := gamevaultv1connect.NewAuthServiceClient(http.DefaultClient, c.baseURL)
	guess := func(secret string) error {
		_, err := gamevaultv1connect.NewOwnershipServiceClient(bearer(secret, "gamevault.example:8080", nil), c.baseURL).
			CheckOwnership(ctx, connect.NewRequest(&pb.CheckOwnershipRequest{}))

		return err
	}

	t.Run("GIVEN ten wrong tokens from a host name", func(t *testing.T) {
		for i := range 10 {
			require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(guess(fmt.Sprintf("gvt_guess%d", i))))
		}

		t.Run("THEN the next attempt is told to wait", func(t *testing.T) {
			assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(guess("gvt_guess10")))
		})

		t.Run("AND signing in with a password waits too", func(t *testing.T) {
			_, err := local.Login(ctx, connect.NewRequest(&pb.LoginRequest{
				Username: "admin",
				Password: "correct horse",
			}))
			assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
		})
	})
}
