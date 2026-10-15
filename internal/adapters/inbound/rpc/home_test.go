package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
)

func TestHome(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	t.Run("GIVEN a server with a deals provider and a game owned on Steam", func(t *testing.T) {
		c := newServer(t, &fakeProvider{})
		_, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
			Title: "Lantern Keep",
			Links: map[string]string{"steam": "4410"},
			Copies: []*pb.CopyDetails{{
				Kind:     pb.CopyKind_COPY_KIND_LIBRARY,
				Platform: "Steam",
			}},
		}))
		require.NoError(t, err)

		t.Run("WHEN the home page is read", func(t *testing.T) {
			res, err := c.home.GetHome(ctx, connect.NewRequest(&pb.GetHomeRequest{}))
			require.NoError(t, err)

			t.Run("THEN deals were never fetched and releases are not configured", func(t *testing.T) {
				assert.Equal(t, "never_fetched", res.Msg.Deals.GetState())
				assert.Equal(t, "Fake deals", res.Msg.Deals.GetProvider())
				assert.Equal(t, "not_configured", res.Msg.Releases.GetState())
				assert.Empty(t, res.Msg.Featured)
			})
		})

		t.Run("WHEN the user refreshes the deals", func(t *testing.T) {
			res, err := c.home.RefreshHome(ctx, connect.NewRequest(&pb.RefreshHomeRequest{Block: "deals"}))
			require.NoError(t, err)

			h := res.Msg.Home

			t.Run("THEN the block is fresh, without the game the user owns", func(t *testing.T) {
				assert.Equal(t, "fresh", h.Deals.GetState())
				assert.NotNil(t, h.Deals.GetFetchedAt())
				assert.NotNil(t, h.Deals.GetRefreshableAt())
				require.Len(t, h.Featured, 1)

				d := h.Featured[0]
				assert.Equal(t, "Harbor Lights", d.Title)
				assert.Equal(t, int64(499), d.Price.GetAmountMinor())
				assert.Equal(t, "EUR", d.Price.GetCurrency())
				assert.Equal(t, int32(75), d.Cut)
				assert.Equal(t, "https://itad.link/fake/", d.Url)
			})
		})

		t.Run("WHEN the user refreshes the deals again at once", func(t *testing.T) {
			_, err := c.home.RefreshHome(ctx, connect.NewRequest(&pb.RefreshHomeRequest{Block: "deals"}))

			t.Run("THEN it is refused as too soon", func(t *testing.T) {
				assert.Equal(t, connect.CodeResourceExhausted, connect.CodeOf(err))
			})
		})

		t.Run("WHEN the user refreshes releases, or a block that does not exist", func(t *testing.T) {
			_, notConfigured := c.home.RefreshHome(ctx, connect.NewRequest(&pb.RefreshHomeRequest{Block: "releases"}))
			_, unknown := c.home.RefreshHome(ctx, connect.NewRequest(&pb.RefreshHomeRequest{Block: "weather"}))

			t.Run("THEN they are told to enable a provider, or that the request is wrong", func(t *testing.T) {
				assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(notConfigured))
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(unknown))
			})
		})
	})
}

// TestHomeDeal_noSteamAppID checks that a deal reaches the web without its Steam app: the page has
// no use for it, and the home service matches ownership on the server.
func TestHomeDeal_noSteamAppID(t *testing.T) {
	fields := (&pb.HomeDeal{}).ProtoReflect().Descriptor().Fields()
	assert.Nil(t, fields.ByName("steam_app_id"))
}
