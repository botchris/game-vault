package rpc_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "gamevault/internal/gen/gamevault/v1"
	"gamevault/internal/gen/gamevault/v1/gamevaultv1connect"
)

func TestCheckOwnership(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	c := newServer(t, &fakeProvider{})
	created, err := c.games.CreateGame(ctx, connect.NewRequest(&pb.CreateGameRequest{
		Title: "Lantern Keep",
		Links: map[string]string{"steam": "4410"},
		Copies: []*pb.CopyDetails{{
			Kind:     pb.CopyKind_COPY_KIND_KEY,
			Platform: "Steam",
		}},
	}))
	require.NoError(t, err)

	id := created.Msg.Game.Id

	t.Run("GIVEN this computer, a trusted network", func(t *testing.T) {
		t.Run("WHEN it checks a store id, a title and an unknown title", func(t *testing.T) {
			res, err := c.ownership.CheckOwnership(ctx, connect.NewRequest(&pb.CheckOwnershipRequest{Items: []*pb.OwnershipItem{
				{
					Store: "steam",
					Id:    "4410",
				},
				{Title: "lantern keep"},
				{Title: "Nobody Owns This"},
			}}))
			require.NoError(t, err)

			r := res.Msg.Results

			t.Run("THEN the first two are the game, with its pending key and the address of its sheet", func(t *testing.T) {
				require.Len(t, r, 3)

				for _, owned := range r[:2] {
					assert.Equal(t, "owned", owned.Status)
					assert.Equal(t, id, owned.Game.GetId())
					assert.Equal(t, "#/game/"+id, owned.Game.GetPath())
					require.Len(t, owned.Game.GetCopies(), 1)
					assert.Equal(t, "key", owned.Game.Copies[0].Kind)
					assert.Equal(t, "PC", owned.Game.Copies[0].System)
					assert.True(t, owned.Game.Copies[0].PendingKey)
				}
			})

			t.Run("AND the last is not owned", func(t *testing.T) {
				assert.Equal(t, "not_owned", r[2].Status)
				assert.Nil(t, r[2].Game)
			})
		})

		t.Run("WHEN it checks more than 200 items", func(t *testing.T) {
			items := make([]*pb.OwnershipItem, 201)
			for i := range items {
				items[i] = &pb.OwnershipItem{Title: "Lantern Keep"}
			}

			_, err := c.ownership.CheckOwnership(ctx, connect.NewRequest(&pb.CheckOwnershipRequest{Items: items}))

			t.Run("THEN it is told to split the list", func(t *testing.T) {
				assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
				assert.Contains(t, err.Error(), "split the list")
			})
		})
	})

	t.Run("GIVEN a request that names the server by a host name, without a token", func(t *testing.T) {
		_, err := gamevaultv1connect.NewOwnershipServiceClient(clientWith("gamevault.example:8080", nil), c.baseURL).
			CheckOwnership(ctx, connect.NewRequest(&pb.CheckOwnershipRequest{Items: []*pb.OwnershipItem{{Title: "Lantern Keep"}}}))

		t.Run("THEN it is refused like any other API request", func(t *testing.T) {
			assert.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		})
	})
}
